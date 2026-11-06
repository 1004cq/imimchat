# Go API

独立 Go 1.22+ HTTP 服务，使用标准库 `net/http`、`pgx` 和 `go-redis`。它与现有 Node 服务共用 PostgreSQL、Redis 与 `UserSession`。没有修改 Node 或 Prisma。Compose 可启动 `go-api`（容器内 `8089`），但 **默认 Nginx 仍把全部 `/api` 交给 Node `cqim:3000`**；生产未自动切流。

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
| POST | `/api/auth/send-code` | 发送 SMS/邮箱验证码；复用 Node 的 `VerifyCode` 与 Redis `verify_code:*` 状态，5 分钟有效、60 秒限发 |
| POST | `/api/auth/reset-password` | 校验 `reset` 验证码、执行与 Node 相同的密码强度检查并撤销全部用户会话 |
| POST | `/api/auth/bind-phone` | 校验 `bind` 短信验证码后绑定手机 |
| POST | `/api/auth/bind-email` | 校验 `bind` 邮箱验证码后绑定邮箱 |
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
| GET | `/api/moments/feed`, `/api/moments`, `/api/moments/my`, `/api/moments/:id` | 好友 Feed（`/feed` 与已登录的 `GET /api/moments`）、未登录公开列表、`?userId=` 资料页可见性、我的动态、详情 |
| POST | `/api/moments`, `/api/moments/:id/like`, `/api/moments/:id/comments`, `/api/moments/:id/pin` | 发布、点赞、评论、置顶 |
| PUT | `/api/moments/:id`, `/api/moments/reorder` | 编辑可见性/内容/位置、排序 |
| DELETE | `/api/moments/:id`, `/api/moments/:momentId/comments/:commentId` | 删除动态或评论 |
| GET | `/api/moments/topics/hot` | 热门话题 |
| POST/GET | `/api/admin/login`, `/api/admin/logout`, `/api/admin/me` | AdminSession 登录、登出、当前管理员；Bearer 与 `admin_token` Cookie 兼容 |
| GET | `/api/admin/dashboard` | PostgreSQL 实际指标与 Redis `user:online:*` 在线人数；不可用指标返回 0 并列入 `unavailableMetrics` |
| GET | `/api/admin/users`, `/api/admin/users/:id` | 用户列表、搜索、分页与详情 |
| POST | `/api/admin/users/create` | 创建用户，格式/强度/唯一性校验 |
| PUT | `/api/admin/users/:id` | 编辑资料、重置密码；重置时撤销全部 UserSession |
| POST | `/api/admin/users/:id/ban` | 封禁/解封、撤销 Session、删除在线键；物理断连限制见下文 |
| DELETE | `/api/admin/users/:id` | 删除用户及 schema 定义的级联数据 |
| GET | `/api/admin/moments` | 动态搜索、分页、首张媒体及评论/点赞数量 |
| GET | `/api/admin/moments/:id` | 动态详情及全部媒体 |
| DELETE | `/api/admin/moments/:id` | 删除动态（按现有 FK 级联） |
| GET | `/api/admin/moments/:id/comments` | 评论列表、分页 |
| DELETE | `/api/admin/comments/:id` | 删除评论 |
| GET | `/api/admin/logs` | 管理员操作日志分页 |
| POST | `/api/crypto/register-key` | 注册 ECDH 公钥到 `e2ee:pubkey:{userId}:{deviceId}` |
| GET | `/api/crypto/get-key` | 读取已注册公钥 |
| POST | `/api/crypto/verify-message` | HMAC-SHA256 完整性校验 |
| POST | `/api/crypto/register-bundle` | 上传 PreKey Bundle；`signingPublicKey` 合并规则与 Node `resolveSigningPublicKey` 相同 |
| GET | `/api/crypto/get-bundle` | 返回 Bundle 并消费一个 one-time prekey；无 Bundle 时 JSON `404 {"error":"用户未注册 E2EE Bundle"}`，不会返回 `404 page not found` |
| GET | `/api/crypto/prekey-count` | 剩余 one-time prekey 数量 |
| POST | `/api/crypto/replenish-prekeys` | 追加 one-time prekeys |
| GET | `/api/users/search` | 按 id/username/phone/email 精确搜索 |
| GET | `/api/users/{userId}` | 公开资料 + 在线/设备；聊天顶栏与资料卡使用此形状 |
| GET | `/api/users/{userId}/presence` | 在线状态、设备、最后在线时间 |
| GET | `/api/profile` | 当前或指定用户资料（`userId=me` 需登录） |
| PUT | `/api/profile` | 更新当前用户资料 |
| PUT | `/api/auth/profile` | 设置页保存昵称/头像/账号 ID |
| GET | `/api/q/profile/{userId}` | 外链公开资料 |
| GET | `/api/user/me` | 当前用户简要信息 |
| GET | `/api/home/sync` | 首页聚合：当前用户、私聊、群、未读 |

头像字段使用与 Node `safeAvatarUrl` 相同的规则：保留 `/api/media/` 与非 COS 的 `https` URL；历史 COS 直链返回空字符串，避免 403。E2EE 公钥与 Bundle 存在 `SystemConfig`，key 与 Node 相同：`e2ee:bundle:`、`e2ee:prekeys:`、`e2ee:pubkey:`。

