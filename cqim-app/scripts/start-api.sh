#!/bin/sh
set -e
cd /app
pnpm exec prisma migrate deploy
exec node dist/index.js
