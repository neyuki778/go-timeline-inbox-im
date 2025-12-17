# Go-Timeline-IM：基于 Go 的即时通讯系统

![](docs/images/lynx.jpg)

一个从零开始构建的现代化即时通讯（IM）系统，采用**读扩散**（Timeline）+ **写扩散**（Inbox）混合模型，支持群聊、单聊及离线消息断点续传。

## ✨ 核心特性

### 架构设计
- **分层架构**：遵循 `golang-standards` 项目结构，职责清晰
- **读扩散 Timeline 模型**：群聊场景下统一存储，按需拉取
- **写扩散 Inbox 模型**：单聊场景下预写用户信箱，秒级查询
- **会话内序列号**：基于 `conversation_id` + `seq` 的复合唯一索引，解决多会话漏拉/误拉问题

### 技术亮点
- **幂等保证**：客户端 `msg_id` + 服务端去重，防止消息重复
- **游标分页**：`cursor_seq` 替代传统 OFFSET，避免翻页重复/遗漏
- **断点续传**：用户离线后基于 `seq` 精准拉取未读消息
- **高性能**：Redis 生成会话内序列号，Sorted Set 实现信箱快速查询
- **最终一致**：Inbox 写入失败不阻塞主链路，进入后台补偿重试（当前为内存重试队列，进程退出会丢任务）
- **生产级可靠性**：事务保证、死锁重试、并发安全连接管理

## 🛠️ 技术栈

| 组件 | 技术选型 | 版本 |
|------|----------|------|
| **开发语言** | Go | 1.21+ |
| **Web 框架** | Gin | v1.9+ |
| **WebSocket** | gorilla/websocket | v1.5+ |
| **数据库** | MySQL | 8.0 |
| **缓存** | Redis | 7.0 |
| **消息队列** | RabbitMQ | 3.12+ |
| **ORM** | GORM | v1.25+ |
| **容器化** | Docker Compose | v3.9+ |

## 📊 系统架构

![架构图](docs/images/架构图.svg)

## 🏗️ 项目迭代过程

本项目采用**分阶段递进**的开发策略，遵循 "Make it Work → Make it Scale" 的工程哲学。

### 阶段一：单机 MVP（已完成）

**目标**：实现基于 MySQL 的读扩散模型，跑通核心流程

#### 关键里程碑
1. **数据库设计**
   - 设计 `timeline_message` 表，采用 `(conversation_id, seq)` 复合唯一索引
   - 引入 `msg_id` 幂等字段，解决客户端重试问题
   - 创建 `user_conversation_state` 表，支持多会话独立 ACK

2. **WebSocket 长连接**
   - 使用 Gin + gorilla/websocket 实现连接升级
   - 实现心跳机制（30s 间隔，2-3 次超时断线）
   - 并发安全的连接管理器（`sync.RWMutex` 保护）

3. **消息收发核心**
   - 事务内生成会话内 `seq`（`SELECT MAX(seq) + 1 FOR UPDATE`）
   - 幂等处理：捕获 MySQL 1062 错误，返回已有 seq
   - 在线推送 + 离线拉取双模式

4. **游标分页**
   - 用 `cursor_seq` 替代传统 OFFSET，避免深分页问题
   - 返回 `next_cursor_seq` 和 `has_more` 标记，支持连续拉取

#### 技术难点与解决方案

| 难点 | 问题 | 解决方案 |
|------|------|----------|
| **会话隔离** | 多会话共享自增 ID 导致漏拉/误拉 | 引入 `(conversation_id, seq)` 复合索引，按会话独立计数 |
| **并发写入** | 多用户同时发送消息，seq 冲突 | 事务 + `FOR UPDATE` 行锁，保证原子性 |
| **连接管理** | Go Map 并发读写 panic | 使用 `sync.RWMutex`，读多写少场景优化 |
| **幂等保证** | 客户端网络抖动重试，消息重复 | 唯一索引 `uk_msg_id`，捕获mySQL错误返回已有结果 |

### 阶段二：Redis 性能优化（已完成）

**目标**：解决 MySQL 性能瓶颈，引入 Inbox 模型支持单聊

