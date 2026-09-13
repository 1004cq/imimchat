# CQIM 部署

与 [README](../../README.md) 一致。Prisma 主库不是 Mongo。

## 组件

- Nginx：TLS、`/api`、`/signal`
- Node API + Go Gateway
- **PostgreSQL**（`DATABASE_URL=postgresql://...`）
- Redis
- MySQL 仅审计（可选）

线上若仍指 `file:./dev.db`，运行的仍是 SQLite，见 MIGRATE_POSTGRES.md。

## 起动

```bash
cd cqim-app
cp .env.example .env
# 填强密码、postgresql DATABASE_URL、PUBLIC_BASE_URL=https://wed.imim.chat
docker compose up -d --build
```

验证：`https://wed.imim.chat/api/health` 与 `wss://wed.imim.chat/signal`。

库端口不要对公网。双机见 TWO_NODES.md。
