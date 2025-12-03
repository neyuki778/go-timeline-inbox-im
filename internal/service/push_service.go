package service

import (
	"context"
	"reflect"

	"go-im/internal/model"
)

// ConnWriter 抽象 WebSocket 连接的 JSON 写入能力，便于测试替换。
type ConnWriter interface {
	WriteJSON(v interface{}) error
}

// ConnLookup 提供按用户 ID 获取连接的能力。
type ConnLookup interface {
	Get(userID string) ConnWriter
}

// PushService 负责将 OutputPacket 推送到在线用户。
type PushService struct {
	conns ConnLookup
}

func NewPushService(conns ConnLookup) *PushService {
	return &PushService{conns: conns}
}

// Broadcast 将消息推送给 targets 中的用户，最佳努力发送。
func (s *PushService) Broadcast(ctx context.Context, packet model.OutputPacket, targets []string) error {
	// TODO: 遍历 targets，获取连接并 WriteJSON；缺失连接或写失败时继续其他用户，最后返回汇总/首个错误。
	var err error
	for _, target := range targets {
		conn := s.conns.Get(target)
		if conn == nil {
			continue // 连接不存在，跳过
		}
		// 处理“带类型的 nil”场景（接口非 nil，但底层指针为 nil）
		if rv := reflect.ValueOf(conn); rv.Kind() == reflect.Ptr && rv.IsNil() {
			continue
		}
		curErr := conn.WriteJSON(packet)
		if curErr != nil && err == nil {
			err = curErr // 返回首个错误
		}
	}
	return err
}
