# 同区域双节点部署指南

> **过时提醒（Mongo 时代稿）：** Prisma 主库是 **PostgreSQL**。禁止把 `mongodb://` 填进 `DATABASE_URL`。现行双机以 [TWO_NODES.md](./TWO_NODES.md) 与 [MIGRATE_POSTGRES.md](./MIGRATE_POSTGRES.md) 为准。

本文说明如何在**同一区域**部署两台 CQIM 节点（Node API + Go Gateway），共享**一台** PostgreSQL / Redis / MySQL，并通过 Redis Pub/Sub 实现跨节点私聊与群消息可达。

> 约束：不拆数据库、不上 K8s、不改 MTProto。

## 架构概览

```
                    ┌──────────── Nginx（节点1 或独立 LB）────────────┐
                    │  /api、/  → upstream cqim_nodes                 │
                    │  /ws      → upstream gateway_nodes               │
                    └────────┬───────────────────────┬─────────────────┘
                             │                       │
              ┌──────────────┴──────────┐   ┌────────┴──────────────┐
              │ 节点1                    │   │ 节点2                  │
              │ cqim:3000               │   │ cqim:3000              │
              │ go-gateway:8081         │   │ go-gateway:8081        │
              │ GATEWAY_ID=gw-node1     │   │ GATEWAY_ID=gw-node2    │
              └────────────┬────────────┘   └────────┬──────────────┘
                           │                         │
                           └──────────┬──────────────┘
                                      │
                           ┌──────────▼──────────┐
                           │ 节点1 内网数据层      │
                           │ PostgreSQL / Redis / MySQL│
                           └─────────────────────┘
```

跨节点实时投递通道：`cqim:im:push`（Redis Pub/Sub）。

## 节点1（主节点，含数据层）

使用仓库根目录 `docker-compose.yml` 正常启动即可：

```bash
cp .env.example .env
# 编辑 .env，设置 PUBLIC_BASE_URL、密码等
docker compose up -d
```

节点1 的 Go Gateway 建议设置唯一 ID：

```env
GATEWAY_ID=gw-node1
```

## 节点2（应用节点，连接节点1 数据层）

在第二台机器上仅部署 **cqim** 与 **go-gateway**（或完整 compose 但关闭 mongo/redis/mysql 服务），环境变量指向节点1 内网地址：

```env
# 节点2 .env 示例（将 10.0.0.1 换成节点1 内网 IP）
NODE_ENV=production
PORT=3000
PUBLIC_BASE_URL=https://wed.imim.chat

# Prisma 主库 = PostgreSQL。Mongo 不是 Prisma 主库，禁止 mongodb:// 与 file:*.db
DATABASE_URL=postgresql://USER:PASS@TENCENT_PG_HOST:5432/cqim?schema=public
REDIS_URL=redis://10.0.0.1:6379
MYSQL_URL=mysql://cqim:YOUR_PASSWORD@10.0.0.1:3306/cqim_audit

# 每台 Gateway 必须不同
GATEWAY_ID=gw-node2
REDIS_ADDR=10.0.0.1:6379
CORS_ORIGINS=https://wed.imim.chat,http://wed.imim.chat
```

安全组 / 防火墙：仅允许节点访问腾讯云 PG 内网 `5432`，以及节点1 的 `6379`、`3306`。不要对公网开库端口。

节点2 启动示例：

```bash
docker compose up -d cqim go-gateway
```

## Nginx：将第二台加入 upstream

在入口 Nginx（通常在节点1，或独立负载均衡）中，为 **API** 与 **WebSocket** 分别配置 upstream。

### API / 静态（`/api`、`/`）

在 `http` 块中定义：

```nginx
upstream cqim_nodes {
    least_conn;
    server 10.0.0.1:3000;   # 节点1 cqim
    server 10.0.0.2:3000;   # 节点2 cqim
}
```

将 `location /api` 与 `location /` 的 `proxy_pass` 改为：

```nginx
proxy_pass http://cqim_nodes;
```

`/signal` WebSocket 随 `location /` 升级，会按 upstream 分发到不同 Node；跨节点私聊由 `cqim:im:push` 保证对端可达。

### Go Gateway（`/ws` 群聊）

```nginx
upstream gateway_nodes {
    least_conn;
    server 10.0.0.1:8081;   # 节点1 go-gateway
    server 10.0.0.2:8081;   # 节点2 go-gateway
}

location /ws {
    proxy_pass http://gateway_nodes;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection $connection_upgrade;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_read_timeout 86400s;
    proxy_send_timeout 86400s;
}
```

> WebSocket 需保持长连接；`least_conn` 或 `ip_hash` 均可，按运维偏好选择。

## 在线状态与跨节点推送

| Redis Key | 含义 |
|-----------|------|
| `user:online:{uid}` | Go Gateway 在线，值为 `GATEWAY_ID`，TTL 90s |
| `gw:users:{GATEWAY_ID}` | 该 Gateway 上的在线用户集合 |
| `gw:alive:{GATEWAY_ID}` | Gateway 心跳，TTL 30s |
| `online:{uid}` | Node `/signal` 在线（TTL 90s） |
| `group:online:{groupId}` | 群在线成员（Node + Go 共同写入） |

私聊 / 群消息写库成功后，Node 调用 `publishImPush` → `PUBLISH cqim:im:push`；各节点订阅后向本机 WebSocket 连接投递。

Go Gateway 在本地 `PushToUser` 失败时同样会 `PublishImPush` 到该频道。

## 健康检查

```bash
curl http://10.0.0.1:8081/health
# {"status":"ok","gatewayId":"gw-node1","online":12,"service":"cqim-go-gateway"}

curl http://10.0.0.2:8081/health
# {"status":"ok","gatewayId":"gw-node2","online":8,"service":"cqim-go-gateway"}
```

## 验证清单

1. 用户 A 连节点1 `/signal`，用户 B 连节点2 `/signal`
2. A 向 B 发私聊 → B 实时收到 `private_message`
3. 同一群内 A、B 分属不同节点 → 群消息 / 撤回信令可达
4. 下线一端 Gateway → `SIGTERM` 后仅清理本机 `gw:users` 集合，不误删他节点在线 key

## CORS（Go Gateway）

生产环境通过 `CORS_ORIGINS` 逗号分隔白名单校验 WebSocket `Origin`；未配置时开发环境默认放行。
