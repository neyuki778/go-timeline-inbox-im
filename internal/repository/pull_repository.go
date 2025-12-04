package repository

import (
	"context"
	"errors"

	"go-im/internal/model"

	"gorm.io/gorm"
)

// PullRepository 提供离线拉取和 ACK 所需的数据访问。
type PullRepository struct {
	db *gorm.DB
}
 
func NewPullRepository(db *gorm.DB) *PullRepository {
	return &PullRepository{db: db}
}

// DB 暴露底层 *gorm.DB，便于测试/复用。
func (r *PullRepository) DB() *gorm.DB {
	return r.db
}

// ListMessages 按会话内 seq 拉取消息，返回升序列表。
func (r *PullRepository) ListMessages(ctx context.Context, conversationID string, afterSeq int64, limit int) ([]model.TimelineMessage, error) {
	// TODO: 实现基于 conversation_id + seq 的范围查询，ORDER BY seq ASC，LIMIT ?
	if conversationID == "" {
		return nil, errors.New("conversationID cannot be empty")
	}
	var messages []model.TimelineMessage
	err := r.db.WithContext(ctx).
		Where("conversation_id = ? AND seq > ?", conversationID, afterSeq).
		Order("seq ASC").Limit(limit).Find(&messages).Error
	if err != nil {
		return nil, err
	}
	return messages, nil
}

// UpsertAck 插入或更新用户在会话的 last_ack_seq，ackSeq 只有在更大时才更新。
func (r *PullRepository) UpsertAck(ctx context.Context, userID, conversationID string, ackSeq int64) error {
	// TODO: 实现 user_conversation_state 的插入/更新逻辑
	if userID == "" || conversationID == "" {
		return errors.New("userId and conversationId required")
	}
	return r.db.WithContext(ctx).Exec(`
	INSERT INTO user_conversation_state (user_id, conversation_id, last_ack_seq)
	VALUES (?, ?, ?)
	ON DUPLICATE KEY UPDATE last_ack_seq = GREATEST(last_ack_seq, VALUES(last_ack_seq))
	`, userID, conversationID, ackSeq).Error
}
