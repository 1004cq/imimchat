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
  # 先清空 assets 再全量覆盖，避免 hash 变更后旧文件残留、以及 tar 覆盖权限失败
  docker exec -u root "$CONTAINER" bash -c 'rm -rf /app/dist/public/assets && mkdir -p /app/dist/public/assets'
  docker cp "$PUBLIC_DIR/index.html" "$CONTAINER:/app/dist/public/index.html"
  docker cp "$PUBLIC_DIR/assets/." "$CONTAINER:/app/dist/public/assets/"
  docker exec -u root "$CONTAINER" chown -R node:1001 /app/dist/public/index.html /app/dist/public/assets 2>/dev/null || true
fi
echo "==> 重启 $CONTAINER"
docker restart "$CONTAINER"
echo "==> 等待健康检查..."
sleep 8
docker ps --filter "name=$CONTAINER" --format '{{.Names}} {{.Status}}'
curl -sf "http://127.0.0.1:3011/api/health" >/dev/null && echo "OK: /api/health" || echo "WARN: health check failed"
