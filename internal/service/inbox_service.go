package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"

	"go-im/internal/model"
)

// InboxWriter 定义 Inbox 写入接口。
type InboxWriter interface {
	Append(ctx context.Context, msg model.TimelineMessage, userIDs []string) error
}

// RedisInboxWriter 使用 Redis Sorted Set 实现单聊 Inbox 写扩散。
type RedisInboxWriter struct {
	client    *redis.Client
	keyPrefix string
	ttl       time.Duration
}

func NewRedisInboxWriter(client *redis.Client, prefix string, ttl time.Duration) *RedisInboxWriter {
	return &RedisInboxWriter{
		client:    client,
		keyPrefix: prefix,
		ttl:       ttl,
	}
}

// Append 将消息元信息写入指定用户的 Inbox。
func (w *RedisInboxWriter) Append(ctx context.Context, msg model.TimelineMessage, userIDs []string) error {
	if w.client == nil {
		return nil
	}
	payload := inboxPayload{
		MsgID:          msg.MsgID,
		ConversationID: msg.ConversationID,
		Seq:            msg.Seq,
		SenderID:       msg.SenderID,
		Content:        msg.Content,
		MsgType:        msg.MsgType,
		SendTime:       msg.SendTime,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	pipe := w.client.Pipeline()
	for _, uid := range userIDs {
		if uid == "" {
			continue
		}
		key := w.keyPrefix + uid
		pipe.ZAdd(ctx, key, redis.Z{
			Score:  float64(msg.Seq),
			Member: data,
		})
		if w.ttl > 0 {
			pipe.Expire(ctx, key, w.ttl)
		}
	}
	_, err = pipe.Exec(ctx)
	return err
}

// inboxPayload 是存入 Inbox 的精简消息元信息。
type inboxPayload struct {
	MsgID          string `json:"msg_id"`
	ConversationID string `json:"conversation_id"`
	Seq            uint64 `json:"seq"`
	SenderID       string `json:"sender_id"`
	Content        string `json:"content"`
	MsgType        int8   `json:"msg_type"`
	SendTime       int64  `json:"send_time"`
}
