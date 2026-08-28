#!/usr/bin/env bash
# 在 node1（42.194.167.201）上执行：允许 node3 接入 NFS / Redis / Nginx upstream
set -euo pipefail

NODE3_HOST="${NODE3_HOST:-103.24.217.244}"
NODE2_HOST="${NODE2_HOST:-106.53.196.247}"
EXPORT_LINE="/home/ubuntu/cqim_shared/data ${NODE3_HOST}(rw,sync,no_subtree_check,no_root_squash)"

echo "==> NFS：允许 ${NODE3_HOST} 挂载共享 SQLite"
if ! grep -qF "$NODE3_HOST" /etc/exports 2>/dev/null; then
  echo "$EXPORT_LINE" | tee -a /etc/exports
  exportfs -ra
fi

echo "==> UFW：允许 node3 访问 Redis/NFS"
ufw allow from "$NODE3_HOST" to any port 6380 proto tcp comment 'cqim-redis-node3' || true
ufw allow from "$NODE3_HOST" to any port 2049 proto tcp comment 'cqim-nfs-node3' || true

echo "==> 提示：请在 node3 安全组/ufw 放行 node1(42.194.167.201) 访问 3011、8082"

echo "==> 更新 Nginx upstream（三节点）"
UPSTREAM_FILE="/etc/nginx/conf.d/cqim-upstreams.conf"
cat > "$UPSTREAM_FILE" <<EOF
map \$http_upgrade \$connection_upgrade {
    default upgrade;
    ''      close;
}

upstream cqim_api_nodes {
    least_conn;
    server 127.0.0.1:3011 max_fails=3 fail_timeout=30s;
    server ${NODE2_HOST}:3011 max_fails=3 fail_timeout=30s;
    server ${NODE3_HOST}:3011 max_fails=3 fail_timeout=30s;
}

upstream cqim_signal_nodes {
    ip_hash;
    server 127.0.0.1:3011 max_fails=3 fail_timeout=30s;
    server ${NODE2_HOST}:3011 max_fails=3 fail_timeout=30s;
    server ${NODE3_HOST}:3011 max_fails=3 fail_timeout=30s;
}

upstream gateway_ws_nodes {
    ip_hash;
    server 127.0.0.1:8082 max_fails=3 fail_timeout=30s;
    server ${NODE2_HOST}:8082 max_fails=3 fail_timeout=30s;
    server ${NODE3_HOST}:8082 max_fails=3 fail_timeout=30s;
}
EOF

nginx -t
systemctl reload nginx

echo "==> node1 已加入 node3（${NODE3_HOST}）"
echo "    请在 node3 执行: bash deploy/scripts/node3-join-cluster.sh"
