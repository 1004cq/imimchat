#!/usr/bin/env bash
# Bind im.cq.je to the Docker Compose CQIM stack and issue a Let's Encrypt certificate.
set -euo pipefail

APP_DIR="${APP_DIR:-/home/ubuntu/cqim-app.new}"
COMPOSE=(docker compose)
DOMAIN="im.cq.je"

cd "$APP_DIR"

mkdir -p certbot/www certbot/conf nginx

echo "==> Installing HTTP bootstrap Nginx (ACME + app proxy)"
cp -f nginx/http-bootstrap.conf nginx/default.conf

if ! grep -q '443:443' docker-compose.yml; then
  echo "WARNING: docker-compose.yml does not publish 443; HTTPS will not be reachable until it does."
fi

${COMPOSE[@]} up -d --force-recreate nginx
${COMPOSE[@]} exec -T nginx nginx -s reload || true

echo "==> Requesting Let's Encrypt certificate for ${DOMAIN}"
docker run --rm \
  -v "${APP_DIR}/certbot/www:/var/www/certbot" \
  -v "${APP_DIR}/certbot/conf:/etc/letsencrypt" \
  certbot/certbot certonly \
    --webroot \
    -w /var/www/certbot \
    -d "$DOMAIN" \
    --agree-tos \
    --register-unsafely-without-email \
    --non-interactive \
    --keep-until-expiring

if [[ ! -f certbot/conf/live/${DOMAIN}/fullchain.pem ]]; then
  echo "ERROR: certificate was not issued. Keeping HTTP-only Nginx so the site stays up."
  exit 1
fi

echo "==> Switching Nginx to HTTPS production config"
cp -f nginx/https.conf nginx/default.conf

${COMPOSE[@]} up -d nginx
${COMPOSE[@]} exec -T nginx nginx -t
${COMPOSE[@]} exec -T nginx nginx -s reload

echo "==> Domain bind complete. Test: https://${DOMAIN}/api/health"
