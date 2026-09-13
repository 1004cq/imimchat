# imimchat / CQIM

[English](./README_en.md) | **中文**

私有化 IM。**主仓只有这一个**（旧仓 `cq` 已删）。产品代码在 `cqim-app/`。

| 项 | 值 |
| --- | --- |
| 线上 Web | https://wed.imim.chat |
| 管理后台 | https://wed.imim.chat/admin |
| API / WS | `https://wed.imim.chat/api` · `wss://wed.imim.chat/signal` |
| iOS 原生 | 另仓 [1004cq/imimchatios](https://github.com/1004cq/imimchatios)，**不在本仓** |

传输：HTTPS JSON + WebSocket。**不做** MTProto / TDLib / 多 DC。
WuKongIM 不是主路径。

---

## 数据库（以这段为准）

两件事要分开说，之前 README 混在一起了：

| | 现状 |
| --- | --- |
| Git 里 Prisma | `cqim-app/prisma/schema.prisma` 的 `provider = "postgresql"` |
| 线上 wed.imim.chat | **仍可能在跑 SQLite 文件**（`dev.db`），直到你改 `DATABASE_URL` 并 `migrate deploy` |
| Mongo | **不是** Prisma 主库。compose 里可能还有 mongo 服务，不要填进 `DATABASE_URL` |
| Redis | 在线、缓存、`cqim:im:push` |
| Go Gateway | 仍可读 `DB_PATH` 默认 sqlite，迁库后要改 |

切 PG 见 [cqim-app/docs/MIGRATE_POSTGRES.md](./cqim-app/docs/MIGRATE_POSTGRES.md)。
Git 已有 `cqim-app/prisma/migrations/20260913225500_init_postgres/`。线上仍要机主改 `DATABASE_URL` 并 `prisma migrate deploy`（本仓不改 wed 机器）。

---

## 技术栈

- Web：React + Vite（`cqim-app/client`）
- API：Node + Express（`cqim-app/server`）
- 网关：Go（`cqim-app/go-gateway`）
- 私聊：强制 `msgType=encrypted`
- 通话：TRTC SDKAppID **1600159677**（禁止 1600136830）

---

## 本地

```bash
cd cqim-app
cp .env.example .env   # DATABASE_URL 必须是 postgresql://...
npx prisma migrate deploy  # 空 PG 应用 init_postgres
docker compose up -d --build
```

部署：[cqim-app/docs/DEPLOY.md](./cqim-app/docs/DEPLOY.md)  
双机：[cqim-app/docs/TWO_NODES.md](./cqim-app/docs/TWO_NODES.md)

EdgeOne 源站只回第一台，不要源站组轮询 WebSocket。禁止 NFS 共享 `.db`。

---

## 还没做完

1. 生产切 PostgreSQL：Git 侧 SQL 已提交，机主仍须设 `DATABASE_URL` + `migrate deploy`
2. APNs：仅 foreground 免推
3. Gateway 离开 sqlite
4. iOS 与 Web 对齐（另仓）

## 版权

本仓为私有仓，**不开源**。保留所有权利。未经授权不得复制、发布或再授权。
GitHub「许可」显示「无」是正确的。
