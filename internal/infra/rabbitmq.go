package infra

import (
	"os"

	amqp "github.com/rabbitmq/amqp091-go"
)

// RabbitMQConfig 描述 MQ 的连接与拓扑信息。
type RabbitMQConfig struct {
	URL        string
	Exchange   string
	Queue      string
	RoutingKey string
}

// LoadRabbitMQConfig 从环境变量加载配置，提供合理默认值。
func LoadRabbitMQConfig() RabbitMQConfig {
	url := os.Getenv("IM_RMQ_URL")
	if url == "" {
		url = "amqp://im_user:im_pass123@localhost:5672/"
	}
	exchange := os.Getenv("IM_RMQ_EXCHANGE")
	if exchange == "" {
		exchange = "im.direct"
	}
	queue := os.Getenv("IM_RMQ_QUEUE")
	if queue == "" {
		queue = "im.msg.process"
	}
	routingKey := os.Getenv("IM_RMQ_ROUTING_KEY")
	if routingKey == "" {
		routingKey = "msg.send"
	}

	return RabbitMQConfig{
		URL:        url,
		Exchange:   exchange,
		Queue:      queue,
		RoutingKey: routingKey,
	}
}

// NewRabbitMQ 建立连接并返回 Connection。
func NewRabbitMQ(cfg RabbitMQConfig) (*amqp.Connection, error) {
	return amqp.Dial(cfg.URL)
}

// PrepareRabbitTopology 在指定 channel 上声明交换机、队列并绑定（幂等）。
func PrepareRabbitTopology(ch *amqp.Channel, cfg RabbitMQConfig) error {
	if err := ch.ExchangeDeclare(
		cfg.Exchange,
		"direct",
		true,  // durable
		false, // autoDelete
		false, // internal
		false, // noWait
		nil,
	); err != nil {
		return err
	}

	q, err := ch.QueueDeclare(
		cfg.Queue,
		true,  // durable
		false, // autoDelete
		false, // exclusive
		false, // noWait
		nil,
	)
	if err != nil {
		return err
	}

	if err := ch.QueueBind(
		q.Name,
		cfg.RoutingKey,
		cfg.Exchange,
		false,
		nil,
	); err != nil {
		return err
	}

	return nil
}
