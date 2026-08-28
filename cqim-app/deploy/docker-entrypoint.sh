#!/bin/sh
set -e
if [ -x ./node_modules/.bin/prisma ]; then
  echo "[entrypoint] 同步 SQLite schema..."
  ./node_modules/.bin/prisma db push --skip-generate
fi
exec "$@"
