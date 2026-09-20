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

$PRISMA_CLI migrate deploy
exec node dist/index.js
