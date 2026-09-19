#!/bin/sh
set -e
cd /app
npx prisma migrate deploy || true
exec node dist/index.js
