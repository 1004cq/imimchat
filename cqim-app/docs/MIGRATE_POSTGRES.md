# SQLite → 腾讯云 PostgreSQL

本页是切流操作说明。Git 已提交 `prisma/migrations/20260913225500_init_postgres/`，空 PG 可直接 `prisma migrate deploy`。

**本仓不替你改线上 `.env`，也不 SSH 腾讯云。** 下面命令由 wed 机主执行。

## 不要搞混的四件事

1. `schema.prisma` 已写 `provider = "postgresql"`
2. 线上若 `.env` 还是 `file:./dev.db` 或 `file:/app/data/cqim.db`，运行中仍是 SQLite
3. **Mongo 的 URL 不能当 Prisma `DATABASE_URL`**（compose 里可能还有 mongo 服务，那是遗留，不是主库）
4. **禁止 NFS 共享 `.db` 跨机器**。两节点必须连同一腾讯云 PG

## 腾讯云 PG 连接

控制台：云数据库 PostgreSQL → 实例 → 内网地址（VPC，不要对公网开 5432）。

```text
DATABASE_URL=postgresql://USER:PASS@内网IP:5432/cqim?schema=public
```

- 用户 / 库名在控制台创建；库用 UTF8
- 安全组 / PG 白名单只放 wed CVM 内网网段
- 若实例强制 SSL：`?schema=public&sslmode=require`
- 账号密码不要提交进 Git

## 机主操作（wed.imim.chat）

在已拉到含 `init_postgres` 的代码目录执行。路径按实际部署目录改（常见 `/home/ubuntu/cqim-release` 或仓内 `cqim-app`）。

```bash
set -euo pipefail
APP_DIR="${APP_DIR:-/home/ubuntu/cqim-release}"
cd "$APP_DIR"

# 1) 备份当前 SQLite（有哪个拷哪个；没有就跳过）
mkdir -p ./data ./backups
STAMP="$(date +%Y%m%d%H%M%S)"
for f in ./data/cqim.db ./prisma/dev.db ./prisma/data/cqim.db; do
  if [ -f "$f" ]; then
    cp -a "$f" "./backups/$(basename "$f").bak-$STAMP"
    echo "backed up $f"
  fi
done

# 2) 指向腾讯云 PG 内网（把 USER/PASS/HOST 换成控制台值）
#    禁止 mongodb:// 与 file:*.db
export DATABASE_URL='postgresql://USER:PASS@TENCENT_PG_HOST:5432/cqim?schema=public'

# 3) 空库上应用 Git 里的 init_postgres
#    不要在生产跑 prisma migrate dev（会写开发阴影库）
npx prisma migrate deploy

# 4) 确认
npx prisma migrate status

# 5) 可选：从备份 .db 导数据（脚本目前只是占位，导完再切流）
# SQLITE_PATH=./backups/cqim.db.bak-$STAMP DATABASE_URL="$DATABASE_URL" \
#   npx tsx scripts/migrate-sqlite-to-postgres.ts

# 6) 写入生产 .env 后重启 API（Gateway DB_PATH 仍是 sqlite，另项）
# 把 DATABASE_URL=postgresql://... 写进 .env
docker compose -f docker-compose.yml -f deploy/docker-compose.prod.yml up -d cqim
```

切流顺序：**备份 `.db` → 空库 `migrate deploy` → 导数据 → 停写 → 改生产 `DATABASE_URL` → 起服务**。

节点2 的 `.env` 必须是**同一条** `postgresql://`，不要再写 `file:/app/data/cqim.db`。

## 切流后还没做

- Go Gateway `DB_PATH` 默认 sqlite，迁完 Prisma 后要另改（本 PR 不动）
- 数据导入脚本 `scripts/migrate-sqlite-to-postgres.ts` 仍是占位
