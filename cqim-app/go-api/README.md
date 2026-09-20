# Go API

独立 Go 1.22+ HTTP 服务，使用标准库 `net/http`、`pgx` 和 `go-redis`。它与现有 Node 服务共用 PostgreSQL、Redis 与 `UserSession`。没有修改 Node、Nginx 或 Prisma，默认 `/api` 仍由 Node 处理。

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
| DELETE | `/api/chat/{chatId}` | 隐藏会话 |
| POST | `/api/chat/send` | JSON 加密私聊发送入口 |
| POST | `/api/chat/{chatId}/messages` | 加密私聊消息写入 |
| GET | `/api/chat/{chatId}/messages` | 私聊历史分页，仅返回存储的信封字段 |
| POST | `/api/chat/{chatId}/read` | 标记对方消息已读 |
| POST | `/api/chat/{chatId}/recall/{messageId}` | 两分钟内撤回自己的消息 |
| POST | `/api/auth/login` | UserSession 密码登录 |
| POST | `/api/auth/register` | 基础账号注册 |
| GET | `/api/auth/me` | 当前用户信息 |
| POST | `/api/auth/logout` | 删除当前会话 |
| POST/DELETE | `/api/web-push/subscription` | 保存/删除 VAPID 订阅 |
| GET | `/api/web-push/public-key` | VAPID 公钥 |
| GET | `/api/stickers/packs`, `/api/stickers/discover`, `/api/stickers/search`, `/api/stickers/status` | 兼容基础 JSON 响应 |
| POST | `/api/apns/token` | 注册或更新 APNs alert token |
| POST | `/api/apns/voip-token` | 注册或更新 APNs VoIP token |
| DELETE | `/api/apns/token` | 按 Node 同样范围删除当前用户 token |
| POST | `/api/media/upload` | 认证后上传 base64 媒体到 MinIO，并仅写入 `MediaFile` 元数据 |
| POST | `/api/media/upload-form` | 认证后 multipart 上传媒体到 MinIO，并仅写入 `MediaFile` 元数据 |
| GET | `/api/media/:id` | 按 `MediaFile.id` 从 MinIO 下载媒体 |
| POST | `/api/group/send` | MLS 加密群消息发送（仅 `mls_encrypted`/`system`） |
| GET | `/api/group/messages` | 群消息历史分页，返回存储信封 |
| POST | `/api/group/ack` | 群消息已读游标 |
| GET | `/api/group/unread` | 群未读计数 |
| POST | `/api/group/create` | 创建群组 |
| GET | `/api/group/list` | 当前用户群列表 |
| GET | `/api/group/info` | 群资料 |
| GET | `/api/group/members` | 群成员分页 |
| POST | `/api/group/join` | 加入群组 |
| GET | `/api/group/search` | 群搜索 |
| POST/GET | `/api/group/invite/create`, `/api/group/invite/list` | 邀请链接 |
| POST | `/api/group/invite/revoke`, `/api/group/invite/join` | 撤销/使用邀请链接 |
| GET | `/api/group/peer/resolve` | dialogId 解析 |
| POST | `/api/group/username/set` | 设置群公开用户名 |
| PUT | `/api/group/update/name`, `/api/group/update/avatar`, `/api/group/update/username`, `/api/group/update` | 群资料更新 |
| POST | `/api/group/leave`, `/api/group/kick`, `/api/group/transfer`, `/api/group/admin/set` | 成员管理 |
| PUT | `/api/group/announcement`, `/api/group/member/nickname` | 公告/群昵称 |
| POST | `/api/group/mute`, `/api/group/dissolve` | 禁言/解散 |
| POST | `/api/group/qrcode`, `/api/group/invite-members` | 群二维码/批量邀请 |
| GET/POST | `/api/group/invites`, `/api/group/invite-accept/:inviteId`, `/api/group/invite-reject/:inviteId` | 群邀请处理 |
| POST | `/api/group/burn-message` | 群阅后即焚销毁通知 |
| GET | `/api/moments/feed`, `/api/moments`, `/api/moments/my`, `/api/moments/:id` | 好友 Feed、公开列表、我的动态、详情与可见性过滤 |
| POST | `/api/moments`, `/api/moments/:id/like`, `/api/moments/:id/comments`, `/api/moments/:id/pin` | 发布、点赞、评论、置顶 |
| PUT | `/api/moments/:id`, `/api/moments/reorder` | 编辑可见性/内容/位置、排序 |
| DELETE | `/api/moments/:id`, `/api/moments/:momentId/comments/:commentId` | 删除动态或评论 |
| GET | `/api/moments/topics/hot` | 热门话题 |
| POST/GET | `/api/admin/login`, `/api/admin/logout`, `/api/admin/me` | AdminSession 登录、登出、当前管理员；Bearer 与 `admin_token` Cookie 兼容 |
| GET | `/api/admin/dashboard` | PostgreSQL 实际指标与 Redis `online:*` 在线人数；不可用指标返回 0 并列入 `unavailableMetrics` |
| GET | `/api/admin/users`, `/api/admin/users/:id` | 用户列表、搜索、分页与详情 |
| POST | `/api/admin/users/:id/ban` | 管理员封禁/解封用户并撤销被封禁用户会话 |
| GET | `/api/admin/logs` | 管理员操作日志分页 |

私聊发送仅接受 `msgType=encrypted`。成功写入后会更新 `Chat.lastMessageAt`、将会话预览固定为 `🔒 [加密消息]`，并向 Redis `cqim:im:push` 发布与 Node 相同的 `{ userId, payload }` 信封，供仍在运行的网关投递。

离线 APNs 与 Web Push 已由 Go API 真实发送：APNs 按设备的 sandbox/production 环境与 alert/VoIP topic 发送，失效设备仅删除对应行；Web Push 使用 VAPID 并会清理失效订阅。私聊唤醒通知只带会话/消息路由数据，不包含明文。缺少 APNs P8 配置时会记录 `no_p8_keys`，不会报告发送成功。不会接入个推。

## 未迁移

验证码发送/校验、密码重置与绑定资料的完整认证流程，未列出的 `/api/admin/*` 管理子路由、二维码业务校验，以及未列出的 `/api/*` 仍未迁移；这些路径会返回 JSON `501`，不会伪装成已完成。

Nginx 未切流；Node `cqim-app/server` 保持可部署，生产流量仍由 Node 处理默认 `/api`。
