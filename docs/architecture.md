# CQIM 架构（现行）

旧文档里的 WuKongIM / TangSengDaoDao / MinIO / `/ws` 已不是主路径。以本页 + 根 [README.md](../README.md) 为准。

## 线上

- 域名：https://wed.imim.chat
- API：`/api`
- WebSocket：`/signal`（不是 `/ws`）
- 入口：EdgeOne → 单源站 Nginx → Node API + Go Gateway

```text
浏览器 / iOS
    → HTTPS /api     → Node (Express + Prisma)
    → WSS /signal    → Go Gateway
                         ↔ Redis（在线、cqim:im:push）
                         ↔ PostgreSQL（schema 已指定；线上切流前可能仍是 SQLite 文件）
```

iOS 原生在仓 `imimchatios`，本仓只有 Web + 后端。

## 栈

| 层 | 组件 |
| --- | --- |
| Web | React + Vite（`cqim-app/client`） |
| 业务 | Node + Express（`cqim-app/server`） |
| 长连接 | Go Gateway |
| ORM | Prisma `provider = postgresql` |
| 缓存 | Redis |
| 通话 | TRTC 1600159677 |
| 加密 | 私聊 encrypted / 群 MLS 方向 |

不做 MTProto。双机用 Redis Pub/Sub，不要共享 SQLite，EdgeOne 不要轮询 `/signal`。

迁库：[cqim-app/docs/MIGRATE_POSTGRES.md](../cqim-app/docs/MIGRATE_POSTGRES.md)
