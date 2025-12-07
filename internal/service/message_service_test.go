package service_test

import (
	"context"
	"errors"
	"testing"

	"go-im/internal/model"
	"go-im/internal/repository"
	"go-im/internal/service"

	"gorm.io/gorm"
)

func newServiceWithMySQL(t *testing.T) (*service.MessageService, *gorm.DB) {
	t.Helper()
	db, err := repository.NewDB()
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	if err := db.AutoMigrate(&model.TimelineMessage{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	repo := repository.NewMessageRepository(db)
	return service.NewMessageService(repo), db
}

func TestHandleChatFillsDefaultsAndPersists(t *testing.T) {
	svc, db := newServiceWithMySQL(t)
	ctx := context.Background()

	packet := model.InputPacket{Cmd: model.CmdChat, ConversationId: uniqueID("conv-defaults")}
	payload := service.ChatPayload{Content: "hi"} // msg_type 缺省
	out, err := svc.HandleChat(ctx, "u1", packet, payload)
	if err != nil {
		t.Fatalf("HandleChat returned error: %v", err)
	}
	if out.Code != 0 {
		t.Fatalf("expected Code=0, got %d", out.Code)
	}
	if out.MsgId == "" {
		t.Fatalf("expected generated msg_id, got empty")
	}
	if out.Seq != 1 {
		t.Fatalf("expected seq=1, got %d", out.Seq)
	}

	var saved model.TimelineMessage
	if err := db.WithContext(ctx).Where("msg_id = ?", out.MsgId).First(&saved).Error; err != nil {
		t.Fatalf("failed to query saved message: %v", err)
	}
	if saved.MsgType != 1 {
		t.Fatalf("expected default msg_type=1, got %d", saved.MsgType)
	}
	if saved.SenderID != "u1" || saved.Content != "hi" || saved.ConversationID != packet.ConversationId {
		t.Fatalf("unexpected saved message: %+v", saved)
	}
	if saved.SendTime == 0 {
		t.Fatalf("expected non-zero send_time")
	}
}

func TestHandleChatIdempotentMsgID(t *testing.T) {
	svc, db := newServiceWithMySQL(t)
	ctx := context.Background()

	packet := model.InputPacket{Cmd: model.CmdChat, ConversationId: uniqueID("conv-idem"), MsgId: uniqueID("fixed-id")}
	payload := service.ChatPayload{Content: "hello", MsgType: 1}

	out1, err := svc.HandleChat(ctx, "u1", packet, payload)
	if err != nil {
		t.Fatalf("HandleChat first error: %v", err)
	}
	if out1.Code != 0 {
		t.Fatalf("expected Code=0 first call, got %d", out1.Code)
	}

	out2, err := svc.HandleChat(ctx, "u1", packet, payload)
	if err != nil {
		t.Fatalf("HandleChat second error: %v", err)
	}
	if out2.Code != 0 {
		t.Fatalf("expected Code=0 second call, got %d", out2.Code)
	}
	if out1.Seq != out2.Seq {
		t.Fatalf("idempotent call should return same seq, got %d and %d", out1.Seq, out2.Seq)
	}
	if out2.MsgId != packet.MsgId {
		t.Fatalf("expected MsgId=%s, got %s", packet.MsgId, out2.MsgId)
	}

	var count int64
	if err := db.WithContext(ctx).Model(&model.TimelineMessage{}).Where("msg_id = ?", packet.MsgId).Count(&count).Error; err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("idempotent call should not insert duplicate rows, got %d", count)
	}
}

type errorRepo struct {
	err error
}

func (e errorRepo) SaveMessage(ctx context.Context, msg *model.TimelineMessage) error {
	return e.err
}
func (e errorRepo) FindByMsgID(ctx context.Context, msgID string) (*model.TimelineMessage, error) {
	return nil, gorm.ErrRecordNotFound
}

func TestHandleChatPropagatesRepoError(t *testing.T) {
	repoErr := errors.New("db down")
	svc := service.NewMessageService(errorRepo{err: repoErr})
	packet := model.InputPacket{Cmd: model.CmdChat, ConversationId: "conv-error", MsgId: "x"}
	payload := service.ChatPayload{Content: "msg", MsgType: 1}

	_, err := svc.HandleChat(context.Background(), "u1", packet, payload)
	if !errors.Is(err, repoErr) {
		t.Fatalf("expected repo error to propagate, got %v", err)
	}
}
