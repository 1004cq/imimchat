# CQIM Prisma PostgreSQL 迁移

本仓库的 Prisma datasource 使用 PostgreSQL：

```prisma
provider = "postgresql"
url      = env("DATABASE_URL")
```

以下步骤按数据库实际状态选择。它们不代表任何现有环境已经完成 PostgreSQL 切换。

## 空 PostgreSQL 数据库

在 `.env` 配置目标库后执行：

```bash
cd cqim-app
# DATABASE_URL=postgresql://user:pass@127.0.0.1:5432/cqim?schema=public
npx prisma migrate deploy
npx prisma generate
```

`migrate deploy` 会按 `prisma/migrations/` 中的顺序创建表、约束和索引；它不使用 `prisma db push` 代替迁移记录。

## 已有 SQLite 数据

1. 停止旧 SQLite 写入，并备份 `.db`、`-wal` 和 `-shm` 文件。不要把 SQLite 文件当作 PostgreSQL 的共享数据源。
2. 创建一个新的空 PostgreSQL 数据库，并用上节命令执行全部 Prisma 迁移。
3. 确认 `.env` 的 `DATABASE_URL` 指向该 PostgreSQL 数据库，然后执行已提供的一次性导入工具：

   ```bash
   cd cqim-app
   # DATABASE_URL 必须是 PostgreSQL；参数是旧 SQLite 文件路径
   pnpm db:migrate:sqlite-to-postgres /secure-backup/cqim.db
   npx prisma generate
   ```

4. 对导入结果执行账户、私聊会话、私聊消息、群成员和群消息抽样核对后，再让应用使用 PostgreSQL 的 `DATABASE_URL`。

该导入工具不会改变 E2EE 消息内容；`content`、`extra`、MLS 字段和 BigInt 群消息序列号按原值迁移。
