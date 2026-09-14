# cqim-app 现状（2026-09-13）

- schema.prisma provider = postgresql
- 生产切流见 MIGRATE_POSTGRES.md
- Gateway 仍是 sqlite-only，但空 `DB_PATH` 与共享 `cqim.db` 会拒绝启动
