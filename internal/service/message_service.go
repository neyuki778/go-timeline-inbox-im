package service

import (
	"context"
	"errors"
	"log"
	"time"

	"go-im/internal/model"
	"go-im/internal/repository"

	"github.com/google/uuid"
)

// MessageService 封装消息写库逻辑。
type MessageService struct {
	msgRepo MessageSaver
}

// MessageSaver 描述消息持久化需要实现的接口，便于测试替换。
type MessageSaver interface {
	SaveMessage(ctx context.Context, msg *model.TimelineMessage) error
	FindByMsgID(ctx context.Context, msgID string) (*model.TimelineMessage, error)
}

func NewMessageService(msgRepo MessageSaver) *MessageService {
	return &MessageService{msgRepo: msgRepo}
}

// ChatPayload 表示聊天消息的负载体。
type ChatPayload struct {
	Content string `json:"content"`
	MsgType int8   `json:"msg_type"` // 1: 文本，2: 图片
}

// HandleChat 保存消息并返回写入后的 seq。
func (s *MessageService) HandleChat(ctx context.Context, userID string, packet model.InputPacket, payload ChatPayload) (model.OutputPacket, error) {
	// TODO: 生成 msg_id（若缺省）、填充默认 msg_type，调用仓储写库并处理幂等/错误，返回 seq
	msg_id := packet.MsgId
	if msg_id == ""{
		msg_id = uuid.NewString()
	}
	if payload.MsgType == 0{
		payload.MsgType = 1
	}

	msg := &model.TimelineMessage{
		MsgID: msg_id,
		ConversationID: packet.ConversationId,
		SenderID: userID,
		Content: payload.Content,
		MsgType: payload.MsgType,
		Status: 1,
		SendTime: time.Now().UnixMilli(),
	}

	err := s.msgRepo.SaveMessage(ctx, msg)
	if err != nil{
		if errors.Is(err, repository.ErrDuplicateMsgID){
			log.Printf("重复消息 msg_id=%s，返回幂等结果", msg_id)
			return model.OutputPacket{MsgId: msg.MsgID, Seq: int64(msg.Seq)}, err
		}else{
			return  model.OutputPacket{Cmd: model.CmdChat, }, err
		}
	}
	return model.OutputPacket{
		Cmd: model.CmdChat,
		Code: 0,
		MsgId: msg_id,
		Seq: int64(msg.Seq),
	}, nil
}
