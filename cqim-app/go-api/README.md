# Go API

独立 Go 1.22+ HTTP 服务，使用标准库 `net/http`、`pgx` 和 `go-redis`。它与现有 Node 服务共用 PostgreSQL、Redis 与 `UserSession`。没有修改 Node 或 Prisma。Compose 会启动 `go-api`（容器内 `8089`），但 **默认 Nginx 仍把全部 `/api` 交给 Node `cqim:3000`**；生产未自动切流。

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
NODE_API_BASE='<Node API base URL>'
GO_API_BASE='<Go API base URL>'
curl -i -H "Authorization: Bearer $TOKEN" "$NODE_API_BASE/api/auth/me"
curl -i -H "Authorization: Bearer $TOKEN" "$GO_API_BASE/api/me"

curl -i -H "Authorization: Bearer $TOKEN" "$NODE_API_BASE/api/friend/list"
curl -i -H "Authorization: Bearer $TOKEN" "$GO_API_BASE/api/friend/list"

curl -i -H "Authorization: Bearer $TOKEN" "$NODE_API_BASE/api/chat/list"
curl -i -H "Authorization: Bearer $TOKEN" "$GO_API_BASE/api/chat/list"
```

健康检查：

```bash
curl -i "$GO_API_BASE/api/health"
```

## 已实现（阶段 A/B/C）

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
| POST | `/api/apns/token` | 注册或更新 APNs alert token |
| POST | `/api/apns/voip-token` | 注册或更新 APNs VoIP token |
| DELETE | `/api/apns/token` | 按 Node 同样范围删除当前用户 token |

私聊发送仅接受 `msgType=encrypted`。成功写入后会更新 `Chat.lastMessageAt`、将会话预览固定为 `🔒 [加密消息]`，并向 Redis `cqim:im:push` 发布与 Node 相同的 `{ userId, payload }` 信封，供仍在运行的网关投递。

离线 APNs 与 Web Push 已由 Go API 真实发送：APNs 按设备的 sandbox/production 环境与 alert/VoIP topic 发送，失效设备仅删除对应行；Web Push 使用 VAPID 并会清理失效订阅。私聊唤醒通知只带会话/消息路由数据，不包含明文。缺少 APNs P8 配置时会记录 `no_p8_keys`，不会报告发送成功。不会接入个推。

## 未迁移（阶段 C/D）

媒体/MinIO、认证注册登录验证码、群聊、朋友圈、贴纸、二维码、管理后台及其余 `/api/*` 路由尚未迁移。

私聊中 Node 仍有、Go 未实现的接口也保持走 Node：`DELETE /api/chat/{chatId}`、`POST /api/chat/{chatId}/read`、`POST /api/chat/{chatId}/recall/{messageId}`。

Node `cqim-app/server` 保持可部署。默认 Nginx 未切流；生产流量仍由 Node 处理全部 `/api`，除非按下一节 **显式复制** 可选切流配置。

## 如何切 / 如何改回 Node

只改本机 Compose 挂载的 `nginx/default.conf`，不会部署到生产。

切到已实现的 Go 路由：

```bash
cd cqim-app
cp nginx/default.go-api-cutover.conf nginx/default.conf
docker compose up -d --build go-api nginx
```

改回 Node（全部 `/api` 再走 `cqim:3000`）：

```bash
cd cqim-app
cp nginx/default.node-all-api.conf nginx/default.conf
docker compose up -d nginx
```

可选停掉 Go 进程：`docker compose stop go-api`。若当前 `default.conf` 已是 HTTPS 版，改用 `https.go-api-cutover.conf` 切流、用 `https.conf` 改回。

### Nginx location 清单（仅切流配置生效时）

| location | 上游 |
| --- | --- |
| `/api/health`、`/api/me`、`/api/presence`、`/api/friend`、`/api/apns` | `go-api:8089` |
| `/api/chat/create`、`/api/chat/list`、`/api/chat/send` | `go-api:8089` |
| `/api/chat/{chatId}/messages` | `go-api:8089` |
| `GET /api/chat/{chatId}` | `go-api:8089` |
| `DELETE /api/chat/{chatId}` 以及 `/api/chat/{chatId}/read`、`/recall` | `cqim:3000` |
| 其余 `/api`（含 `/api/admin`、`/api/moments`、`/api/media`、`/api/auth`、群聊、贴纸、二维码、Web Push） | `cqim:3000` |
| `/signal` | **不变**，仍 `cqim:3000`（本仓库 Go 网关不处理 `/signal`） |
| `/ws` | **不变**，仍 `go-gateway:8081` |

不要把 `/api/admin`、`/api/moments`、`/api/media` 指到 go-api：这些路由尚未实现。路径是 `/api/friend`，不是 `/api/friends`。`/api/me` 使用边界匹配，不会把 `/api/media` 切走。
