# SQLite → PostgreSQL

Prisma `provider` 已改为 `postgresql`。切流前先在空库 `prisma migrate dev` / `deploy`，**不要**重放旧 SQLite migration。

## 连接串

```text
DATABASE_URL=postgresql://USER:PASS@内网IP:5432/cqim?schema=public
```

腾讯云 PG：同 VPC、禁公网、开自动备份。Mongo URL 不要再填进 `DATABASE_URL`。

## 切流

1. 备份 `prisma/dev.db`
2. 空 PG：migrate deploy
3. `npx tsx scripts/migrate-sqlite-to-postgres.ts`（坏表跳过）
4. 停 Node/Gateway → 改生产 `.env` → 起服务
5. 验证注册/登录/发私聊

禁止 NFS 共享 SQLite。Gateway 的 `DB_PATH` 仅遗留，迁完后改读 PG 或 Node API。
