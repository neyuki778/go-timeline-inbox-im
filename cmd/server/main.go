package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	amqp "github.com/rabbitmq/amqp091-go"

	"go-im/internal/handler"
	"go-im/internal/infra"
	"go-im/internal/repository"
	"go-im/internal/service"
)

const serverAddr = ":8080"

func main() {
	// 构建依赖
	ctx := context.Background()
	db, err := repository.NewDB()
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}
	redisClient := infra.NewRedisClient()
	if err := infra.PingRedis(ctx, redisClient); err != nil {
		log.Fatalf("Redis 未就绪，启动失败: %v", err)
	}
	var (
		rabbitConn     *amqp.Connection
		rabbitPubCh    *amqp.Channel
		rabbitSubCh    *amqp.Channel
		consumerCancel context.CancelFunc
	)

	connManager := service.NewConnectionManager()
	msgRepo := repository.NewMessageRepository(db)
	seqGen := service.NewRedisSeqGenerator(redisClient, "im:seq:")
	inbox := service.NewRedisInboxWriter(redisClient, "im:inbox:", 7*24*time.Hour)
	msgSvc := service.NewMessageServiceWithSeq(msgRepo, seqGen).WithInbox(inbox)

	// 初始化 RabbitMQ（可通过 IM_USE_RMQ=0 关闭；默认启用，失败直接退出）
	var producer *service.MessageProducer
	if mqEnabled() {
		mqCfg := infra.LoadRabbitMQConfig()
		rabbitConn, err = infra.NewRabbitMQ(mqCfg)
		if err != nil {
			log.Fatalf("连接 RabbitMQ 失败: %v", err)
		}

		rabbitPubCh, err = rabbitConn.Channel()
		if err != nil {
			log.Fatalf("创建 RabbitMQ 发布 channel 失败: %v", err)
		}
		if err := infra.PrepareRabbitTopology(rabbitPubCh, mqCfg); err != nil {
			log.Fatalf("声明 RabbitMQ 拓扑失败: %v", err)
		}
		producer = service.NewMessageProducer(rabbitPubCh, mqCfg.Exchange, mqCfg.RoutingKey)

		// 消费端使用独立 channel
		rabbitSubCh, err = rabbitConn.Channel()
		if err != nil {
			log.Fatalf("创建 RabbitMQ 消费 channel 失败: %v", err)
		}
		if err := infra.PrepareRabbitTopology(rabbitSubCh, mqCfg); err != nil {
			log.Fatalf("声明 RabbitMQ 拓扑失败: %v", err)
		}
		consumer := service.NewMessageConsumer(rabbitSubCh, mqCfg.Queue, msgSvc)
		var consumeCtx context.Context
		consumeCtx, consumerCancel = context.WithCancel(context.Background())
		if err := consumer.Start(consumeCtx); err != nil {
			log.Fatalf("启动 RabbitMQ 消费者失败: %v", err)
		}
		log.Printf("RabbitMQ 已启用，交换机=%s 队列=%s", mqCfg.Exchange, mqCfg.Queue)
	} else {
		log.Printf("已关闭 RabbitMQ，使用直落库路径")
	}

	wsHandler := handler.NewWebSocketHandler(connManager, msgSvc).WithProducer(producer)

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

	if consumerCancel != nil {
		consumerCancel()
	}
	if rabbitSubCh != nil {
		_ = rabbitSubCh.Close()
	}
	if rabbitPubCh != nil {
		_ = rabbitPubCh.Close()
	}
	if rabbitConn != nil {
		_ = rabbitConn.Close()
	}
	log.Println("服务已关闭")
}

// mqEnabled 返回是否启用 RabbitMQ（默认启用，IM_USE_RMQ=0/false 关闭）。
func mqEnabled() bool {
	val := strings.ToLower(os.Getenv("IM_USE_RMQ"))
	return val == "" || val == "1" || val == "true" || val == "yes"
}
