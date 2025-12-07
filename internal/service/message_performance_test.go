package service

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"go-im/internal/infra"
	"go-im/internal/model"
	"go-im/internal/repository"
)

func prepareMySQL(b *testing.B) *repository.MessageRepository {
	b.Helper()
	db, err := repository.NewDB()
	if err != nil {
		b.Skipf("skip: MySQL not available: %v", err)
	}
	// 清理基准用数据
	_ = db.Exec("DELETE FROM timeline_message WHERE conversation_id LIKE 'bench-%' OR conversation_id LIKE 'private_bench%' OR msg_id LIKE 'msg-%'").Error
	return repository.NewMessageRepository(db)
}

func prepareRedis(b *testing.B) *RedisSeqGenerator {
	b.Helper()
	rdb := infra.NewRedisClient()
	if rdb == nil {
		b.Skip("skip: Redis not configured")
	}
	if err := infra.PingRedis(context.Background(), rdb); err != nil {
		b.Skipf("skip: Redis not reachable: %v", err)
	}
	// 清理基准用数据
	_ = rdb.FlushDB(context.Background()).Err()
	return NewRedisSeqGenerator(rdb, "bench:seq:")
}

func prepareRedisInbox(b *testing.B, rdb *redis.Client) InboxWriter {
	if rdb == nil {
		return nil
	}
	return NewRedisInboxWriter(rdb, "bench:inbox:", time.Hour)
}

func BenchmarkHandleChatSeqMySQL(b *testing.B) {
	repo := prepareMySQL(b)
	svc := NewMessageService(repo) // 使用 MySQL MAX(seq)+1
	var counter uint64
	runID := time.Now().UnixNano()
	convID := fmt.Sprintf("bench-mysql-%d", runID)

	payload := ChatPayload{Content: "hi", MsgType: 1}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := atomic.AddUint64(&counter, 1)
		packet := model.InputPacket{
			Cmd:            model.CmdChat,
			ConversationId: convID,
			MsgId:          fmt.Sprintf("msg-%d-%d", runID, id),
		}
		if _, err := svc.HandleChat(context.Background(), "u1", packet, payload); err != nil {
			b.Fatalf("HandleChat MySQL error: %v", err)
		}
	}
}

func BenchmarkHandleChatSeqRedis(b *testing.B) {
	repo := prepareMySQL(b)
	seqGen := prepareRedis(b)
	svc := NewMessageServiceWithSeq(repo, seqGen) // 使用 Redis INCR 生成 seq
	var counter uint64
	runID := time.Now().UnixNano()
	convID := fmt.Sprintf("bench-redis-%d", runID)

	payload := ChatPayload{Content: "hi", MsgType: 1}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := atomic.AddUint64(&counter, 1)
		packet := model.InputPacket{
			Cmd:            model.CmdChat,
			ConversationId: convID,
			MsgId:          fmt.Sprintf("msg-%d-%d", runID, id),
		}
		if _, err := svc.HandleChat(context.Background(), "u1", packet, payload); err != nil {
			b.Fatalf("HandleChat Redis error: %v", err)
		}
	}
}

func BenchmarkHandleChatSeqRedisWithInbox(b *testing.B) {
	repo := prepareMySQL(b)
	seqGen := prepareRedis(b)
	rdb := infra.NewRedisClient()
	if rdb == nil {
		b.Skip("skip: Redis not configured")
	}
	if err := infra.PingRedis(context.Background(), rdb); err != nil {
		b.Skipf("skip: Redis not reachable: %v", err)
	}
	inbox := prepareRedisInbox(b, rdb)
	svc := NewMessageServiceWithSeq(repo, seqGen).WithInbox(inbox) // seq + inbox
	var counter uint64
	runID := time.Now().UnixNano()
	convID := fmt.Sprintf("private_bench_%d_u1_u2", runID)

	payload := ChatPayload{Content: "hi", MsgType: 1}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := atomic.AddUint64(&counter, 1)
		packet := model.InputPacket{
			Cmd:            model.CmdChat,
			ConversationId: convID,
			MsgId:          fmt.Sprintf("msg-%d-%d", runID, id),
		}
		if _, err := svc.HandleChat(context.Background(), "u1", packet, payload); err != nil {
			b.Fatalf("HandleChat Redis+Inbox error: %v", err)
		}
	}
}
