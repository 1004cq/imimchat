# imimchat / CQIM

[English](./README_en.md) | **中文**

私有化 IM。产品代码在 `cqim-app/`。iOS 原生客户端不在本仓。

传输：HTTPS JSON + WebSocket。不做 MTProto / TDLib / 多 DC。
WuKongIM 不是主路径。

生产域名、证书与密钥只写在部署机 `.env`，不写进本说明。

---

## 技术栈

| 层 | 用什么 |
| --- | --- |
| Web | React + Vite（`cqim-app/client`） |
| API | Node + Express（`cqim-app/server`） |
| 网关 | Go（`cqim-app/go-gateway`） |
| 主库 | PostgreSQL（Prisma） |
| 缓存 / 在线 | Redis |
| 媒体 | MinIO（库中只存元数据） |
| 私聊 | 强制 `msgType=encrypted` |
| 推送 | 自建 APNs + Web Push |
| 通话 | TRTC（AppId 以环境变量为准，禁止旧 ID 兜底） |

不使用 MongoDB、MySQL。

朋友圈、用户资料、SMTP 配置、管理日志都在 PostgreSQL。

---

## 本地 / 部署

```bash
cd cqim-app
cp .env.example .env
# DATABASE_URL 必须是 postgresql://...
# 填 MinIO、主库密码与 PUBLIC_BASE_URL
./scripts/deploy.sh
```

详细步骤和冒烟清单见 [cqim-app/docs/DEPLOY.md](./cqim-app/docs/DEPLOY.md)。

生产只使用 `main`。不要合并 C++ / MTProto 实验分支，不要把 `/api` 指到 C++。

---

## 多机部署（单区域 · Gateway 多节点）

同区域 2+ 台 Gateway，共享一台 PostgreSQL / Redis / MinIO。跨节点实时投递走 Redis Pub/Sub（`cqim:im:push` + `cqim:im:push:{nodeId}`），路由表在 `user:online:{userId}`。

### 本地启动 2 个 Gateway

```bash
cd cqim-app
cp .env.example .env   # 填同一套 DATABASE_URL / REDIS_URL / MinIO
docker compose -f docker-compose.yml -f docker-compose.multi.yml up -d --build

curl -s http://127.0.0.1:8082/health   # nodeId=gw-1，redis=true
curl -s http://127.0.0.1:8083/health   # nodeId=gw-2
```

入口 Nginx 示例：`cqim-app/deploy/nginx-multi.conf`（`/api` → API upstream，`/signal` 与 `/ws/group` → 长连接 upstream，支持 Upgrade）。

### 验证跨节点发消息

1. 用户 A 连节点 1 的 `/signal`（或 `/ws/group`），用户 B 连节点 2。
2. A 给 B 发私聊/群消息：写库成功后，B 应实时收到（本机无连接时发布 Pub/Sub，对端节点只本地推，不二次转发）。
3. 断开重连后查 Redis：`GET user:online:{userId}` 应为 JSON `{"nodeId","connId","ts"}`，TTL ≈ 90s。
4. B 完全离线时应仍走原有落库 + APNs/Web Push。

更细的双节点运维说明见 [cqim-app/docs/TWO_NODES.md](./cqim-app/docs/TWO_NODES.md)。

### 回滚到单节点

```bash
# 停掉第二台 Gateway，或只用基础 compose
docker compose -f docker-compose.yml -f docker-compose.multi.yml stop go-gateway-2
# 或：
docker compose -f docker-compose.yml up -d
```

Nginx upstream 只保留一台；行为与单机一致（本机直推，无跨节点时不依赖第二台）。

环境变量：`GATEWAY_NODE_ID`（或别名 `GATEWAY_ID`）、所有节点同一 `REDIS_URL` / `DATABASE_URL`。见 `cqim-app/.env.example`。

---

## 还没做完

- 现网是否已按本文档完成切库与冒烟，以部署机为准
- iOS 与 Web 对齐（另仓）
- 真机杀进程推送验证

## 版权

本仓为私有仓，**不开源**。保留所有权利。
