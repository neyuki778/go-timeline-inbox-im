package service

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"go-im/internal/model"
	"go-im/internal/repository"

	"github.com/google/uuid"
)

// MessageService 封装消息写库逻辑。
type MessageService struct {
	msgRepo MessageSaver
	seqGen  SeqGenerator // 可选的 seq 生成器（例如 Redis），为 nil 时走仓储默认逻辑
	inbox   InboxWriter  // 可选的 Inbox 写入器（Redis），为 nil 时不写
}

// MessageSaver 描述消息持久化需要实现的接口，便于测试替换。
type MessageSaver interface {
	SaveMessage(ctx context.Context, msg *model.TimelineMessage) error
	FindByMsgID(ctx context.Context, msgID string) (*model.TimelineMessage, error)
}

func NewMessageService(msgRepo MessageSaver) *MessageService {
	return &MessageService{msgRepo: msgRepo}
}

// NewMessageServiceWithSeq 允许注入自定义的 seq 生成器（如 Redis）。
func NewMessageServiceWithSeq(msgRepo MessageSaver, seqGen SeqGenerator) *MessageService {
	return &MessageService{msgRepo: msgRepo, seqGen: seqGen}
}

// WithSeqGenerator 可选注入自定义 seq 生成器。
func (s *MessageService) WithSeqGenerator(gen SeqGenerator) *MessageService {
	s.seqGen = gen
	return s
}

// WithInbox 可选注入 Inbox 写入器（如 Redis）。
func (s *MessageService) WithInbox(inbox InboxWriter) *MessageService {
	s.inbox = inbox
	return s
}

// ChatPayload 表示聊天消息的负载体。
type ChatPayload struct {
	Content  string `json:"content"`
	MsgType  int8   `json:"msg_type"`  // 1: 文本，2: 图片
	SendTime int64  `json:"send_time"` // 可选：外部指定发送时间（ms）
}

// HandleChat 保存消息并返回写入后的 seq。
func (s *MessageService) HandleChat(ctx context.Context, userID string, packet model.InputPacket, payload ChatPayload) (model.OutputPacket, error) {
	// TODO: 生成 msg_id（若缺省）、填充默认 msg_type，调用仓储写库并处理幂等/错误，返回 seq
	msg_id := packet.MsgId
	if msg_id == "" {
		msg_id = uuid.NewString()
	}
	if payload.MsgType == 0 {
		payload.MsgType = 1
	}

	sendTime := payload.SendTime
	if sendTime == 0 {
		sendTime = time.Now().UnixMilli()
	}

	msg := &model.TimelineMessage{
		MsgID:          msg_id,
		ConversationID: packet.ConversationId,
		SenderID:       userID,
		Content:        payload.Content,
		MsgType:        payload.MsgType,
		Status:         1,
		SendTime:       sendTime,
	}

	// 如果有外部 seq 生成器（这里是 Redis），优先获取 seq 后写库
	if s.seqGen != nil {
		seq, seqErr := s.seqGen.NextSeq(ctx, packet.ConversationId)
		if seqErr != nil {
			return model.OutputPacket{Cmd: model.CmdChat, Code: 1, MsgId: msg_id, Payload: "generate seq failed"}, seqErr
		}
		msg.Seq = seq
	}

	err := s.msgRepo.SaveMessage(ctx, msg)
	if err != nil {
		if errors.Is(err, repository.ErrDuplicateMsgID) {
			log.Printf("重复消息 msg_id=%s，返回幂等结果", msg_id)
			// 幂等场景：查已有记录并返回已有 seq
			existing, findErr := s.msgRepo.FindByMsgID(ctx, msg_id)
			if findErr != nil {
				return model.OutputPacket{Cmd: model.CmdChat, Code: 1, MsgId: msg_id}, findErr
			}
			return model.OutputPacket{
				Cmd:   model.CmdChat,
				Code:  0,
				MsgId: msg_id,
				Seq:   int64(existing.Seq),
			}, nil
		} else {
			return model.OutputPacket{Cmd: model.CmdChat, Code: 1, MsgId: msg_id}, err
		}
	}

	// 写入 Inbox（仅在配置了 Redis 时，假设会话 ID 形如 private_userA_userB）
	if s.inbox != nil {
		targets := parsePrivateParticipants(packet.ConversationId, userID)
		if len(targets) > 0 {
			if err := s.inbox.Append(ctx, *msg, targets); err != nil {
				log.Printf("写入 Inbox 失败 conv=%s msg_id=%s: %v", packet.ConversationId, msg_id, err)
			}
		}
	}

	return model.OutputPacket{
		Cmd:   model.CmdChat,
		Code:  0,
		MsgId: msg_id,
		Seq:   int64(msg.Seq),
	}, nil
}

// parsePrivateParticipants 会话 ID 形如 "private_u1_u2"，返回需要投递 Inbox 的用户列表。
func parsePrivateParticipants(conversationID, senderID string) []string {
	const prefix = "private_"
	if !strings.HasPrefix(conversationID, prefix) {
		return nil
	}
	parts := strings.Split(strings.TrimPrefix(conversationID, prefix), "_")
	if len(parts) < 2 {
		return nil
	}
	seen := make(map[string]struct{})
	var users []string
	for _, uid := range parts {
		if uid == "" {
			continue
		}
		if _, ok := seen[uid]; ok {
			continue
		}
		seen[uid] = struct{}{}
		users = append(users, uid)
	}
	// 确保包含发送者（以防会话 ID 未包含）
	if senderID != "" {
		if _, ok := seen[senderID]; !ok {
			users = append(users, senderID)
		}
	}
	return users
}
