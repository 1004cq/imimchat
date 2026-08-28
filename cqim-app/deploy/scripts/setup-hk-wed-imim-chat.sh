#!/usr/bin/env bash
# 在香港节点配置 wed.imim.chat SSL + SSH 密钥登录
set -euo pipefail

DOMAIN=wed.imim.chat
NGINX_SITE=/etc/nginx/sites-available/${DOMAIN}
NGINX_ENABLED=/etc/nginx/sites-enabled/${DOMAIN}
CQIM_UPSTREAM="${CQIM_UPSTREAM:-127.0.0.1:999}"

echo "==> 安装依赖"
apt-get update -qq
apt-get install -y -qq certbot python3-certbot-nginx sshpass 2>/dev/null || true

echo "==> HTTP 引导（ACME）"
mkdir -p /var/www/html/.well-known/acme-challenge
cat > /etc/nginx/sites-available/${DOMAIN}.bootstrap <<EOF
server {
    listen 80;
    listen [::]:80;
    server_name ${DOMAIN};

    location /.well-known/acme-challenge/ {
        root /var/www/html;
        allow all;
    }

    location / {
        return 200 'ok';
        add_header Content-Type text/plain;
    }
}
EOF

ln -sf /etc/nginx/sites-available/${DOMAIN}.bootstrap "$NGINX_ENABLED"
nginx -t && systemctl reload nginx

echo "==> 申请 Let's Encrypt 证书"
certbot certonly --webroot -w /var/www/html \
  -d "${DOMAIN}" \
  --agree-tos --register-unsafely-without-email \
  --non-interactive --keep-until-expiring || {
    echo "WARN: webroot 验证失败，尝试 nginx 插件..." >&2
    certbot certonly --nginx -d "${DOMAIN}" \
      --agree-tos --register-unsafely-without-email \
      --non-interactive --keep-until-expiring
  }

echo "==> 部署 HTTPS Nginx"
sed "s|127.0.0.1:999|${CQIM_UPSTREAM}|g" "$(dirname "$0")/../nginx-wed.imim.chat.conf" > "$NGINX_SITE"
ln -sf "$NGINX_SITE" "$NGINX_ENABLED"
rm -f /etc/nginx/sites-available/${DOMAIN}.bootstrap
nginx -t && systemctl reload nginx

echo "==> 配置 SSH 公钥（node1/node2 同款密钥可登录）"
mkdir -p /root/.ssh
chmod 700 /root/.ssh
touch /root/.ssh/authorized_keys
chmod 600 /root/.ssh/authorized_keys

if [[ -n "${NODE1_PUBKEY:-}" ]]; then
  grep -qF "$NODE1_PUBKEY" /root/.ssh/authorized_keys 2>/dev/null || echo "$NODE1_PUBKEY" >> /root/.ssh/authorized_keys
fi
if [[ -n "${NODE2_PUBKEY:-}" ]]; then
  grep -qF "$NODE2_PUBKEY" /root/.ssh/authorized_keys 2>/dev/null || echo "$NODE2_PUBKEY" >> /root/.ssh/authorized_keys
fi

# ubuntu 用户（若存在）
if id ubuntu &>/dev/null; then
  mkdir -p /home/ubuntu/.ssh
  chmod 700 /home/ubuntu/.ssh
  touch /home/ubuntu/.ssh/authorized_keys
  chmod 600 /home/ubuntu/.ssh/authorized_keys
  if [[ -n "${NODE1_PUBKEY:-}" ]] && ! grep -qF "$NODE1_PUBKEY" /home/ubuntu/.ssh/authorized_keys 2>/dev/null; then
    echo "$NODE1_PUBKEY" >> /home/ubuntu/.ssh/authorized_keys
  fi
  if [[ -n "${NODE2_PUBKEY:-}" ]] && ! grep -qF "$NODE2_PUBKEY" /home/ubuntu/.ssh/authorized_keys 2>/dev/null; then
    echo "$NODE2_PUBKEY" >> /home/ubuntu/.ssh/authorized_keys
  fi
  chown -R ubuntu:ubuntu /home/ubuntu/.ssh
fi

echo "==> 完成"
echo "    HTTPS: https://${DOMAIN}/"
echo "    SSH:   ssh -i node1.pem root@${DOMAIN}  或  ssh -i node2.pem ubuntu@${DOMAIN}"
