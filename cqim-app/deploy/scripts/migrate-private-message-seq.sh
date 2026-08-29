#!/usr/bin/env bash
# 为 PrivateMessage 表补齐 seq / clientMsgId，并部署匹配的 Prisma Client
# 用法（在已 build 的机器上，node1 上 DB 路径默认 /home/ubuntu/cqim_shared/data/cqim.db）：
#   npm run build && npx prisma generate
#   DB=/home/ubuntu/cqim_shared/data/cqim.db CONTAINER=cqim-release-api bash deploy/scripts/migrate-private-message-seq.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
CONTAINER="${CONTAINER:-cqim-release-api}"
DB="${DB:-/home/ubuntu/cqim_shared/data/cqim.db}"
TARGET="/app/node_modules/.pnpm/@prisma+client@6.19.0_prisma@6.19.0_typescript@5.6.3__typescript@5.6.3/node_modules/.prisma"

if [[ ! -f "$ROOT/node_modules/.prisma/client/index.js" ]]; then
  echo "请先在 $ROOT 执行: npx prisma generate" >&2
  exit 1
fi

echo "==> 备份并迁移 SQLite: $DB"
cp "$DB" "${DB}.bak-$(date +%Y%m%d-%H%M%S)"
sqlite3 "$DB" "ALTER TABLE PrivateMessage ADD COLUMN seq INTEGER NOT NULL DEFAULT 0;" 2>/dev/null || true
sqlite3 "$DB" "ALTER TABLE PrivateMessage ADD COLUMN clientMsgId TEXT;" 2>/dev/null || true
sqlite3 "$DB" "CREATE UNIQUE INDEX IF NOT EXISTS PrivateMessage_chatId_clientMsgId_key ON PrivateMessage(chatId, clientMsgId);"
sqlite3 "$DB" "CREATE INDEX IF NOT EXISTS PrivateMessage_chatId_seq_idx ON PrivateMessage(chatId, seq);"
sqlite3 "$DB" "
WITH numbered AS (
  SELECT id, ROW_NUMBER() OVER (PARTITION BY chatId ORDER BY datetime(createdAt)) AS new_seq
  FROM PrivateMessage
)
UPDATE PrivateMessage SET seq = (SELECT new_seq FROM numbered WHERE numbered.id = PrivateMessage.id);
"

echo "==> 打包 Prisma Client（含 debian-openssl-1.1.x engine）"
tar czf /tmp/prisma-client-deploy.tgz -C "$ROOT/node_modules/.prisma" client

echo "==> 部署到容器 $CONTAINER"
docker cp "$ROOT/prisma/schema.prisma" "$CONTAINER:/app/prisma/schema.prisma"
docker cp /tmp/prisma-client-deploy.tgz "$CONTAINER:/tmp/"
docker exec -u root "$CONTAINER" rm -rf "$TARGET/client"
docker exec -u root "$CONTAINER" mkdir -p "$TARGET"
docker exec -u root "$CONTAINER" tar xzf /tmp/prisma-client-deploy.tgz -C "$TARGET"
docker exec -u root "$CONTAINER" chown -R node:1001 "$TARGET/client" /app/prisma/schema.prisma

echo "==> 重启 $CONTAINER"
docker restart "$CONTAINER"
sleep 10
curl -sf "http://127.0.0.1:3011/api/health" && echo " OK"
