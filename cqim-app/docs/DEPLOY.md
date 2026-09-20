# CQIM main 部署与验收

## 分支边界

生产部署只使用 GitHub `main`。不要合并 `codex/wed-cpp-migration`、`cursor/mtproto-*`、`cursor/neomsg-*`、`cursor/jpush-*` 或 `cursor/tg-architecture-*`。默认 Nginx 仍然把 `/api` 和 WebSocket 流量转发到 Node/Go 现网服务，不指向 C++。

## 存储方案

默认媒体存储是 **MinIO**，不是本地盘。图片、语音、视频、贴纸和文档写入 MinIO，PostgreSQL 只保存 `MediaFile` 元数据；客户端新上传统一调用 `/api/media/upload` 或 `/api/media/upload-form`。旧 COS 只作为历史 URL 的兼容读取路径，COS 配置失败不会让头像接口抛 500；无效历史头像返回空值，由客户端显示占位图。`/api/cos/sts` 已废弃，无密钥时固定返回 503。

`MINIO_ROOT_USER` 和 `MINIO_ROOT_PASSWORD` 必须写入部署机的 `.env`，不能使用仓库里的示例值。

## 管理员后台存储

管理员后台只依赖 **PostgreSQL + Redis**：用户、仪表盘统计、管理员操作日志、登录日志和非法请求日志均通过 Prisma 写入 PostgreSQL；在线用户数从 Redis 的 presence key 实时统计。后台不连接 MySQL，`server/mysql.ts` 仅保留废弃占位，避免旧部署脚本引用路径失效。

## 启动顺序

```text
PostgreSQL healthy
Redis healthy
MinIO healthy (/minio/health/live)
    ↓
cqim 容器执行 prisma migrate deploy
    ↓
Node cqim 与 Go gateway 提供服务
    ↓
Nginx 对外提供 HTTP（默认 default.conf）/ HTTPS（切换 https.conf 后）
```

首次部署：

```bash
cd cqim-app
cp .env.example .env
# 编辑 .env：至少修改 MINIO_ROOT_USER、MINIO_ROOT_PASSWORD
# PUBLIC_BASE_URL 示例为 https://im.cq.je；CORS_ORIGINS 须包含 https://im.cq.je
# 生产再填 TRTC_SECRET_KEY、APNS_*、WEB_PUSH_*（不要把 APNs P8 提交进 Git）
./scripts/deploy.sh
# 等价于：docker compose up -d --build && docker compose ps
```

`scripts/deploy.sh` 会拒绝空的 `MINIO_ROOT_USER` / `MINIO_ROOT_PASSWORD` / `DATABASE_URL`，以及 `file:./dev.db`。它还会创建 `./data` 并尽量把属主改成镜像里的 `nodeuser`（uid 999），否则 Node 无法在 `/app/data/stickers` 建目录。

Node 容器的启动脚本会在启动服务前执行 `prisma migrate deploy`。如果迁移失败，Node 不会启动，先查看 `docker compose logs cqim`。

## Nginx 配置与证书目录

Compose 只挂载这些路径，必须和 `nginx/*.conf` 一致：

| 宿主机 | 容器内 | 用途 |
|--------|--------|------|
| `nginx/default.conf` | `/etc/nginx/conf.d/default.conf` | 当前生效配置 |
| `certbot/www` | `/var/www/certbot` | ACME webroot |
| `certbot/conf` | `/etc/letsencrypt` | Let's Encrypt 证书（`https.conf` 读 `live/im.cq.je/`） |

`cqim-app/certs/` 是 APNs 密钥目录，不是 Nginx TLS 目录，不要挂进 Nginx。

默认 `nginx/default.conf` 的 `server_name` 是 `im.cq.je`（以及 `localhost`），只监听 80，因此不需要先有证书。签发证书后：

```bash
# scripts/bind-cq-je-domain.sh 为 im.cq.je 申请证书，成功后再切 https.conf
cp nginx/https.conf nginx/default.conf
docker compose up -d nginx
```

`https.conf` 的证书路径是 `/etc/letsencrypt/live/im.cq.je/fullchain.pem` 与 `privkey.pem`，对应宿主机 `certbot/conf/live/im.cq.je/`。把 Let's Encrypt 或其它证书放到该目录，文件名保持 `fullchain.pem` / `privkey.pem`。

## APNs 环境与 Token 生命周期

