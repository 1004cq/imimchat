# NeoMsg

> 类 Telegram 高性能即时通讯系统 — 客户端-服务器架构，Protobuf 二进制协议，云同步 + 可选端到端加密。

**完整技术方案** → [docs/ARCHITECTURE.md](./docs/ARCHITECTURE.md)

## 快速启动

```bash
cd neomsg/deploy
cp .env.example .env
docker compose up -d
```

| 服务 | 端口 | 说明 |
|------|------|------|
| API Gateway | 8080 | REST + WebSocket |
| TCP Gateway | 5222 | 移动端长连接 |
| PostgreSQL | 5432 | 业务数据 |
| Redis | 6379 | 会话/在线/限流 |
| MinIO | 9000 | 媒体存储 |
| NATS | 4222 | 消息队列 |

## 目录结构

```
neomsg/
├── docs/ARCHITECTURE.md    # 完整技术方案（8 章节）
├── proto/                  # Protobuf 协议定义
├── backend/                # Go 微服务
├── clients/ios/            # Swift + SwiftUI 客户端
└── deploy/                 # Docker Compose
```

## 技术栈

| 层 | 选型 |
|----|------|
| 后端 | Go 1.22+ |
| iOS | Swift 5.9 + SwiftUI |
| 协议 | Protobuf 3 + WebSocket/TCP |
| 数据库 | PostgreSQL 16 + Redis 7 |
| 存储 | MinIO (S3 兼容) |
| 队列 | NATS JetStream |
| E2EE | X3DH + Double Ratchet (Secret Chat) |