#### 关键改进

1. **Redis 序列号生成器**
   ```go
   // 替代 MySQL 事务锁，性能提升 10x+
   func (g *RedisSeqGenerator) NextSeq(ctx context.Context, conversationID string) (uint64, error) {
       key := g.prefix + conversationID  // im:seq:group_101
       return g.client.Incr(ctx, key).Result()
   }
   ```
   - **优势**：无锁、内存操作、支持分布式
   - **持久化**：AOF 保证数据安全

2. **Inbox 写扩散**
   - 使用 Redis Sorted Set 实现单聊信箱
   - Key 设计：`im:inbox:{user_id}`
   - Score：消息 seq（保证有序）
   - Member：JSON 序列化的消息元信息
   
   ```go
   // 单聊消息双写
   // 1. MySQL 归档（全量）
   repo.SaveMessage(msg)
   // 2. Redis 信箱（热数据）
   inbox.Append(msg, []string{"alice", "bob"})
   ```

3. **过期策略**
   - TTL 设置：7 天自动过期
   - 降低 Redis 内存压力
   - 超期消息自动回源 MySQL

#### 性能对比

**基准测试环境**：Apple M4 (10 cores)、MySQL 8.0、Redis 7.0

```bash
# 运行基准测试
go test -bench=HandleChatSeq -benchmem ./internal/service
```

**测试结果**：

| 方案 | 延迟 (ns/op) | 内存 (B/op) | 分配次数 (allocs/op) | 性能提升 |
|------|-------------|------------|---------------------|---------|
| **MySQL seq 生成** | 1,926,873 ns (~1.93ms) | 14,432 | 168 | 基线 |
| **Redis seq 生成** | 1,666,781 ns (~1.67ms) | 10,008 | 104 | **延迟 ↓ 13.5%**<br>**内存 ↓ 30.6%** |
| **Redis seq + Inbox** | 1,755,499 ns (~1.76ms) | 12,808 | 161 | **延迟 ↓ 8.9%**<br>**内存 ↓ 11.3%** |

**关键发现**：
- ✅ Redis 序列号生成比 MySQL 事务锁快 **13.5%**，内存占用减少 **30.6%**
- ✅ 加入 Inbox 写扩散后，延迟仅增加 5%（从 1.67ms → 1.76ms），单聊场景收益巨大

- 📊 Redis 方案支持多线程并发，MySQL 方案受事务锁限制**

### 阶段三：消息队列解耦（已完成）

**目标**：引入 RabbitMQ 实现削峰填谷，提升系统吞吐

#### 核心实现

1. **Producer（生产者）**
   - WebSocket Handler 收到消息后立即发布到 MQ
   - 使用持久化消息（`DeliveryMode: Persistent`）保证可靠性
   - 快速返回响应，不阻塞长连接
   
   ```go
   // 消息入队
   evt := ChatEvent{
       MsgID:          packet.MsgId,
       ConversationID: packet.ConversationId,
       SenderID:       userID,
       Content:        payload.Content,
   }
   producer.PublishChat(ctx, evt)  // 异步发布
   ```

2. **Consumer（消费者）**
   - 独立 goroutine 消费队列消息
   - 执行完整业务流程：seq 生成（Redis）→ 落库（MySQL）→ Inbox 写入 → 在线推送
   - 失败自动重试（`Nack(requeue=true)`），幂等由 `msg_id` 保证
   
   ```go
   // 消费消息
   consumer := NewMessageConsumer(ch, queue, msgSvc)
   consumer.Start(ctx)  // 后台消费
   ```

3. **拓扑设计**
   - **Exchange**：`im.direct`（Direct 类型，精准路由）
   - **Queue**：`im.msg.process`（持久化队列）
   - **Routing Key**：`msg.send`
   - 通过环境变量配置，支持测试隔离

4. **灵活开关**
   - 环境变量 `IM_USE_RMQ=0` 可关闭 MQ，回退到直落库模式
   - 方便开发调试和渐进式上线

#### 架构收益

