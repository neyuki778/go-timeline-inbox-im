package infra

import (
	"context"
	"os"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// NewRedisClient 基于环境变量创建 Redis 客户端。
// 使用的环境变量：
//
//	IM_REDIS_ADDR   例：localhost:6379
//	IM_REDIS_PASS   例：password，可为空
//	IM_REDIS_DB     例：0（整数）
func NewRedisClient() *redis.Client {
	addr := os.Getenv("IM_REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379" // 默认指向 docker-compose 暴露的本机端口
	}
	db := 0
	if v := os.Getenv("IM_REDIS_DB"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			db = parsed
		}
	}
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     os.Getenv("IM_REDIS_PASS"),
		DB:           db,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
		DialTimeout:  2 * time.Second,
	})
	return client
}

// PingRedis 用于启动阶段验证连接；若 client 为 nil 则直接返回 nil。
func PingRedis(ctx context.Context, client *redis.Client) error {
	if client == nil {
		return nil
	}
	_, err := client.Ping(ctx).Result()
	return err
}
