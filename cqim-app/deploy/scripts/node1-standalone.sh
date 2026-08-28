#!/usr/bin/env bash
# 在 node1（42.194.167.201）上执行：切回单机模式，不再分发到 node2/node3
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UPSTREAM_FILE="/etc/nginx/conf.d/cqim-upstreams.conf"
UPSTREAM_SRC="${ROOT}/deploy/nginx-node1-standalone-upstreams.conf"

echo "==> 写入单机 upstream（仅 127.0.0.1:3011 / 8082）"
sudo cp "$UPSTREAM_SRC" "$UPSTREAM_FILE"

if ! grep -q 'location \^~ /api/crypto/' /etc/nginx/sites-enabled/cqim 2>/dev/null; then
  echo "==> 补充 /api/crypto/ 固定到本机（若尚未配置）"
  sudo python3 - <<'PY'
from pathlib import Path
site = Path("/etc/nginx/sites-enabled/cqim")
text = site.read_text()
block = """
    location ^~ /api/crypto/ {
        proxy_pass http://127.0.0.1:3011;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

"""
needle = "    location /api/ {"
if needle in text and "location ^~ /api/crypto/" not in text:
    site.write_text(text.replace(needle, block + needle, 1))
    print("已插入 /api/crypto/ 路由")
else:
    print("跳过：/api/crypto/ 已存在或找不到 /api/ 块")
PY
fi

echo "==> 校验并重载 Nginx"
sudo nginx -t
sudo systemctl reload nginx

echo "==> 检查本机 CQIM 容器"
docker ps --format 'table {{.Names}}\t{{.Status}}' | grep -E 'cqim-release|NAMES' || true

if curl -sf http://127.0.0.1:3011/api/health >/dev/null; then
  echo "OK: 本机 API /api/health"
else
  echo "WARN: 本机 3011 未响应，请检查 cqim-release-api 容器" >&2
fi

echo ""
echo "==> node1 已切换为单机模式"
echo "    - API/页面/WS 仅走 127.0.0.1:3011"
echo "    - 群聊 WS 仅走 127.0.0.1:8082"
echo "    - node2/node3 不再接收 wed.imim.chat 流量"
echo ""
echo "可选：停止对从节点的 NFS/Redis 暴露（确认不再接入集群后）"
echo "    sudo sed -i '/106.53.196.247/d;/103.24.217.244/d' /etc/exports && sudo exportfs -ra"