私聊发送仅接受 `msgType=encrypted`。成功写入后会更新 `Chat.lastMessageAt`、将会话预览固定为 `🔒 [加密消息]`，并向 Redis `cqim:im:push` 发布与 Node 相同的 `{ userId, payload }` 信封，供仍在运行的网关投递。

离线 APNs 与 Web Push 已由 Go API 真实发送：APNs 按设备的 sandbox/production 环境与 alert/VoIP topic 发送，失效设备仅删除对应行；Web Push 使用 VAPID 并会清理失效订阅。私聊唤醒通知只带会话/消息路由数据，不包含明文。缺少 APNs P8 配置时会记录 `no_p8_keys`，不会报告发送成功。不会接入个推。

## 未迁移

未列出的 `/api/admin/*` 管理子路由、二维码业务校验，以及未列出的 `/api/*` 仍未迁移；这些路径会返回 JSON `501`，不会伪装成已完成。验证码由 `SystemConfig` 中与 Node 相同的 `smtp`、`aliyun` 配置读取（也兼容现有 SMTP/阿里云环境变量）；缺少 SMTP 或短信配置时返回明确错误，不会返回伪造的发送成功。

### 只热更新 go-api（不改默认 Nginx）

生产若已经把 `/api` 指到 go-api，补齐 E2EE / 用户资料后只需重建 `go-api` 容器。不要改仓库里的默认 Nginx 配置；切流与回滚由运维在服务器上复制可选 conf。

```bash
cd cqim-app
docker compose up -d --build go-api
```

本机编译检查：

```bash
cd cqim-app/go-api
go build -o ./bin/go-api ./cmd/go-api
```

Node `cqim-app/server` 保持可部署。默认 Nginx 未切流；生产流量仍由 Node 处理全部 `/api`，除非按下一节 **显式复制** 可选切流配置。

## 如何切 / 如何改回 Node

只改本机 Compose 挂载的 `nginx/default.conf`，不会部署到生产。

启用全部 `/api` 切流（`default.go-api-all.conf`；`/signal` → go-gateway）：

```bash
cd cqim-app
cp nginx/default.go-api-all.conf nginx/default.conf
docker compose up -d --build go-api nginx
```

改回 Node（`node-all-api`）：

```bash
cd cqim-app
cp nginx/default.node-all-api.conf nginx/default.conf
docker compose up -d nginx
```

可选停掉 Go 进程：`docker compose stop go-api`。

### 全部切流 `default.go-api-all.conf`

| location | 上游 |
| --- | --- |
| `location /api/`（及 `location = /api`） | `go-api:8089` |
| `/signal` | `go-gateway:8081`（不是 Node，不是 C++） |
| `/ws` | `go-gateway:8081` |
| `/` 与 ACME `root` | 与 `default.conf` 相同（站点仍由 `cqim` 提供） |


## 管理后台实现与边界

实现位于 `internal/api/admin.go`（路由、鉴权、仪表盘、用户读取、日志）、
`admin_write.go`（登录及事务写入）、`admin_moments.go`（动态/评论读取）。
只使用现有 PostgreSQL pool 和 Redis client，不修改 schema、Node 或流量路由。

- 共享 Node 的 `"AdminAccount"` / `"AdminSession"` 表。Token 有效期 2 小时；先读 Bearer，再读 `admin_token` Cookie。
  Cookie 的 `HttpOnly`、`SameSite=Strict`、`/api/admin` 路径、`ADMIN_DOMAIN` 和生产环境 `Secure` 与 Node 保持一致。
- bcrypt cost=12；旧 SHA-256 登录成功后升级。兼容 bcryptjs 的 UTF-8 72 字节截断和 JS 字符串长度。
  创建用户要求用户名 1–20 位字母/数字/下划线，密码至少 8 位且含大小写及数字；重置密码遵循 Node 的至少 8 位规则。
- 所有成功写操作与 `AdminLog` 在同一事务提交，审计写入失败则业务回滚。登录失败也写审计，未知账号的 adminId 为 null。
  日志记录 actor/action/target/ip，密码重置只记字段名，不记密码、哈希、Token 或聊天内容。
- 读取允许所有已登录管理员；用户和内容写操作仅允许 `admin` / `superadmin`，与现有 Node 对应操作一致。
  用户接口使用字段白名单，不照搬 Node ban 响应中泄漏密码哈希的行为。
  原始 SQL JSON 的日期统一补齐 UTC 标记，按 Prisma 的 Timestamp(3) 语义输出，避免客户端解析偏移。每日统计使用与 Node 相同的本地日期窗口并转换为 UTC 查询；两进程的 TZ 应保持一致。
- Go 登录使用 Redis `admin:login:go:{ip}`：每 IP 15 分钟最多 5 次，成功后重置；与 Node 的限流计数不共享。
  Redis 不可用时登录返回 503。IP 读取规则与 Node 一致，上游必须清理不可信的 X-Forwarded-For，Go 管理端口不应直接暴露公网。
