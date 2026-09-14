# 线上 cqim-release ↔ 本仓 cqim-app

本仓私有。线上 wed（`42.194.167.201` / `https://wed.imim.chat`）**不是** `imimchat` 的 git checkout。  
产品树在仓里是 `cqim-app/FOO`，在现网上是扁平的 `/home/ubuntu/cqim-release/FOO`。

不要从这份对照去发明 MTProto / 换协议。传输仍是 HTTPS JSON + WebSocket。

## 路径对照

同步时按目录映射，不要把 `cqim-app/` 整层拷进 release 根下再套一层。

| Git（本仓 master） | 线上 `/home/ubuntu/cqim-release/` |
| --- | --- |
| `cqim-app/client` | `client` |
| `cqim-app/server` | `server` |
| `cqim-app/go-gateway` | `go-gateway` |
| `cqim-app/prisma` | `prisma` |
| `cqim-app/deploy` | `deploy` |
| `cqim-app/docs` | `docs` |
| `cqim-app/Dockerfile` | `Dockerfile` |
| `cqim-app/docker-compose.yml` | `docker-compose.yml` |
| `cqim-app/.env.example` | `.env.example`（**不要**覆盖线上 `.env`） |

线上 Docker Compose 项目名：`cqim-release`。API 镜像形如 `cqim:postgresql-*-*`。  
`docker-compose.postgres.yml` **只在现网**，本仓没有这份 overlay。

## 互补矩阵（2026-09-14，以树 + 开放 PR 为准）

| 项 | 线上 wed 已有 | Git master 已有 | 仍在开放 PR |
| --- | --- | --- | --- |
| 目录形状 | 扁平 `cqim-release/` | 嵌套 `cqim-app/` | — |
| Prisma schema | 容器内走 PostgreSQL | `provider = "postgresql"` | — |
| `DATABASE_URL` | compose overlay 覆盖成 `postgresql://…` | 根 compose 仍可能默认 Mongo；deploy 样例仍有 sqlite | #28 改样例 + `init_postgres` SQL |
| `prisma/migrations/` | 现网库已在跑 PG | 仅有 `migration_lock.toml`（仍写 `sqlite`），**没有** `init_postgres` | #28 |
| TRTC | 运行中容器已强制 `1600159677` | `.env.example` 写对了；`server/index.ts` 仍回落到 `1600136830` | #26 去掉回落 |
| Gateway sqlite | 现网自有 `DB_PATH` | `DB_PATH` 默认空，仍 sqlite-only；deploy 仍可写共享 `cqim.db` | #27 fail-closed |
| `POST /api/presence` | 视现网镜像；仓侧已接线 | **已挂**（#31） | — |
| Web 前后台上报 presence | 未随 #31 上客户端 | `AppContext` 只拿 `visibilitychange` 重连 WS，**不** POST presence | #29（与 #31 **冲突**，需 rebase） |
| 聊天预览 / 媒体 / 密钥同步 UX | 视现网静态资源 | **已进 master**（#30） | — |

不要在本对照 PR 里实现 #26 / #27 / #28 / #29。

## 同步顺序

1. **先合 Git，再碰现网。** 建议：`#28` → `#27` → `#26`。  
   `#28` 给空 PG 的 `migrate deploy` SQL；`#27` 避免 Gateway 默默落到共享 sqlite；`#26` 去掉过期 TRTC 回落。
2. **再 rebase `#29`**（它会改 `server/presence.ts` / `index.ts`，与已合并的 #31 重叠）。Web 客户端上报不要和 #31 的路由再打一架。
3. 映射拷贝（例）：

   ```bash
   # 在已合并上述 PR 的树上；切勿覆盖线上 .env / docker-compose.postgres.yml
   rsync -av --relative \
     cqim-app/./client cqim-app/./server cqim-app/./go-gateway cqim-app/./prisma \
     cqim-app/./deploy cqim-app/./docs cqim-app/./Dockerfile cqim-app/./docker-compose.yml \
     ubuntu@42.194.167.201:/home/ubuntu/cqim-release/
   ```

4. 现网：`pnpm build`（或按现网 Dockerfile 流程），**重建 API 镜像**（`cqim:postgresql-*-*`），再 `docker compose` 带上现网的 `docker-compose.postgres.yml` 起来。

## 警告：宿主机 `.env` ≠ 容器里的库

现网宿主机 `/home/ubuntu/cqim-release/.env` **仍可能看起来像 sqlite**（`file:…` / `dev.db`）。  
真正进 Node 的 `DATABASE_URL` 是 **容器 environment / `docker-compose.postgres.yml` overlay**，已指向 PostgreSQL。

- 不要只看宿主机 `.env` 就断定「线上还是 SQLite」。
- 也不要把 Git 里的 `docker-compose.yml` / `deploy/docker-compose.prod.yml` 直接覆盖上去，把 overlay 冲掉。
- `README.md` / `TWO_NODES.md` 里「线上可能仍是 sqlite」描述的是 **Git 样例与切流文档**，不是「打开容器 env 也一定是 sqlite」。

核对容器内实际值（在现网执行，本仓跑不了）：

```bash
cd /home/ubuntu/cqim-release
docker compose -p cqim-release exec cqim printenv DATABASE_URL TRTC_SDK_APP_ID
# 容器名也可能是 cqim-release-api：
# docker exec cqim-release-api printenv DATABASE_URL TRTC_SDK_APP_ID
```

## 相关文件

- 仓布局：[REPO_LAYOUT.md](./REPO_LAYOUT.md)
- 离线推送（presence 路由状态）：[cqim-app/docs/OFFLINE_PUSH.md](../cqim-app/docs/OFFLINE_PUSH.md)
- 迁库步骤（SQL 在 #28）：[cqim-app/docs/MIGRATE_POSTGRES.md](../cqim-app/docs/MIGRATE_POSTGRES.md)
- P0 代码备忘：[cqim-app/docs/P0_CODE.md](../cqim-app/docs/P0_CODE.md)

`.codebuddy/` 是编辑器配置，与现网映射无关；不要为了对齐线上去整目录删它。
