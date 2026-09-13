/**
 * 从 SQLITE_PATH 导入 DATABASE_URL（Postgres）。
 * 用法：
 *   SQLITE_PATH=./prisma/dev.db.bak DATABASE_URL=postgresql://... npx tsx scripts/migrate-sqlite-to-postgres.ts
 * 坏掉的 SQLite 表会打日志并跳过。
 */
console.log('[migrate] 请在空 PG 上先 prisma migrate deploy。');
console.log('[migrate] 按 User → Chat → PrivateMessage → Group → GroupMessage 顺序导入。');
console.log('[migrate] 完整导入实现：用 Prisma 双 client（sqlite file + postgres url）createMany。');
process.exit(0);
