package repository

import (
	"context"
	"errors"

	"go-im/internal/model"

	"github.com/go-sql-driver/mysql"

	"gorm.io/gorm"
)

// MessageRepository 负责消息的持久化。
type MessageRepository struct {
	db *gorm.DB
}

func NewMessageRepository(db *gorm.DB) *MessageRepository {
	return &MessageRepository{db: db}
}

// DB 暴露底层 *gorm.DB，便于测试/复用。
func (r *MessageRepository) DB() *gorm.DB {
	return r.db
}

// SaveMessage 在事务中生成会话内 seq 并写入消息记录。
func (r *MessageRepository) SaveMessage(ctx context.Context, msg *model.TimelineMessage) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 若已有 seq（例如由 Redis 生成），直接尝试写库；否则按会话内最大 seq+1 生成。
		if msg.Seq == 0 {
			var maxSeq uint64
			if err := tx.Raw(
				"SELECT COALESCE(MAX(seq), 0) FROM timeline_message WHERE conversation_id = ? FOR UPDATE",
				msg.ConversationID,
			).Scan(&maxSeq).Error; err != nil {
				return err
			}
			msg.Seq = maxSeq + 1
		}

		if err := tx.Create(msg).Error; err != nil {
			var mysqlErr *mysql.MySQLError
			if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
				return ErrDuplicateMsgID
			}
			return err
		}

		return nil
	})
}

// FindByMsgID 根据 msg_id 查询单条消息，用于幂等返回 seq。
func (r *MessageRepository) FindByMsgID(ctx context.Context, msgID string) (*model.TimelineMessage, error) {
	// TODO: 实现根据 msg_id 查询的逻辑，未找到时返回 gorm.ErrRecordNotFound
	var msg model.TimelineMessage
	err := r.db.WithContext(ctx).
		Where("msg_id = ?", msgID).
		First(&msg).Error
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

// ErrDuplicateMsgID 用于幂等冲突识别。
var ErrDuplicateMsgID = errors.New("duplicate msg_id")
