package service

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"go-im/internal/model"

	amqp "github.com/rabbitmq/amqp091-go"
)

// MessageConsumer 消费 MQ 中的消息并落库/写 Inbox/推送。
type MessageConsumer struct {
	ch    *amqp.Channel
	queue string
	svc   *MessageService
}

func NewMessageConsumer(ch *amqp.Channel, queue string, svc *MessageService) *MessageConsumer {
	return &MessageConsumer{
		ch:    ch,
		queue: queue,
		svc:   svc,
	}
}

// Start 启动消费循环（非阻塞），ctx 取消后退出。
func (c *MessageConsumer) Start(ctx context.Context) error {
	deliveries, err := c.ch.Consume(
		c.queue,
		"",
		false, // autoAck
		false, // exclusive
		false, // noLocal
		false, // noWait
		nil,
	)
	if err != nil {
		return err
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-deliveries:
				if !ok {
					return
				}
				c.handleDelivery(ctx, msg)
			}
		}
	}()
	return nil
}

func (c *MessageConsumer) handleDelivery(parentCtx context.Context, msg amqp.Delivery) {
	var evt ChatEvent
	if err := json.Unmarshal(msg.Body, &evt); err != nil {
		log.Printf("解析 MQ 消息失败: %v", err)
		_ = msg.Nack(false, false) // 丢弃坏消息
		return
	}

	packet := model.InputPacket{
		Cmd:            model.CmdChat,
		MsgId:          evt.MsgID,
		ConversationId: evt.ConversationID,
	}
	payload := ChatPayload{
		Content:  evt.Content,
		MsgType:  evt.MsgType,
		SendTime: evt.SendTime,
	}

	ctx, cancel := context.WithTimeout(parentCtx, 5*time.Second)
	defer cancel()

	if _, err := c.svc.HandleChat(ctx, evt.SenderID, packet, payload); err != nil {
		log.Printf("消费消息失败 msg_id=%s: %v", evt.MsgID, err)
		_ = msg.Nack(false, true) // 失败可重试
		return
	}

	_ = msg.Ack(false)
}