| 对比项 | 直落库（阶段二） | 消息队列（阶段三） |
|--------|-----------------|-------------------|
| **响应时延** | ~1.7ms（阻塞等待 MySQL） | < 1ms（仅入队） |
| **峰值吞吐** | 受 MySQL 写入速度限制 | 队列缓冲，平滑处理 |
| **系统解耦** | Gateway 与存储紧耦合 | 生产/消费独立扩展 |
| **失败处理** | 同步返回错误 | 自动重试 + 死信队列(拓展) |
| **可观测性** | 日志 | MQ 管理界面 + 监控 |

#### 测试覆盖

**集成测试**：`TestRabbitMQPipelineIntegration`
- 验证完整链路：入队 → 消费 → 落库 → Inbox → 数据一致性
- 覆盖场景：正常消息、幂等重复、并发消费
- 需要本地 MySQL + Redis + RabbitMQ 环境

### 数据流

**阶段三（当前）- 异步消息队列模式**：
1. **发送消息**：客户端 → WebSocket → **入队 RabbitMQ**（快速返回）→ Consumer 消费 → 生成 seq（Redis）→ 写 MySQL（Timeline）→ 写 Inbox（Redis）→ 推送在线用户
2. **离线拉取**：客户端重连 → 带 `cursor_seq` 请求 → 查询 MySQL/Redis → 返回消息列表 + 下一游标

**阶段二（兼容）- 直落库模式**：
- 设置 `IM_USE_RMQ=0` 后回退到同步处理：WebSocket → 生成 seq → 写库 → 写 Inbox → 推送

## 🚀 快速开始

### 前置要求
- Go 1.21+
- Docker & Docker Compose

### 一键启动

```bash
# 1. 克隆项目
git clone https://github.com/neyuki778/go-inbox-im.git
cd go-im

# 2. 启动基础设施（MySQL + Redis + RabbitMQ）
docker-compose up -d

# 3. 等待服务就绪（约 30 秒）
docker-compose ps

# 4. 验证数据库
docker exec -it go-im-mysql mysql -u im_user -pim_pass123 \
  -e "USE go_im; SHOW TABLES;"

# 5. 安装依赖
go mod download

# 6. 运行服务
go run cmd/server/main.go
```

服务启动后监听 `http://localhost:8080`。

### 环境配置

| 服务 | 地址 | 端口 | 用户名 | 密码 |
|------|------|------|--------|------|
| MySQL | localhost | 8848 | im_user | im_pass123 |
| Redis | localhost | 6379 | - | - |
| RabbitMQ | localhost | 5672 / 15672 (管理界面) | im_user | im_pass123 |
| WebSocket | localhost | 8080 | - | - |

**RabbitMQ 管理界面**：http://localhost:15672

## 🧪 测试

```bash
# 运行所有测试
go test ./internal/...

# 运行指定包测试
go test ./internal/service -v

# 测试覆盖率
go test ./internal/... -cover
```

### 测试覆盖
- ✅ **单元测试**：Repository、Service 层 Mock 测试
- ✅ **集成测试**：Redis + MySQL 端到端验证
- ✅ **幂等测试**：重复 `msg_id` 场景覆盖

## 📡 协议示例

### 连接
```bash
# 客户端通过 WebSocket 连接
ws://localhost:8080/ws?user_id=alice
```

### 心跳
```json
// 客户端 → 服务端
{"cmd": 0}

// 服务端 → 客户端
{"cmd": 0, "code": 0}
```

### 发送消息
```json
// 客户端 → 服务端
{
  "cmd": 2,
  "msg_id": "uuid-client-generated",
  "conversation_id": "group_101",
  "payload": {"content": "Hello World", "msg_type": 1}
}

// 服务端 → 客户端
{
  "cmd": 2,
  "code": 0,
  "msg_id": "uuid-client-generated",
  "seq": 1001
}
```

### 拉取离线消息
```json
// 客户端 → 服务端
{
  "cmd": 3,
  "conversation_id": "group_101",
  "cursor_seq": 100
}

// 服务端 → 客户端
{
  "cmd": 3,
  "code": 0,
  "payload": [
    {"msg_id": "...", "seq": 101, "content": "..."},
    {"msg_id": "...", "seq": 102, "content": "..."}
  ],
  "next_cursor_seq": 102,
  "has_more": true
}
```

