#!/usr/bin/env bash
# One-machine Compose deploy for cqim-app (see docs/DEPLOY.md).
set -euo pipefail

APP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$APP_DIR"

if [[ ! -f .env ]]; then
  echo "ERROR: $APP_DIR/.env is missing."
  echo "Copy .env.example to .env and fill secrets, then re-run:"
  echo "  cp .env.example .env"
  exit 1
fi

env_value() {
  local key="$1"
  local line
  line="$(grep -E "^${key}=" .env | tail -n 1 || true)"
  if [[ -z "$line" ]]; then
    printf '%s' ""
    return 0
  fi
  printf '%s' "${line#*=}"
}

MINIO_ROOT_USER="$(env_value MINIO_ROOT_USER)"
MINIO_ROOT_PASSWORD="$(env_value MINIO_ROOT_PASSWORD)"
DATABASE_URL="$(env_value DATABASE_URL)"

if [[ -z "$MINIO_ROOT_USER" ]]; then
  echo "ERROR: MINIO_ROOT_USER is empty in .env"
  exit 1
fi
if [[ -z "$MINIO_ROOT_PASSWORD" ]]; then
  echo "ERROR: MINIO_ROOT_PASSWORD is empty in .env"
  exit 1
fi
if [[ -z "$DATABASE_URL" ]]; then
  echo "ERROR: DATABASE_URL is empty in .env"
  exit 1
fi
if [[ "$DATABASE_URL" == file:* || "$DATABASE_URL" == *file:./dev.db* ]]; then
  echo "ERROR: DATABASE_URL must be postgresql://..., not file:./dev.db"
  exit 1
fi
if [[ "$DATABASE_URL" != postgresql://* && "$DATABASE_URL" != postgres://* ]]; then
  echo "ERROR: DATABASE_URL must start with postgresql:// (got: ${DATABASE_URL%%:*})"
  exit 1
fi

mkdir -p certbot/www certbot/conf data

echo "==> docker compose up -d --build"
docker compose up -d --build

echo
echo "==> docker compose ps"
docker compose ps

echo
echo "When nginx and cqim are up, probe the Node API through Nginx:"
echo "  curl -fsS http://127.0.0.1/api/health"
echo
echo "MinIO is not published on the host. Probe inside the container:"
echo "  docker compose exec minio /usr/local/bin/busybox wget -q -O /dev/null http://127.0.0.1:9000/minio/health/live && echo minio-live"
