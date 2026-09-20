# Go API 骨架

本目录是独立的 Go 1.22+ HTTP 服务，使用标准库 `net/http`、`pgx` 连接 PostgreSQL、`go-redis` 连接 Redis。它只提供健康检查和与现有 Node 服务对拍的当前用户接口；不会替代 Node 服务，也没有切换 Nginx 的默认 `/api`。

## 编译与启动

在 `cqim-app` 目录执行（与现有 Node 服务使用同一个 `.env`）：

```bash
set -a
. ./.env
set +a
go build -o ./go-api/bin/go-api ./go-api/cmd/go-api
./go-api/bin/go-api
```

默认监听 `127.0.0.1:8089`。可用 `GO_API_LISTEN_ADDR` 覆盖监听地址。健康检查同时读取 `DATABASE_URL` 与 `REDIS_URL`（或 `REDIS_ADDR`）。

## 验证

```bash
curl -i http://127.0.0.1:8089/api/health
curl -i -H "Authorization: Bearer <现有用户 token>" http://127.0.0.1:8089/api/me
```

`/api/me` 与 Node 的 `/api/auth/me` 使用相同的 `UserSession.token`：Node 当前用户路由只接受 `Authorization: Bearer`，没有用于该 token 的 Cookie 规则。Go 服务同样查 `UserSession` 与 `User`，并检查会话过期和用户封禁状态；它不使用或新增 JWT 密钥。

## 路由

- `GET /api/health`：返回 PostgreSQL 与 Redis 可用性；两者都可用时为 `200`，否则为 `503`。
- `GET /api/me`：要求现有 Bearer 用户 token；响应保持 Node 的 `{ "user": { ... } }` 结构。

未修改 `docker-compose.yml`、Nginx 或 Node 路由；因此该服务默认不在 Compose 中启动，也不会接管默认 `/api`。
