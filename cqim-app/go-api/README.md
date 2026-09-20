# Go API

Go 1.22+ service using net/http, pgx and go-redis. It is a parallel API for parity testing; Node remains the default production API.

## Build and run

From cqim-app, load the same .env used by Node:

```bash
set -a
. ./.env
set +a
go build -o ./go-api/bin/go-api ./go-api/cmd/go-api
./go-api/bin/go-api
```

It listens on 127.0.0.1:8089 by default. GO_API_LISTEN_ADDR can override it. DATABASE_URL and REDIS_URL (or REDIS_ADDR) are read unchanged; a Prisma-only schema query parameter is removed only for pgx's connection setup.

## Parity curl

Use the same existing user token against Node and Go:

```bash
curl -i -H "Authorization: Bearer <token>" http://127.0.0.1:3000/api/auth/me
curl -i -H "Authorization: Bearer <token>" http://127.0.0.1:8089/api/me
curl -i http://127.0.0.1:3000/api/health
curl -i http://127.0.0.1:8089/api/health
```

## Implemented

- GET /api/health: Node-compatible ok/service/checks/env/uptime/timestamp fields plus postgres and redis booleans.
- GET /api/me: compatible user envelope and UserSession Bearer authentication.

Node user authentication is an opaque UserSession token, not JWT. server/auth.ts does not read a Cookie for this user token, so Go accepts the same Authorization: Bearer value and does not add a new Cookie convention. Expired tokens return 401; banned users return the Node-compatible 403 error and reason.

## Not migrated

All remaining /api routes, including auth login/registration, friends, chats, groups, presence, media, APNs, web push, moments, stickers, QR, admin, crypto, MLS, channels and other registered Node routes.

The Compose go-api service is behind the explicit go-api profile and is off by default. Nginx and cqim-app/server are not changed; the default /api continues to use Node.
