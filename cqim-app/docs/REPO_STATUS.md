# cqim-app 现状（2026-09-13）

- schema.prisma provider = postgresql
- `prisma/migrations/` 已有 `init_postgres` SQL + `provider = postgresql` lock
- 生产切流（机主设 DATABASE_URL + migrate deploy）见 MIGRATE_POSTGRES.md
- Gateway 默认仍可指 sqlite DB_PATH
