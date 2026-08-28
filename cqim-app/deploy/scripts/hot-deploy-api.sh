#!/usr/bin/env bash
# 热更新 CQIM API（dist/index.js）到运行中的 Docker 容器
# 用法：在项目根目录执行
#   npm run build
#   CONTAINER=cqim-release-api bash deploy/scripts/hot-deploy-api.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
CONTAINER="${CONTAINER:-cqim-release-api}"
SRC="${SRC:-$ROOT/dist/index.js}"
PUBLIC_DIR="${PUBLIC_DIR:-$ROOT/dist/public}"

if [[ ! -f "$SRC" ]]; then
  echo "缺少 $SRC，请先 npm run build" >&2
  exit 1
fi

echo "==> 复制 $SRC -> $CONTAINER:/app/dist/index.js"
docker cp "$SRC" "$CONTAINER:/app/dist/index.js"

if [[ -d "$PUBLIC_DIR" ]]; then
  echo "==> 同步前端静态文件 -> $CONTAINER:/app/dist/public/"
  tar -C "$PUBLIC_DIR" -cf - . | docker exec -i "$CONTAINER" tar -xf - -C /app/dist/public
fi
echo "==> 重启 $CONTAINER"
docker restart "$CONTAINER"
echo "==> 等待健康检查..."
sleep 8
docker ps --filter "name=$CONTAINER" --format '{{.Names}} {{.Status}}'
curl -sf "http://127.0.0.1:3011/api/health" >/dev/null && echo "OK: /api/health" || echo "WARN: health check failed"
