# SQLite → PostgreSQL

## 不要搞混的三件事

1. `schema.prisma` 已写 `provider = "postgresql"`
2. 线上若 `.env` 还是 `file:./dev.db`，运行中仍是 SQLite
3. Mongo 的 URL 不能当 Prisma `DATABASE_URL`

## 连接

```text
DATABASE_URL=postgresql://USER:PASS@内网IP:5432/cqim?schema=public
```

## 还缺

仓里还没有一份可在空 PG 上直接 `prisma migrate deploy` 的 SQL。必须在空库执行：

```bash
cd cqim-app
npx prisma migrate dev --name init_postgres
```

再把 `prisma/migrations/` 提交回 Git。

## 切流

备份 `.db` → 空库 migrate → 导数据 → 停写 → 改生产 DATABASE_URL → 起服务。  
禁止两台共享 SQLite。Gateway 迁完要离开 `DB_PATH`。
