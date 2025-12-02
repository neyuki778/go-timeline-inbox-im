# Go-IM 开发环境配置

## 快速启动

### 前置要求
- Docker Desktop for Mac (已安装并运行)
- Go 1.21+ (推荐 1.22)

### 一键启动基础设施

```bash
# 进入项目目录

# 启动 MySQL + Redis
docker-compose up -d

# 查看容器状态
docker-compose ps

# 查看日志（可选）
docker-compose logs -f mysql
```

### 验证服务

```bash
# 验证 MySQL
docker exec -it go-im-mysql mysql -u im_user -pim_pass123 -e "USE go_im; SHOW TABLES;"

# 验证 Redis
docker exec -it go-im-redis redis-cli ping
# 应该返回 PONG
```

### 连接信息

| 服务 | Host | Port | 用户名 | 密码 | 备注 |
|------|------|------|--------|------|------|
| MySQL | localhost | 3306 | im_user | im_pass123 | |
| MySQL (root) | localhost | 3306 | root | root123 | |
| Redis | localhost | 6379 | - | - | |
| RabbitMQ | localhost | 5672 | im_user | im_pass123 | AMQP 协议 |
| RabbitMQ 管理界面 | localhost | 15672 | im_user | im_pass123 | Web UI |

### Go 应用配置示例

```go
// configs/config.go
type Config struct {
    MySQL struct {
        DSN string `default:"im_user:im_pass123@tcp(localhost:3306)/go_im?charset=utf8mb4&parseTime=True&loc=Local"`
    }
    Redis struct {
        Addr string `default:"localhost:6379"`
    }
    RabbitMQ struct {
        URL string `default:"amqp://im_user:im_pass123@localhost:5672/"`
    }
}
```

## 常用命令

```bash
# 停止所有服务
docker-compose down

# 停止并清除数据卷（重置数据库）
docker-compose down -v

# 重启单个服务
docker-compose restart mysql

# 进入 MySQL 命令行
docker exec -it go-im-mysql mysql -u im_user -pim_pass123 go_im

# 进入 Redis 命令行
docker exec -it go-im-redis redis-cli
```

## 阶段二：启用 Redis

Redis 已包含在 docker-compose.yml 中，默认启动。

## 阶段三：启用 RabbitMQ

编辑 `docker-compose.yml`，取消 rabbitmq 相关的注释（包括 volumes 中的 `rabbitmq_data`），然后：

```bash
docker-compose up -d

# 验证 RabbitMQ
docker exec -it go-im-rabbitmq rabbitmq-diagnostics check_running

# 访问管理界面
open http://localhost:15672
# 用户名/密码: im_user / im_pass123
```

## 常见问题

### 1. 端口冲突
如果 3306 或 6379 端口被占用，修改 `docker-compose.yml` 中的端口映射：
```yaml
ports:
  - "3307:3306"  # 改为 3307
```

### 2. 数据持久化
数据存储在 Docker volume 中，即使容器删除数据也不会丢失。
如需完全重置，使用 `docker-compose down -v`。
