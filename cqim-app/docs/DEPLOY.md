# CQIM 部署

生产只部署 GitHub `main`。  
不要合并 C++ 迁移分支、MTProto / NeoMsg / 第三方推送实验分支。  
`/api` 和 `/signal` 只指向 Node API 与 Go 网关，不要指向 C++。

## 域名

`PUBLIC_BASE_URL` 与 `CORS_ORIGINS` 必须和客户端实际请求的 HTTPS 来源一致。  
具体域名、解析与证书只写在部署机，不写进本文件。

源站已有 Nginx 时：不要覆盖原站点配置，用反代把站点、`/api`、`/signal` 转到本项目的 API 与网关容器。

## 栈

| 组件 | 作用 |
|------|------|
| PostgreSQL | 用户、会话、消息、朋友圈、管理日志、系统配置 |
| Redis | 在线状态、网关路由、Pub/Sub |
| MinIO | 图片、语音、视频、贴纸、文档 |
| Node | HTTP API |
| Go 网关 | WebSocket |
| Nginx | 入口 |

不使用 MongoDB、MySQL、第三方推送商。旧对象存储地址仅兼容读取，失败时不得让接口 500。

8 核 8G 可以先上线、做冒烟。同机还有其它站点时内存会紧。第一次构建镜像可能要几十分钟。

## 启动

```bash
cd cqim-app
git checkout main && git pull
cp .env.example .env
```

在 `.env` 填写数据库密码、MinIO 账号密码、`PUBLIC_BASE_URL` 与 `CORS_ORIGINS`。  
通话与推送密钥只放环境变量，不要写入 Git。

```bash
./scripts/deploy.sh
# 或：docker compose up -d --build && docker compose ps
```

顺序：Postgres / Redis / MinIO healthy → `prisma migrate deploy` → API 与网关 → Nginx。  
`DATABASE_URL` 必须是 PostgreSQL，禁止文件数据库路径。

### 部署后必须确认迁移

`cqim` 容器入口（`scripts/start-api.sh`）会先跑 `pnpm exec prisma migrate deploy`；失败则不启 API。  
含 `User.webPushSubscription` 的迁移 SQL 使用 `ADD COLUMN IF NOT EXISTS`，已热修过的库再跑也不会失败。

手工补跑 / 核验：

```bash
docker compose exec cqim pnpm exec prisma migrate deploy
docker compose exec cqim pnpm exec prisma migrate status
```

## 探活

```bash
docker compose ps
curl -fsS https://你的域名/api/health
docker compose exec redis redis-cli ping
docker compose exec minio /usr/local/bin/busybox wget -q -O /dev/null \
  http://127.0.0.1:9000/minio/health/live
```

健康检查须显示 Redis 可用。MinIO 只在容器内探活，不要把对象存储控制台暴露到公网。

## 推送

仅前台在聊时跳过系统通知；长连接（WebSocket /signal 仍在）不算免推。  
代码路径：`shouldSkipApns` / `shouldSkipApnsFromState` 只看 presence=`foreground`（及可选 activeChatId），不看 Gateway 在线表。  
按设备令牌环境分别发往对应推送网关。失效令牌只删对应一行。通知不含消息明文。

## 冒烟

1. 登录后刷新会话仍在
2. 加密私聊双方能解密
3. 结束进程后有系统通知（未真机验证不得写已通过）
4. 图片和语音可上传、刷新后仍可打开
5. 通话使用环境变量里的正式 AppId，禁止旧 ID 兜底
6. 二维码中的用户标识与头像正确
7. 从通知或联系人进入正确会话
8. 未读数字正确，进入会话后清零

## 单机 / 多机模式

系统按 Redis 存活键自动判定，客户端连信令前会请求 `GET /api/runtime`：