`PushDeviceToken.environment` 是 APNs 网关选择的第一依据：`sandbox` Token 发送到 `api.sandbox.push.apple.com`，`production` Token 发送到 `api.push.apple.com`。因此 Xcode Debug 包和 TestFlight/正式包可以同时注册、同时存在。只有旧客户端没有提交 environment 时，服务端才读取 `.env` 中的 `APNS_PRODUCTION=true|false`。

`DELETE /api/apns/token` 带 `token` 时只删除当前用户的这一行；不带 `token` 时按当前用户、iOS、`kind` 和 environment 删除最近更新的一行，不会清空用户的全部 iOS Token。APNs 返回 410、`BadDeviceToken` 或 `Unregistered` 时，发送路径只删除对应失效行。VoIP 发送会遍历该用户全部 `kind=voip` Token，并按各行 environment 分网关发送。

## Redis 故障语义

`GET /api/health` 的 `checks.redis` 和 `redisAvailable` 来自真实 Redis `PING`；Redis 不可用时健康接口返回 503。`GET /api/presence` 同样返回 `redisAvailable`。presence 读取失败会记录错误并保守按 `offline` 处理，让离线推送继续尝试；presence 写入失败会记录错误并让 `POST /api/presence` 返回 500，不再伪装成成功。

## MinIO 健康检查

Compose 使用的最终探针是：

```yaml
test: ["CMD", "/usr/local/bin/busybox", "wget", "-q", "-O", "/dev/null", "http://127.0.0.1:9000/minio/health/live"]
```

官方 MinIO 镜像（`quay.io/minio/minio`，Docker Hub 的 `minio/minio` 已下线）提供 `/minio/health/live` 未授权存活接口，但近期官方镜像不保证包含 `mc`、`curl` 或 `wget`。仓库中的 `minio-healthcheck.Dockerfile` 仍以官方 MinIO 镜像为基础，只从官方 BusyBox 镜像复制一个静态 `busybox` 二进制，因此探针不依赖 `mc`，并且确实请求 MinIO 官方健康路径。HTTP 200 才会使 `minio` 变为 healthy，`cqim` 的 `depends_on` 才会继续。

默认 Compose **不**把 MinIO `9000` 映射到宿主机。手工检查：

```bash
docker compose exec minio /usr/local/bin/busybox wget -q -O /dev/null http://127.0.0.1:9000/minio/health/live
echo $?  # 0 表示 live
```

## 探活

```bash
# Node API（经 Nginx → cqim:3000，不是 C++ 8088）
# HTTP 默认配置：
curl -fsS http://im.cq.je/api/health
# 本机尚未解析域名时：
curl -fsS -H 'Host: im.cq.je' http://127.0.0.1/api/health
# 切换 https.conf 之后：
curl -fsS https://im.cq.je/api/health

# Redis
docker compose exec redis redis-cli ping  # PONG

# MinIO（容器内，不是宿主机 :9000）
docker compose exec minio /usr/local/bin/busybox wget -q -O /dev/null http://127.0.0.1:9000/minio/health/live

# WebSocket /signal（Node；HTTP 默认 / HTTPS 生产）
curl --http1.1 -i -N \
  -H 'Connection: Upgrade' \
  -H 'Upgrade: websocket' \
  -H 'Sec-WebSocket-Version: 13' \
  -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' \
  http://im.cq.je/signal
# 预期 HTTP/1.1 101 Switching Protocols；若需要 token，追加现网认证参数。
# HTTPS：https://im.cq.je/signal
```

## 八条发布后冒烟

1. 使用密码、短信或邮箱完成登录，并确认刷新页面后会话仍有效。
2. 注册或加载端到端密钥，建立加密私聊，确认双方能发送和解密消息。
3. 在接收端杀掉 App/浏览器进程，发送一条消息，确认后台路径仅调用 APNs 或 Web Push；不把它描述为已通过真机 APNs 验证，除非实际装包测试。
4. 在聊天中分别发送图片和语音，确认文件进入 MinIO、`MediaFile` 只保存元数据，读取 `/api/media/:id` 成功。
5. 打开音视频通话，确认使用环境变量中的 TRTC SDKAppID `1600159677`，未配置或不匹配时接口明确报错。
6. 打开二维码页面，完成二维码生成、扫描或邀请链接解析。
7. 从通知、二维码或联系人进入聊天，确认能定位到正确的会话。
8. 发送多条未读消息后退出并重新进入，确认未读数、已读状态和清零行为一致。

## 常用故障定位

```bash
docker compose ps
docker compose logs --tail=200 postgres redis minio cqim go-gateway nginx
docker inspect --format '{{json .State.Health}}' cqim-minio
docker compose exec cqim pnpm exec prisma migrate status
```
