package repository

import (
	"context"
	"errors"

	"go-im/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SeqRepository 使用 MySQL 生成会话内 seq（现有方案），便于与 Redis 方案对比。
type SeqRepository struct {
	db *gorm.DB
}

func NewSeqRepository(db *gorm.DB) *SeqRepository {
	return &SeqRepository{db: db}
}

// NextSeq 在事务中获取会话内最大 seq+1。
func (r *SeqRepository) NextSeq(ctx context.Context, conversationID string) (uint64, error) {
	if conversationID == "" {
		return 0, errors.New("conversationID required")
	}
	var seq uint64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var maxSeq uint64
		if err := tx.Model(&model.TimelineMessage{}).
			Select("COALESCE(MAX(seq),0)").
			Where("conversation_id = ?", conversationID).
			// 锁定行，防止并发竞争；若无匹配行，InnoDB 会锁间隙
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Scan(&maxSeq).Error; err != nil {
			return err
		}
		seq = maxSeq + 1
		return nil
	})
	return seq, err
}