- 列表 page 最小 1、最大 1000000；pageSize 默认 20、最大 100；日志固定每页 20。
  搜索使用与 Prisma contains 一致的区分大小写、字面子串匹配，`%`、`_` 不作为通配符。
- 仪表盘在线数使用 SCAN `user:online:*`、去重，绝不 KEYS；失败返回 0 并标记 `onlineUsers`。
  数据库指标分别降级，无法完整计算的总消息数返回 0；缺失集合返回 [] 并列出集合名。`unavailableMetrics` 去重。
  统计只读取消息数量、类型、发送者和时间，不读取私聊/群聊正文或解密数据。
- 本次额外补齐动态详情、评论列表/删除；当前参考 Node 路由文件没有这三条，不能把 Node 404 当作 Go 失败。
  其他未迁移 `/api/admin/*`：未登录返回 401，已登录返回明确 JSON 501。

### 封禁与网关断线

封禁事务设置 `User.isBanned`、删除全部 UserSession 并写审计；提交后删除
`user:online:{uid}` / `user:presence:{uid}` / `user:activeChat:{uid}`，并向现有
`cqim:im:push` 发布 `{userId, payload:{type:"session_revoked", reason:"admin_action"}}`。
Redis 操作失败返回 503 和 `applied:true`，表示数据库封禁已经生效，可重试清理。

后续网关修复已增加撤销事件消费者：重新核对数据库 Session，关闭被撤销连接（WebSocket 1008）。
每条入站命令、Pong 和定期在线续期也核对 Session，避免漏收 Pub/Sub 后旧 Token 仍可操作。
网关必须配置与 Go API 相同的 PostgreSQL `DATABASE_URL`；不配置时仍使用旧 SQLite 会话源。
该改动仅接通共享 Session 校验，网关原有群消息 SQLite 存储没有迁移。

已在隔离 PostgreSQL/Redis 中用真实管理 HTTP 请求和两台 WebSocket 网关验证断线。
生产尚未部署或验收。API 没有逐节点断线回执，因此保留 `disconnectConfirmed:false`，
它表示未收到分布式断线确认，不表示本地消费者尚未实现。
详见 [网关验证记录](docs/gateway-revocation-20261106.md)。

### 本地验证

```sh
go test ./...
go vet ./...
go build ./cmd/go-api
```

真实数据库回归使用 **专用** `imim_admin_test` 数据库和独立 Redis 实例（DB 15），不要指向生产：

```sh
ADMIN_TEST_DATABASE_URL='postgres://postgres@127.0.0.1:55439/imim_admin_test?sslmode=disable' \
ADMIN_TEST_REDIS_ADDR='127.0.0.1:56389' \
go test ./internal/api -run TestAdmin -v -count=1
```

测试生成随机 schema，读取已有 Prisma migrations，只在该 schema 建表，结束后删除该测试 schema。
Redis 只清理测试创建的键，不执行 FLUSHDB。未设置以上变量时集成测试会明确 SKIP。
覆盖共享 Session、Cookie/Bearer、TTL、旧密码升级、失败审计、角色、创建/唯一性/搜索、
重置撤销、审计回滚、封禁/Redis 事件、SCAN 多页及不可用降级、动态评论/级联删除、登出、限流。
不能据此宣称实际客户端已被踢出。

### 同一 Bearer Token 对拍 Node 与 Go

在隔离环境让 Node 与 Go 连接**同一个测试数据库及 Redis**。Node 正常登录后使用返回的管理员 token；
Go 无需再次登录，也不创建另一套 Session。Go 默认监听 `127.0.0.1:8089`。
无需修改默认 Nginx；Node 直接端口按本地实际启动配置填写。

以下 Python 脚本只发送只读请求，Token 隐藏输入，不放进命令行参数、源文件或打印到终端：

```python
import getpass, json, urllib.request
node = input("Node 本地地址（含端口）: ").rstrip("/")
go = "http://127.0.0.1:8089"
token = getpass.getpass("现有管理员 Bearer token: ")
for path in ("/me", "/dashboard", "/users?page=1&pageSize=20", "/moments?page=1", "/logs?page=1"):
    for label, base in (("Node", node), ("Go", go)):
        req = urllib.request.Request(base + "/api/admin" + path,
            headers={"Authorization": "Bearer " + token})
        with urllib.request.urlopen(req, timeout=20) as response:
            body = json.load(response)
            print(label, path, response.status, json.dumps(body, ensure_ascii=False, sort_keys=True))
```

比对业务字段、数量、筛选和分页；时间统一解析为 UTC 后比较，列表相同时间戳的并列行可按 id 排序。
Go 增加的 `unavailableMetrics`、安全响应字段，以及双方各自登录/审计生成的时间和记录需单独解释。
只在测试环境做写接口对拍：用同一 token 操作不同的临时用户/动态，核对数据库结果及 AdminLog；
不要对同一对象连续执行两边删除再把第二次 404 当成不一致。登出放最后，两边都应随后返回 401。
`disconnectConfirmed:false` 表示 API 不等待逐节点断线回执；两端对拍不能将事件发布视为所有生产连接已关闭。
