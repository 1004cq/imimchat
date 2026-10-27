# im.cq.je 双节点部署指南

两台服务器作为**同一站点**运行，用户只访问 `https://im.cq.je`。EdgeOne 仅回源**节点1**，由节点1 宿主机 Nginx 将流量分发到两台应用节点。

## 架构

```
用户 → EdgeOne（im.cq.je）→ 仅回源 42.194.167.201（节点1）
                                      │
                    节点1 宿主机 Nginx（TLS 终结）
                    ├─ /api、/        least_conn → 节点1:3011 + 节点2:3011
                    ├─ /signal        ip_hash    → 节点1:3011 + 节点2:3011
                    └─ /ws/group      ip_hash    → 节点1:8082 + 节点2:8082
                                      │
              ┌───────────────────────┴───────────────────────┐
              │ 节点1（42.194.167.201）    节点2（106.53.196.247）│
              │ cqim:3011                  cqim:3011            │
              │ go-gateway:8082            go-gateway:8082      │
              │ GATEWAY_ID=gw-app-1        GATEWAY_ID=gw-app-2  │
              └───────────────────────┬───────────────────────┘
                                      │
                           节点1：Redis（6380）+ 共享数据（NFS/SQLite）
                           跨节点推送：Redis Pub/Sub cqim:im:push
```

**禁止**：EdgeOne 多源站轮询、两套独立数据库、在节点2 对外暴露 im.cq.je 的 Nginx/SSL。

## 节点角色

| 角色 | IP | 运行服务 |
|------|-----|----------|
| 节点1（入口 + 数据） | 42.194.167.201 | cqim、go-gateway、Redis、MySQL、Mongo、宿主机 Nginx + SSL |
| 节点2（应用） | 106.53.196.247 | 仅 cqim、go-gateway；**无** 对外 Nginx |

当前生产使用 **SQLite**（NFS 共享 `cqim.db`）+ **Redis Pub/Sub** 跨节点投递。若迁移 Mongo，将 `DATABASE_URL` 指向节点1 内网 `27017` 即可，双节点推送逻辑不变。

## 节点1 部署

```bash
cd /home/ubuntu/cqim-release
cp deploy/env.app.example .env   # 或沿用现有 .env
# 节点1 必须设置：
#   GATEWAY_ID=gw-app-1
#   PUBLIC_BASE_URL=https://im.cq.je

docker compose -f docker-compose.yml -f deploy/docker-compose.prod.yml up -d
```

### SSL 证书

证书放在宿主机（**勿提交私钥到 Git**）：

```
/etc/nginx/ssl/im.cq.je/im.cq.je_bundle.pem   # 站点证书 + 中间证书
/etc/nginx/ssl/im.cq.je/im.cq.je.key          # 私钥
```

### Nginx 双节点 upstream

1. 编辑 `deploy/nginx-lb.conf`，将 `NODE2_HOST` 换为节点2 地址（跨 VPC 时用 `106.53.196.247`）。
2. 在 `/etc/nginx/sites-enabled/cqim` 的 `http` 上下文 include upstream 段。
3. 将 `/api/`、`/`、`/signal`、`/ws/group` 的 `proxy_pass` 改为 `cqim_api_nodes`、`cqim_signal_nodes`、`gateway_ws_nodes`。
4. `nginx -t && systemctl reload nginx`

### Redis 对节点2 开放

节点1 Docker Redis 映射 `6380:6379`，安全组 / `ufw` 仅允许 `106.53.196.247` 访问 `6380`。

### NFS 共享数据（SQLite）

```bash
# 节点1
echo '/home/ubuntu/cqim_shared/data 106.53.196.247(rw,sync,no_subtree_check,no_root_squash)' | sudo tee -a /etc/exports
sudo exportfs -ra

# 节点2
sudo mount -t nfs 42.194.167.201:/home/ubuntu/cqim_shared/data /home/ubuntu/cqim_shared/data
```

## 节点2 部署

```bash
cd /home/ubuntu/cqim-release
cp deploy/env.app.example .env
# 编辑 .env：NODE1_HOST=42.194.167.201，GATEWAY_ID=gw-app-2

docker compose -f docker-compose.yml -f deploy/docker-compose.app.yml up -d cqim go-gateway
```

节点2 容器端口需对节点1 可达：`3011`（API）、`8082`（Gateway）。`docker-compose.app.yml` 已绑定 `0.0.0.0`；请在云安全组限制来源为节点1 公网 IP。

