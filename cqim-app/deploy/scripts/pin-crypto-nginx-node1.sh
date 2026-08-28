#!/usr/bin/env bash
# 在 node1 宿主机 Nginx 中固定 /api/crypto/ 到本机 3011，避免 least_conn 打到 NFS 从节点
# 在 node1 上以 root/sudo 执行
set -euo pipefail

SITE="${NGINX_SITE:-/etc/nginx/sites-enabled/wed.imim.chat}"
MARKER='location ^~ /api/crypto/'

if grep -q "$MARKER" "$SITE" 2>/dev/null; then
  echo "已存在 crypto 固定路由，跳过"
  exit 0
fi

python3 - <<'PY'
from pathlib import Path
import os
site = Path(os.environ.get("NGINX_SITE", "/etc/nginx/sites-enabled/wed.imim.chat"))
text = site.read_text()
block = """
    location ^~ /api/crypto/ {
        proxy_pass http://127.0.0.1:3011;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

"""
needle = "    location /api/ {"
if needle not in text:
    raise SystemExit(f"未找到 {needle!r}，请手动编辑 {site}")
site.write_text(text.replace(needle, block + needle, 1))
print(f"已在 {site} 插入 /api/crypto/ 固定路由")
PY

nginx -t
systemctl reload nginx
echo "完成：/api/crypto/ 已固定到 127.0.0.1:3011"
