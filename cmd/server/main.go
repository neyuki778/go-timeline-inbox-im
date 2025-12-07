package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"go-im/internal/handler"
	"go-im/internal/infra"
	"go-im/internal/repository"
	"go-im/internal/service"
)

const serverAddr = ":8080"

func main() {
	// 构建依赖
	db, err := repository.NewDB()
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}
	redisClient := infra.NewRedisClient()
	if err := infra.PingRedis(context.Background(), redisClient); err != nil {
		log.Printf("Redis 未就绪，将回退 MySQL seq 方案: %v", err)
		redisClient = nil
	}

	connManager := service.NewConnectionManager()
	msgRepo := repository.NewMessageRepository(db)
	var seqGen service.SeqGenerator
	if redisClient != nil {
		seqGen = service.NewRedisSeqGenerator(redisClient, "im:seq:")
	}
	msgSvc := service.NewMessageServiceWithSeq(msgRepo, seqGen)
	wsHandler := handler.NewWebSocketHandler(connManager, msgSvc)

	// 初始化 Gin，引入基础日志与 panic 恢复
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	// WebSocket 路由；REST API 可在 /api 组下扩展
	router.GET("/ws", wsHandler.HandleWebSocket)

	httpServer := &http.Server{
		Addr:    serverAddr,
		Handler: router,
	}

	go func() {
		log.Printf("WebSocket/Gin 服务启动，监听 %s", serverAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务启动失败: %v", err)
		}
	}()

	// 监听系统信号，优雅退出
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("服务关闭异常: %v", err)
	}
	log.Println("服务已关闭")
}
