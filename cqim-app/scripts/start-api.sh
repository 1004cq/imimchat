#!/bin/sh
set -e
cd /app

if [ -f /app/node_modules/prisma/build/index.js ]; then
  PRISMA_CLI="node /app/node_modules/prisma/build/index.js"
elif [ -x /app/node_modules/.bin/prisma ]; then
  PRISMA_CLI="/app/node_modules/.bin/prisma"
else
  echo "prisma CLI not found (expected node_modules/prisma)" >&2
  exit 1
fi

i=0
until $PRISMA_CLI migrate deploy; do
  i=$((i + 1))
  if [ "$i" -ge 15 ]; then
    echo "prisma migrate deploy failed after ${i} attempts" >&2
    exit 1
  fi
  echo "prisma migrate deploy: waiting for PostgreSQL (attempt ${i}/15)"
  sleep 2
done

exec node dist/index.js
