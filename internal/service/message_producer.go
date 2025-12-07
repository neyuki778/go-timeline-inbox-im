package service

import (
	"context"
	"encoding/json"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// ChatEvent 代表发送到 MQ 的聊天事件。
type ChatEvent struct {
	MsgID          string `json:"msg_id"`
	ConversationID string `json:"conversation_id"`
	SenderID       string `json:"sender_id"`
	Content        string `json:"content"`
	MsgType        int8   `json:"msg_type"`
	SendTime       int64  `json:"send_time"`
}

// MessageProducer 负责将事件发布到 RabbitMQ。
type MessageProducer struct {
	ch         *amqp.Channel
	exchange   string
	routingKey string
}

func NewMessageProducer(ch *amqp.Channel, exchange, routingKey string) *MessageProducer {
	return &MessageProducer{
		ch:         ch,
		exchange:   exchange,
		routingKey: routingKey,
	}
}

// PublishChat 将聊天事件发布到 MQ，使用持久化消息。
func (p *MessageProducer) PublishChat(ctx context.Context, evt ChatEvent) error {
	body, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	return p.ch.PublishWithContext(ctx,
		p.exchange,
		p.routingKey,
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         body,
			DeliveryMode: amqp.Persistent,
			Timestamp:    time.Now(),
			MessageId:    evt.MsgID,
		})
}
