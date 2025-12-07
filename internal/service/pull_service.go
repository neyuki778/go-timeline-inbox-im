package service

import (
	"context"

	"go-im/internal/model"
)

// PullResult 封装拉取结果。
type PullResult struct {
	Messages      []model.TimelineMessage
	NextCursorSeq int64
	HasMore       bool
}

// PullStorage 抽象仓储接口，便于测试替换。
type PullStorage interface {
	ListMessages(ctx context.Context, conversationID string, afterSeq int64, limit int) ([]model.TimelineMessage, error)
	UpsertAck(ctx context.Context, userID, conversationID string, ackSeq int64) error
}

type PullService struct {
	store PullStorage
}

func NewPullService(store PullStorage) *PullService {
	return &PullService{store: store}
}

// PullMessages 按会话内 seq 拉取消息，返回游标信息。
func (s *PullService) PullMessages(ctx context.Context, conversationID string, cursorSeq int64, limit int) (PullResult, error) {
	if limit <= 0 {
		limit = 50
	}
	// 多查一条用于判断是否还有更多
	msgs, err := s.store.ListMessages(ctx, conversationID, cursorSeq, limit+1)
	if err != nil {
		return PullResult{}, err
	}
	if len(msgs) == 0 {
		return PullResult{NextCursorSeq: cursorSeq, Messages: msgs, HasMore: false}, nil
	}
	hasMore := len(msgs) > limit
	if hasMore {
		msgs = msgs[:limit]
	}
	next := int64(msgs[len(msgs)-1].Seq)

	return PullResult{
		Messages:      msgs,
		NextCursorSeq: next,
		HasMore:       hasMore,
	}, nil
}

// AckConversation 更新用户在会话的 last_ack_seq。
func (s *PullService) AckConversation(ctx context.Context, userID, conversationID string, ackSeq int64) error {
	// TODO: 调用 store.UpsertAck，并确保 ackSeq 回退不覆盖已有较大值（已在仓储层实现）
	return s.store.UpsertAck(ctx, userID, conversationID, ackSeq)
}
