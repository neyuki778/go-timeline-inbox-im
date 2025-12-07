package service

import (
	"context"
	"errors"

	"github.com/redis/go-redis/v9"
)

// SeqGenerator 定义会话内 seq 生成接口，方便对比 Redis 与 MySQL 方案。
type SeqGenerator interface {
	NextSeq(ctx context.Context, conversationID string) (uint64, error)
}

// RedisSeqGenerator 使用 Redis INCR 生成 per-conversation 序号。
type RedisSeqGenerator struct {
	client *redis.Client
	prefix string
}

func NewRedisSeqGenerator(client *redis.Client, prefix string) *RedisSeqGenerator {
	return &RedisSeqGenerator{client: client, prefix: prefix}
}

func (g *RedisSeqGenerator) NextSeq(ctx context.Context, conversationID string) (uint64, error) {
	if g.client == nil {
		return 0, errors.New("redis client is nil")
	}
	key := g.prefix + conversationID
	val, err := g.client.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	return uint64(val), nil
}