**不要**在节点2 安装 im.cq.je 的 Nginx/SSL。

## EdgeOne 配置

- 域名：`im.cq.je`
- 源站：**仅** `42.194.167.201`（HTTP/HTTPS 按现有配置）
- 不要添加 `106.53.196.247` 为源站

## 在线状态与跨节点推送

| Redis Key | 含义 |
|-----------|------|
| `user:online:{uid}` | Go Gateway 路由表，JSON `{"nodeId","connId","ts"}`（兼容旧纯字符串 nodeId），TTL 90s |
| `gw:users:{GATEWAY_NODE_ID}` | 该 Gateway 在线用户集合 |
| `gw:alive:{GATEWAY_NODE_ID}` | 节点存活心跳，TTL 30s |
| `online:{uid}` | Node `/signal` 在线 |
| `cqim:im:push` | 共享 Pub/Sub 频道（Node + Gateway） |
| `cqim:im:push:{nodeId}` | 按目标节点定向投递（Gateway 订阅自身频道） |

鉴权成功后写入路由；心跳 / ping 续期；断开时条件删除（仅当仍指向本 nodeId+connId）。  
投递：本机连接 → 直推；否则查路由 → 他节点则 Pub/Sub（带 `msgId`/`fromNodeId`，接收端不二次转发）；离线则返回，由现有落库 + APNs/Web Push 处理。

本地双 Gateway Compose：`docker-compose.multi.yml`。仅 Gateway 边缘：`docker-compose.gateway-only.yml`（`GATEWAY_ONLY=true`）。Nginx 多机示例：`deploy/nginx-multi.conf`。

## 本地验证（2 Gateway + 共享 Redis）

完整栈（API + 两台 Gateway）：

```bash
cd cqim-app
docker compose -f docker-compose.yml -f docker-compose.multi.yml up -d --build
curl -s http://127.0.0.1:8082/health   # mode + gatewayId=gw-1
curl -s http://127.0.0.1:8083/health   # gatewayId=gw-2
```

仅 Gateway（`GATEWAY_ONLY`，适合 1C1G / 边缘）：

```bash
docker compose -f docker-compose.gateway-only.yml --profile dual up -d --build
curl -s http://127.0.0.1:8082/health   # gatewayOnly=true
curl -s http://127.0.0.1:8083/health
```

跨节点互发：

1. 用户 A 连节点 1（`:8082` 的 `/ws/group` 或经 Nginx 的 `/signal`），用户 B 连节点 2（`:8083`）。
2. A→B、B→A 各发一条：本机无连接时走 Redis Pub/Sub（`cqim:im:push` / `cqim:im:push:{nodeId}`），对端只本地推、不二次转发。
3. `redis-cli GET user:online:{userId}` → JSON `{"nodeId","connId","ts"}`，TTL ≈ 90s；`gw:alive:{nodeId}` 有心跳。
4. 停掉一台 Gateway 后，该节点用户应改走落库 + APNs/Web Push（长连接在也不算免推，除非 presence=foreground）。

单元测试（需本机 Redis）：

```bash
cd cqim-app/go-gateway && go test ./internal/gateway/ -count=1
```

## 验收清单

```bash
# 1. 两台 Gateway ID 不同
curl -s http://127.0.0.1:8082/health          # 节点1 → gatewayId: gw-app-1
curl -s http://106.53.196.247:8082/health     # 节点2 → gatewayId: gw-app-2

# 2. 域名仅回源节点1（公网 DNS 应指向 EdgeOne，非两台服务器 IP）

# 3. 跨节点私聊：用户 A 连节点1 /signal，用户 B 连节点2 /signal，互发消息实时可达

# 4. 单节点 compose 仍可启动（仓库根 docker-compose.yml，不含 app 覆盖）
docker compose up -d
```

## 相关文件

| 文件 | 说明 |
|------|------|
| `deploy/nginx-lb.conf` | 节点1 upstream 模板 |
| `deploy/docker-compose.prod.yml` | 节点1 生产覆盖（im.cq.je） |
| `deploy/docker-compose.app.yml` | 节点2 仅应用层 |
| `deploy/env.app.example` | 节点2 环境变量模板 |
| `docs/dual-node-deploy.md` | 通用双节点说明（Mongo 版） |
