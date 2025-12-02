package model

import "encoding/json"

// 客户端发给服务器的包
type CmdType int
const (
    CmdHeartbeat CmdType = iota // 心跳
    CmdLogin      // 登录
    CmdChat      // 发送消息
    CmdPull      // 核心：主动拉取消息
    CmdAck       // 消息确认
)

type InputPacket struct {
    Cmd            CmdType         `json:"cmd"`
    MsgId          string          `json:"msg_id,omitempty"`          // 客户端生成的消息唯一ID (幂等)
    ConversationId string          `json:"conversation_id,omitempty"` // 会话ID
    CursorSeq      int64           `json:"cursor_seq,omitempty"`      // ⭐ 游标：从该seq之后开始拉取
    Payload        json.RawMessage `json:"payload,omitempty"`         // 具体数据
}

// 服务端发给客户端的包
type OutputPacket struct {
    Cmd           CmdType     `json:"cmd"`
    Code          int         `json:"code"`                        // 0:成功, 非0:失败
    MsgId         string      `json:"msg_id,omitempty"`            // 对应请求的消息ID
    Seq           int64       `json:"seq,omitempty"`               // 服务端分配的序列号
    NextCursorSeq int64       `json:"next_cursor_seq,omitempty"`   // ⭐ 下次拉取的游标
    HasMore       bool        `json:"has_more,omitempty"`          // ⭐ 是否还有更多消息
    Payload       interface{} `json:"payload,omitempty"`
}