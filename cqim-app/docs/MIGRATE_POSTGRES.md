# SQLite → PostgreSQL

## 不要搞混的三件事

1. `schema.prisma` 已写 `provider = "postgresql"`
2. 线上若 `.env` 还是 `file:./dev.db`，运行中仍是 SQLite
3. Mongo 的 URL 不能当 Prisma `DATABASE_URL`

## 连接

```text
DATABASE_URL=postgresql://USER:PASS@内网IP:5432/cqim?schema=public
```

## 空库迁移

`prisma/migrations/` 已包含可在空 PostgreSQL 上 `prisma migrate deploy` 的 SQL。Compose 里 `cqim` 容器启动脚本会先跑 `pnpm exec prisma migrate deploy`，失败则不启动 Node。

本地对已有开发库追加迁移时再用：

```bash
cd cqim-app
npx prisma migrate dev --name describe_the_change
```

## 切流

备份 `.db` → 空库 migrate → 导数据 → 停写 → 改生产 DATABASE_URL → 起服务。  
禁止两台共享 SQLite。Gateway 迁完要离开 `DB_PATH`。
