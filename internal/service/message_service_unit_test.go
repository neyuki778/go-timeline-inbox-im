package service

import (
	"context"
	"errors"
	"testing"

	"go-im/internal/model"
	"go-im/internal/repository"
)

type stubSeqGen struct {
	seq uint64
	err error
}

func (s *stubSeqGen) NextSeq(ctx context.Context, conversationID string) (uint64, error) {
	if s.err != nil {
		return 0, s.err
	}
	s.seq++
	return s.seq, nil
}

type stubInbox struct {
	appends []stubInboxRecord
	err     error
}

type stubInboxRecord struct {
	msg     model.TimelineMessage
	userIDs []string
}

func (s *stubInbox) Append(ctx context.Context, msg model.TimelineMessage, userIDs []string) error {
	s.appends = append(s.appends, stubInboxRecord{msg: msg, userIDs: userIDs})
	return s.err
}

type stubMsgRepo struct {
	store map[string]*model.TimelineMessage
}

func newStubMsgRepo() *stubMsgRepo {
	return &stubMsgRepo{store: make(map[string]*model.TimelineMessage)}
}

func (r *stubMsgRepo) SaveMessage(ctx context.Context, msg *model.TimelineMessage) error {
	if _, ok := r.store[msg.MsgID]; ok {
		return repository.ErrDuplicateMsgID
	}
	// 简单的自增模拟
	if msg.Seq == 0 {
		msg.Seq = uint64(len(r.store) + 1)
	}
	cp := *msg
	r.store[msg.MsgID] = &cp
	return nil
}

func (r *stubMsgRepo) FindByMsgID(ctx context.Context, msgID string) (*model.TimelineMessage, error) {
	if v, ok := r.store[msgID]; ok {
		cp := *v
		return &cp, nil
	}
	return nil, errors.New("not found")
}

func TestHandleChatUsesSeqGenAndInbox(t *testing.T) {
	repo := newStubMsgRepo()
	seqGen := &stubSeqGen{seq: 99}
	inbox := &stubInbox{}
	svc := NewMessageServiceWithSeq(repo, seqGen).WithInbox(inbox)

	packet := model.InputPacket{Cmd: model.CmdChat, ConversationId: "private_u1_u2", MsgId: "mid-1"}
	payload := ChatPayload{Content: "hi", MsgType: 1}

	out, err := svc.HandleChat(context.Background(), "u1", packet, payload)
	if err != nil {
		t.Fatalf("HandleChat returned error: %v", err)
	}
	if out.Code != 0 {
		t.Fatalf("expected Code=0, got %d", out.Code)
	}
	if out.Seq != 100 {
		t.Fatalf("expected seq from seqGen (100), got %d", out.Seq)
	}
	saved := repo.store[packet.MsgId]
	if saved == nil || saved.Seq != 100 {
		t.Fatalf("message not saved with expected seq")
	}
	if len(inbox.appends) != 1 {
		t.Fatalf("expected inbox append once, got %d", len(inbox.appends))
	}
	targets := inbox.appends[0].userIDs
	if len(targets) != 2 || !contains(targets, "u1") || !contains(targets, "u2") {
		t.Fatalf("unexpected inbox targets: %v", targets)
	}
}

func TestHandleChatSeqGenError(t *testing.T) {
	repo := newStubMsgRepo()
	seqGen := &stubSeqGen{err: errors.New("redis down")}
	svc := NewMessageServiceWithSeq(repo, seqGen)

	packet := model.InputPacket{Cmd: model.CmdChat, ConversationId: "private_u1_u2", MsgId: "mid-err"}
	payload := ChatPayload{Content: "x", MsgType: 1}

	_, err := svc.HandleChat(context.Background(), "u1", packet, payload)
	if err == nil {
		t.Fatalf("expected error when seq generator fails")
	}
	if _, ok := repo.store[packet.MsgId]; ok {
		t.Fatalf("message should not be saved on seq gen error")
	}
}

func TestHandleChatIdempotentDuplicate(t *testing.T) {
	repo := newStubMsgRepo()
	seqGen := &stubSeqGen{}
	svc := NewMessageServiceWithSeq(repo, seqGen)

	packet := model.InputPacket{Cmd: model.CmdChat, ConversationId: "private_u1_u2", MsgId: "mid-dup"}
	payload := ChatPayload{Content: "hello", MsgType: 1}

	first, err := svc.HandleChat(context.Background(), "u1", packet, payload)
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	second, err := svc.HandleChat(context.Background(), "u1", packet, payload)
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if first.Seq != second.Seq {
		t.Fatalf("duplicate msg_id should return same seq, got %d and %d", first.Seq, second.Seq)
	}
}

func contains(arr []string, target string) bool {
	for _, v := range arr {
		if v == target {
			return true
		}
	}
	return false
}
