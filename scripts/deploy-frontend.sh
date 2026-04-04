#!/bin/bash
# imimchat 前端自定义文件部署脚本
# 用途：将 frontend/ 目录下的自定义 JS/CSS 文件注入到 Web 容器中
# 使用方法：bash scripts/deploy-frontend.sh

set -e

CONTAINER="tsdd-tangsengdaodaoweb-1"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"

echo "=== imimchat 前端文件部署 ==="
echo "容器: $CONTAINER"
echo "源目录: $REPO_ROOT/frontend"

# 检查容器是否运行
if ! docker ps --format '{{.Names}}' | grep -q "$CONTAINER"; then
  echo "错误: 容器 $CONTAINER 未运行"
  exit 1
fi

# 备份原始文件
echo "备份原始文件..."
BACKUP_TAG=$(date +%Y%m%d_%H%M%S)
docker exec "$CONTAINER" cp /usr/share/nginx/html/static/js/imim_adaptive.js \
  /usr/share/nginx/html/static/js/imim_adaptive.js.bak_$BACKUP_TAG 2>/dev/null || true
docker exec "$CONTAINER" cp /usr/share/nginx/html/static/css/mobile.css \
  /usr/share/nginx/html/static/css/mobile.css.bak_$BACKUP_TAG 2>/dev/null || true

# 复制新文件
echo "部署 imim_adaptive.js..."
docker cp "$REPO_ROOT/frontend/static/js/imim_adaptive.js" \
  "$CONTAINER:/usr/share/nginx/html/static/js/imim_adaptive.js"

echo "部署 mobile.css..."
docker cp "$REPO_ROOT/frontend/static/css/mobile.css" \
  "$CONTAINER:/usr/share/nginx/html/static/css/mobile.css"

echo "=== 部署完成 ==="
echo "备份标签: $BACKUP_TAG"
echo "如需回滚，执行："
echo "  docker exec $CONTAINER cp /usr/share/nginx/html/static/js/imim_adaptive.js.bak_$BACKUP_TAG /usr/share/nginx/html/static/js/imim_adaptive.js"
