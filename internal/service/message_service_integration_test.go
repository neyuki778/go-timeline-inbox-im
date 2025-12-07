package service

import (
	"context"
	"testing"
	"time"

	"go-im/internal/infra"
	"go-im/internal/model"
	"go-im/internal/repository"
)

func TestHandleChatWithRedisSeqAndInboxIntegration(t *testing.T) {
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
	if err := infra.PingRedis(context.Background(), rdb); err != nil {
		t.Skipf("skip: Redis not reachable: %v", err)
	}
	// 清理测试用的 key 空间
	_ = rdb.FlushDB(context.Background()).Err()

	seqGen := NewRedisSeqGenerator(rdb, "test:im:seq:")
	inbox := NewRedisInboxWriter(rdb, "test:im:inbox:", time.Hour)
	svc := NewMessageServiceWithSeq(msgRepo, seqGen).WithInbox(inbox)

	ctx := context.Background()
	convID := "private_integration_u1_u2"
	packet := model.InputPacket{Cmd: model.CmdChat, ConversationId: convID, MsgId: ""}
	payload := ChatPayload{Content: "hello int", MsgType: 1}

	out, err := svc.HandleChat(ctx, "u1", packet, payload)
	if err != nil {
		t.Fatalf("HandleChat error: %v", err)
	}
	if out.Code != 0 || out.Seq == 0 || out.MsgId == "" {
		t.Fatalf("unexpected output: %+v", out)
	}

	// 验证落库
	var saved model.TimelineMessage
	if err := db.WithContext(ctx).First(&saved, "msg_id = ?", out.MsgId).Error; err != nil {
		t.Fatalf("query saved message: %v", err)
	}
	if saved.Seq != uint64(out.Seq) || saved.ConversationID != convID {
		t.Fatalf("saved message mismatch: %+v", saved)
	}

	// 验证 Inbox 写入（u1/u2 都应有数据）
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