| 条件 | `mode` | 客户端行为 |
|------|--------|------------|
| `api:alive:*` / `gw:alive:*` 各 ≤1 | `single` | 同源 `/signal` |
| 任一类 >1 | `multi` | 有 Nginx LB → 仍同源 `/signal`；配了 `PUBLIC_SIGNAL_URLS` → 按 userId 粘性选节点 |

强制覆盖（排障 / 切流）：

```bash
# .env
DEPLOY_MODE=auto    # 默认
# DEPLOY_MODE=single
# DEPLOY_MODE=multi
```

单机：只用 `docker compose up -d`，`GATEWAY_NODE_ID` / `API_NODE_ID` 各一个即可。  
多机：两台共用同一 Postgres / Redis / MinIO，节点 ID 互不相同，入口 Nginx 见 `deploy/nginx-lb.conf`。

```bash
curl -s https://你的域名/api/runtime
# {"mode":"single"|"multi","apiCount":…,"gatewayCount":…,"signalPath":"/signal",…}
```

## 多节点 Gateway（本地）

同区域 2 个 Gateway + 共享 Redis（完整栈）：

```bash
cd cqim-app
docker compose -f docker-compose.yml -f docker-compose.multi.yml up -d --build
curl -s http://127.0.0.1:8082/health   # nodeId=gw-1
curl -s http://127.0.0.1:8083/health   # nodeId=gw-2
```

仅 Gateway 边缘节点（`GATEWAY_ONLY=true`，1C1G）：

```bash
docker compose -f docker-compose.gateway-only.yml --profile dual up -d --build
curl -s http://127.0.0.1:8082/health   # gatewayOnly=true
curl -s http://127.0.0.1:8083/health
```

跨节点互发：用户 A 连节点 1（8082），用户 B 连节点 2（8083）；查 Redis `GET user:online:{userId}` 应为 JSON `{"nodeId","connId","ts"}`。详见 `docs/TWO_NODES.md` 与仓库根 README。

## 更新

```bash
git checkout main && git pull
cd cqim-app && docker compose up -d --build
# 确认迁移（compose 启动已跑；手工核验）：
docker compose exec cqim pnpm exec prisma migrate deploy
```

## 排障

```bash
docker compose logs --tail=200
docker compose exec cqim pnpm exec prisma migrate status
```

## app.imim.chat 双机入口（HK + YN）

生产拓扑（内网 `10.66.66.0/24`）：

| 角色 | 公网 | 内网 | 职责 |
|------|------|------|------|
| HK | `103.24.217.244` | `10.66.66.1` | TLS 终结、静态站、Postgres/Redis/MinIO、API+Gateway |
| YN | `140.210.12.160` | `10.66.66.2` | API/Gateway 应用节点；**公网 80/443 只做 stream 透传到 HK** |

宿主机 Nginx 模板：

- HK TLS 入口 → Docker `127.0.0.1:18080`：`deploy/nginx-app.imim.chat.host.conf`
- YN `stream {}` 透传：`deploy/nginx-edge-stream-yn.conf`

**DNS（DNSPod）**：`app.imim.chat` 的 A 记录**优先只保留 HK**。若同时解析到 YN：

1. YN 必须 `ufw allow 80,443/tcp`，否则客户端连 YN 会超时（表现为「网页打不开」）。
2. YN 必须用 stream 透传到 `10.66.66.1`，**不要**在 YN 再挂一套静态 HTML/JS（双份构建 hash 不一致会导致白屏）。
3. 验收：分别 `--resolve` 到两台公网 IP，`/` 与 `/api/health` 均为 200，且首页 `sha256` 相同。

```bash
dig +short app.imim.chat A
curl -sS -o /dev/null -w "%{http_code}\n" --resolve app.imim.chat:443:103.24.217.244 https://app.imim.chat/
curl -sS -o /dev/null -w "%{http_code}\n" --resolve app.imim.chat:443:140.210.12.160 https://app.imim.chat/
curl -sS https://app.imim.chat/api/runtime
```