## 🗂️ 数据库设计

### timeline_message（消息主表）
| 字段 | 类型 | 说明 |
|------|------|------|
| id | BIGINT | 主键（自增） |
| msg_id | VARCHAR(64) | 客户端生成的唯一 ID（幂等） |
| conversation_id | VARCHAR(64) | 会话 ID |
| seq | BIGINT | **会话内序列号**（核心） |
| sender_id | VARCHAR(64) | 发送者 ID |
| content | VARCHAR(4096) | 消息内容 |
| msg_type | TINYINT | 1:文本, 2:图片 |
| send_time | BIGINT | 发送时间戳 |

**索引**：
- `UNIQUE(msg_id)` → 幂等去重
- `UNIQUE(conversation_id, seq)` → 保证会话内 seq 唯一
- `INDEX(conversation_id, seq)` → 范围拉取性能优化

### user_conversation_state（ACK 位点表）
| 字段 | 类型 | 说明 |
|------|------|------|
| user_id | VARCHAR(64) | 用户 ID |
| conversation_id | VARCHAR(64) | 会话 ID |
| last_ack_seq | BIGINT | 最后确认的序列号 |
| updated_at | TIMESTAMP | 更新时间 |

**主键**：`(user_id, conversation_id)`

## 📈 性能优化

### 已实现
- ✅ Redis 生成序列号，避免 MySQL 自增锁竞争
- ✅ 单聊消息走 Inbox（Redis Sorted Set），查询耗时 < 5ms
- ✅ Inbox 写入失败后台补偿重试（最佳努力）
- ✅ 游标分页避免深分页性能问题
- ✅ 消息内容限制 4KB，防止超大消息影响传输
- ✅ RabbitMQ 削峰填谷，WebSocket 响应时延 < 1ms

### 未来规划
- 🔲 大群（>500人）动态切换读扩散
- 🔲 消息补洞机制（检测 seq 不连续）
- 🔲 CDN 支持图片/文件消息
- 🔲 水平扩展：多实例 Gateway + 统一会话管理

## 📝 代码结构

```
.
├── cmd/
│   └── server/
│       └── main.go                 # 入口：依赖注入与启动
├── internal/
│   ├── handler/
│   │   └── websocket.go            # WebSocket 握手与消息路由
│   ├── service/
│   │   ├── message_service.go      # 消息处理核心逻辑
│   │   ├── message_producer.go     # RabbitMQ 生产者
│   │   ├── message_consumer.go     # RabbitMQ 消费者
│   │   ├── seq_generator.go        # Redis 序列号生成器
│   │   ├── inbox_service.go        # Inbox 写扩散
│   │   ├── push_service.go         # 在线推送
│   │   ├── pull_service.go         # 离线拉取
│   │   └── connection_manager.go   # 连接管理
│   ├── repository/
│   │   ├── message_repository.go   # 消息持久化
│   │   └── pull_repository.go      # 拉取查询
│   ├── model/
│   │   ├── message.go              # 数据模型
│   │   └── protocol.go             # 协议定义
│   └── infra/
│       ├── redis_client.go         # Redis 连接
│       └── rabbitmq.go             # RabbitMQ 连接与拓扑
├── scripts/
│   └── init.sql                    # 数据库初始化脚本
├── docker-compose.yml              # 基础设施编排
└── go.mod                          # 依赖管理
```

## 🔧 简单故障排查

### WebSocket 连接失败
```bash
# 检查服务是否启动
curl http://localhost:8080/ws

# 查看服务日志
docker-compose logs -f
```

### MySQL 连接超时
```bash
# 确认 MySQL 就绪
docker exec -it go-im-mysql mysqladmin ping -h localhost -u root -proot123

# 查看端口占用
lsof -i :8848
```

### Redis 连接失败
```bash
# 测试 Redis 连通性
docker exec -it go-im-redis redis-cli ping
```
## 参考

[阿里云官方文档](https://help.aliyun.com/zh/tablestore/use-cases/message-system-architecture-in-the-modern-im-system)
