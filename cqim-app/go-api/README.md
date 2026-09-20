# Go API

独立 Go 1.22+ HTTP 服务，使用标准库 `net/http`、`pgx` 和 `go-redis`。它与现有 Node 服务共用 PostgreSQL、Redis 与 `UserSession`，默认监听 `127.0.0.1:8089`。没有修改 Node、Nginx 或 Prisma，默认 `/api` 仍由 Node 处理。

## 编译与启动

在 `cqim-app` 目录使用与 Node 相同的 `.env`：

```bash
set -a
. ./.env
set +a
go build -o ./go-api/bin/go-api ./go-api/cmd/go-api
./go-api/bin/go-api
```

读取 `DATABASE_URL`、`REDIS_URL`（或 `REDIS_ADDR`）。`GO_API_LISTEN_ADDR` 可覆盖监听地址。

## 与 Node 对拍

Node 当前用户鉴权使用 `Authorization: Bearer <UserSession.token>`；没有另一套 Cookie/JWT 用户 token 规则。对同一个 token 可分别请求 Node 与 Go：

```bash
TOKEN='<现有 UserSession token>'
curl -i -H "Authorization: Bearer $TOKEN" http://127.0.0.1:3000/api/auth/me
curl -i -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8089/api/me

curl -i -H "Authorization: Bearer $TOKEN" http://127.0.0.1:3000/api/friend/list
curl -i -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8089/api/friend/list

curl -i -H "Authorization: Bearer $TOKEN" http://127.0.0.1:3000/api/chat/list
curl -i -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8089/api/chat/list
```

健康检查：

```bash
curl -i http://127.0.0.1:8089/api/health
```

## 已实现（阶段 A/B）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/health` | PostgreSQL、Redis 依赖状态 |
| GET | `/api/me` | UserSession 当前用户 |
| GET/POST | `/api/presence` | Redis `user:presence:`、`user:activeChat:`，TTL 90 秒 |
| POST | `/api/friend/request` | 好友申请（含双向申请自动接受） |
| GET | `/api/friend/requests` | 收到/发出的申请 |
| POST | `/api/friend/accept/{requestId}` | 接受并创建私聊会话 |
| POST | `/api/friend/reject/{requestId}` | 拒绝申请 |
| GET | `/api/friend/list` | 好友列表 |
| DELETE | `/api/friend/{friendId}` | 删除好友 |
| GET | `/api/friend/check/{userId}` | 好友及待处理申请状态 |
| POST | `/api/chat/create` | 获取或创建私聊会话 |
| GET | `/api/chat/list` | 会话列表；加密消息不泄露信封预览 |
| GET | `/api/chat/{chatId}` | 会话详情 |
| POST | `/api/chat/send` | JSON 加密私聊发送入口 |
| POST | `/api/chat/{chatId}/messages` | 加密私聊消息写入 |
| GET | `/api/chat/{chatId}/messages` | 私聊历史分页，仅返回存储的信封字段 |

私聊发送仅接受 `msgType=encrypted`。成功写入后会更新 `Chat.lastMessageAt`、将会话预览固定为 `🔒 [加密消息]`，并向 Redis `cqim:im:push` 发布与 Node 相同的 `{ userId, payload }` 信封，供仍在运行的网关投递。

离线 APNs/Web Push 在此阶段**仍由 Node 执行**；Go 不会假实现推送，也不会接入个推。

## 未迁移（阶段 C/D）

媒体/MinIO、APNs、Web Push、认证注册登录验证码、群聊、朋友圈、贴纸、二维码、管理后台及其余 `/api/*` 路由尚未迁移。

Nginx 未切流；Node `cqim-app/server` 保持可部署，默认 `/api` 没有切到 `:8089`。
