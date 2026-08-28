#!/bin/sh
set -e
if [ "${SKIP_SCHEMA_PUSH:-}" != "true" ] && [ -x ./node_modules/.bin/prisma ]; then
  if [ ! -f ./data/cqim.db ] && [ ! -f /app/data/cqim.db ]; then
    echo "[entrypoint] 同步 SQLite schema..."
    ./node_modules/.bin/prisma db push --skip-generate
  else
    echo "[entrypoint] 跳过 schema push（共享库已存在）"
  fi
fi
exec "$@"
