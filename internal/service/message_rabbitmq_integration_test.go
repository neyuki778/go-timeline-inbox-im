package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go-im/internal/infra"
	"go-im/internal/model"
	"go-im/internal/repository"
)

// TestRabbitMQPipelineIntegration 验证 MQ 入队 -> 消费 -> 落库 -> Inbox 的闭环。
func TestRabbitMQPipelineIntegration(t *testing.T) {
	ctx := context.Background()

	// 初始化 MySQL
	db, err := repository.NewDB()
	if err != nil {
		t.Skipf("skip: MySQL not available: %v", err)
	}
	if err := db.AutoMigrate(&model.TimelineMessage{}, &model.UserConversationState{}); err != nil {
		t.Fatalf("migrate tables: %v", err)
	}
	msgRepo := repository.NewMessageRepository(db)

	// 初始化 Redis
	rdb := infra.NewRedisClient()
	if rdb == nil {
		t.Skip("skip: Redis not configured")
	}
	if err := infra.PingRedis(ctx, rdb); err != nil {
		t.Skipf("skip: Redis not reachable: %v", err)
	}
	seqGen := NewRedisSeqGenerator(rdb, "test:im:seq:")
	inbox := NewRedisInboxWriter(rdb, "test:im:inbox:", time.Hour)
	msgSvc := NewMessageServiceWithSeq(msgRepo, seqGen).WithInbox(inbox)

	// 初始化 RabbitMQ，使用测试专用拓扑名称，避免污染正式队列
	mqCfg := infra.LoadRabbitMQConfig()
	mqCfg.Exchange = "test.im.direct"
	mqCfg.Queue = "test.im.msg.process"
	mqCfg.RoutingKey = "test.msg.send"

	conn, err := infra.NewRabbitMQ(mqCfg)
	if err != nil {
		t.Skipf("skip: RabbitMQ not available: %v", err)
	}
	defer conn.Close()

	pubCh, err := conn.Channel()
	if err != nil {
		t.Fatalf("open publish channel: %v", err)
	}
	defer pubCh.Close()
	subCh, err := conn.Channel()
	if err != nil {
		t.Fatalf("open consume channel: %v", err)
	}
	defer subCh.Close()

	if err := infra.PrepareRabbitTopology(pubCh, mqCfg); err != nil {
		t.Fatalf("prepare topology (pub): %v", err)
	}
	if err := infra.PrepareRabbitTopology(subCh, mqCfg); err != nil {
		t.Fatalf("prepare topology (sub): %v", err)
	}
	if _, err := subCh.QueuePurge(mqCfg.Queue, false); err != nil {
		t.Fatalf("purge queue: %v", err)
	}

	consumer := NewMessageConsumer(subCh, mqCfg.Queue, msgSvc)
	consumeCtx, cancelConsume := context.WithCancel(ctx)
	defer cancelConsume()
	if err := consumer.Start(consumeCtx); err != nil {
		t.Fatalf("start consumer: %v", err)
	}

	producer := NewMessageProducer(pubCh, mqCfg.Exchange, mqCfg.RoutingKey)

	convID := fmt.Sprintf("private_mq_u1_u2_%d", time.Now().UnixNano())
	msgID := fmt.Sprintf("mq-%d", time.Now().UnixNano())
	evt := ChatEvent{
		MsgID:          msgID,
		ConversationID: convID,
		SenderID:       "u1",
		Content:        "hello via mq",
		MsgType:        1,
		SendTime:       time.Now().UnixMilli(),
	}

	pubCtx, cancelPub := context.WithTimeout(ctx, 3*time.Second)
	if err := producer.PublishChat(pubCtx, evt); err != nil {
		cancelPub()
		t.Fatalf("publish to mq: %v", err)
	}
	cancelPub()

	// 等待消费完成并写入 MySQL
	var saved model.TimelineMessage
	waitDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(waitDeadline) {
		if err := db.WithContext(ctx).First(&saved, "msg_id = ?", msgID).Error; err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if saved.MsgID == "" {
		t.Fatalf("message not persisted via MQ")
	}
	if saved.ConversationID != convID || saved.Content != evt.Content || saved.Seq == 0 {
		t.Fatalf("persisted message mismatch: %+v", saved)
	}

	// 验证 Inbox 两个参与者都写入
	for _, uid := range []string{"u1", "u2"} {
		key := "test:im:inbox:" + uid
		vals, err := rdb.ZRange(ctx, key, 0, -1).Result()
		if err != nil {
			t.Fatalf("read inbox for %s: %v", uid, err)
		}
		if len(vals) != 1 {
			t.Fatalf("expected 1 inbox item for %s, got %d", uid, len(vals))
		}
	}
}
