package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"go-im/internal/model"
	"go-im/internal/service"
)

const (
	readDeadline = 90 * time.Second // 允许心跳丢 2-3 次（30s/跳）
	writeTimeout = 10 * time.Second // 写超时防止阻塞
	readLimit    = int64(4 << 10)   // 单条消息最大 4KB
)

// WebSocketHandler 负责握手、注册连接以及消息读循环。
type WebSocketHandler struct {
	connManager *service.ConnectionManager
	messageSvc  *service.MessageService
	producer    *service.MessageProducer
	upgrader    websocket.Upgrader
}

// NewWebSocketHandler 创建 Handler，允许注入连接管理器与消息服务。
func NewWebSocketHandler(connManager *service.ConnectionManager, messageSvc *service.MessageService) *WebSocketHandler {
	return &WebSocketHandler{
		connManager: connManager,
		messageSvc:  messageSvc,
		upgrader: websocket.Upgrader{
			// 生产环境需校验 Origin
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

// WithProducer 注入 MQ 生产者，启用“先入队再处理”的路径。
func (h *WebSocketHandler) WithProducer(producer *service.MessageProducer) *WebSocketHandler {
	h.producer = producer
	return h
}

// HandleWebSocket 提供给 Gin 的路由函数。
func (h *WebSocketHandler) HandleWebSocket(c *gin.Context) {
	userID := c.Query("user_id")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id 不能为空"})
		return
	}

	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("用户 %s 升级 WebSocket 失败: %v", userID, err)
		return
	}

	h.connManager.Add(userID, conn)
	log.Printf("用户 %s 已连接，当前在线: %v", userID, h.connManager.ListIDs())

	// 独立 goroutine 读消息，避免阻塞握手返回
	go h.readLoop(userID, conn)
}

// readLoop 读取客户端消息，先支持心跳，后续扩展业务指令。
func (h *WebSocketHandler) readLoop(userID string, conn *websocket.Conn) {
	defer func() {
		h.connManager.Remove(userID)
		log.Printf("用户 %s 连接关闭", userID)
	}()

	conn.SetReadLimit(readLimit)
	_ = conn.SetReadDeadline(time.Now().Add(readDeadline))
	conn.SetPongHandler(func(string) error {
		// 客户端 Pong 刷新超时
		return conn.SetReadDeadline(time.Now().Add(readDeadline))
	})

	for {
		var packet model.InputPacket
		if err := conn.ReadJSON(&packet); err != nil {
			log.Printf("读取用户 %s 消息失败: %v", userID, err)
			return
		}

		switch packet.Cmd {
		case model.CmdHeartbeat:
			if err := h.writeJSON(conn, model.OutputPacket{Cmd: model.CmdHeartbeat, Code: 0}); err != nil {
				log.Printf("心跳回复失败 user=%s: %v", userID, err)
				return
			}
		case model.CmdChat:
			if err := h.handleChat(userID, packet, conn); err != nil {
				log.Printf("处理聊天消息失败 user=%s: %v", userID, err)
				return
			}
		default:
			// 预留：登录、聊天、拉取等指令后续接入 service 层
			log.Printf("收到用户 %s 的指令 cmd=%d msg_id=%s", userID, packet.Cmd, packet.MsgId)
		}
	}
}

// writeJSON 统一设置写超时，防止写阻塞。
func (h *WebSocketHandler) writeJSON(conn *websocket.Conn, payload interface{}) error {
	_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	return conn.WriteJSON(payload)
}

// handleChat 处理聊天消息：解析、写库并返回 seq。
func (h *WebSocketHandler) handleChat(userID string, packet model.InputPacket, conn *websocket.Conn) error {
	if packet.ConversationId == "" {
		return h.writeJSON(conn, model.OutputPacket{Cmd: model.CmdChat, Code: 400, MsgId: packet.MsgId, Payload: "ConversationId 不能为空!"})
	}

	var payload service.ChatPayload
	if err := json.Unmarshal(packet.Payload, &payload); err != nil {
		return h.writeJSON(conn, model.OutputPacket{Cmd: model.CmdChat, Code: 400, MsgId: packet.MsgId, Payload: "Payload 解析失败!"})
	}

	// 如果注入了 MQ 生产者，则走“入队”路径立即响应
	if h.producer != nil {
		msgID := packet.MsgId
		if msgID == "" {
			msgID = uuid.NewString()
		}
		event := service.ChatEvent{
			MsgID:          msgID,
			ConversationID: packet.ConversationId,
			SenderID:       userID,
			Content:        payload.Content,
			MsgType:        payload.MsgType,
			SendTime:       payload.SendTime,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		if err := h.producer.PublishChat(ctx, event); err != nil {
			return h.writeJSON(conn, model.OutputPacket{Cmd: model.CmdChat, Code: 1, MsgId: msgID, Payload: "MQ 发布失败"})
		}
		return h.writeJSON(conn, model.OutputPacket{Cmd: model.CmdChat, Code: 0, MsgId: msgID, Payload: "accepted"})
	}

	// 默认路径：直接落库
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	outputPacket, err := h.messageSvc.HandleChat(ctx, userID, packet, payload)
	if err != nil {
		_ = h.writeJSON(conn, outputPacket)
		return err
	}
	return h.writeJSON(conn, outputPacket)
}
