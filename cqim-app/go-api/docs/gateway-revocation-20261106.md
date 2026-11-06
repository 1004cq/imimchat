# Gateway session revocation — 2026-11-06

Follow-up to the Go admin implementation. Base checkout commit:
`314a4b4a0dc563ea3324714c07fe281562f15804`.

## Behavior

- `cqim:im:push` / `session_revoked` causes database revalidation and WebSocket close 1008 for revoked/banned sessions.
- Rechecks after HTTP upgrade, before inbound commands, on Pong and during the 30-second presence sweep. No positive session cache.
- Delayed events revalidate the current token rather than blindly disconnecting a fresh session after unban.
- Disconnect uses an idempotent close signal; fan-out never sends into a closed channel.
- Redis ownership check and deletion are atomic. Old-node heartbeats cannot overwrite a newer node; same-node replacement cleanup cannot delete the replacement's presence.
- Session query timeout is two seconds and errors fail closed. Database outages can therefore disconnect clients rather than allow unverified access.
- Periodic sweeps are sequential; 30 seconds is the timer interval, not a hard upper bound under database latency/load. This is not a throughput benchmark.

## Configuration and boundaries

For shared Node/Go API sessions, set gateway `DATABASE_URL` to the same PostgreSQL database/schema.
Prisma's `schema` URL option is translated to a quoted PostgreSQL search_path. No secrets are recorded here.
With no `DATABASE_URL`, legacy SQLite authentication remains available; invalid PostgreSQL settings never fall back silently.

Only Session verification gains PostgreSQL support. Existing gateway group-message storage remains SQLite.
This change does not claim a full gateway storage migration or compatibility of all production routing paths.
No Node, Nginx, Prisma schema, Compose behavior or frontend changes were made. No production deployment or Git push.

Admin API retains `disconnectConfirmed:false`: Redis publication is not a per-node acknowledgement.
All production nodes must run the consumer with the correct session source before production kick acceptance.

## Reproducible checks

From go-api:

```sh
go build -o /tmp/imim-admin-go-test ./cmd/go-api
go vet ./...
```

From go-gateway (only disposable local services):

```sh
ADMIN_TEST_DATABASE_URL='postgres://postgres@127.0.0.1:55439/imim_admin_test?sslmode=disable' \
ADMIN_TEST_REDIS_ADDR='127.0.0.1:56389' \
ADMIN_TEST_API_BINARY=/tmp/imim-admin-go-test \
go test -race ./... -count=3
go vet ./...
go build -o /tmp/imim-gateway-test ./cmd
```

The opt-in integration test refuses a database name other than `imim_admin_test`, creates a random schema using existing migrations, and uses Redis DB 15 with random fixture IDs. Never point it at production. No FLUSHDB. Without environment variables it explicitly skips.

The integration test runs a real Go admin HTTP process plus two local WebSocket gateways, PostgreSQL 16 and Redis 7. It verifies ban closes both sockets, revoked-token handshake returns 401, revoked heartbeats cannot recreate presence, unban needs a new token, delayed revoke preserves a new token, and replacement cleanup preserves presence. A third gateway intentionally omits Pub/Sub and proves command and sweep revalidation still close invalid sessions. It does not exercise app UI, group-message persistence, production routing or load.

## Executed results

- Go gateway `go test -race ./... -count=3`: passed, including the opt-in real transport integration test (not skipped).
- Go API `go test -race ./... -count=1`: passed, including PostgreSQL/Redis admin regressions.
- Both modules `go vet ./...` and builds: passed.
- `git diff --check`: passed.
- The stress-test producer initially leaked into its later fresh-login phase and caused an unexpected test message; its lifecycle is now bounded/idempotent and the final three-run suite passes.
- Only the dedicated local test containers were used. They are stopped after verification; their data is retained.
- Real iOS/browser UI and production acceptance are still unverified.

## Source SHA-256

- `cqim-app/go-gateway/cmd/main.go`: `7195de1cecf15036c47830e64c80510b1d0c9b988fc9675081e61d31cde3eb52`
- `cqim-app/go-gateway/go.mod`: `b9b380da01f88d31d6bbe9a0e7390c2b6e72d7ddf630dec115047efee8827feb`
- `cqim-app/go-gateway/go.sum`: `1bb595461fce39d191dadb387cc32d396e352559f8c0f2597aa888d499cc9d1c`
- `cqim-app/go-gateway/internal/auth/auth.go`: `387cab569fbf32cdfd9c6f5e4b402ae3eafe49d2ddf93ce302c45ec38a2b2388`
- `cqim-app/go-gateway/internal/auth/auth_test.go`: `166e236e88a2536282ede3b5d44927165002d8ab20d10ea695c854c258a6572b`
- `cqim-app/go-gateway/internal/gateway/gateway.go`: `ac4e76797e2042d826b79e298d30e5d94aed93214d638fb871bf2aeabc40c628`
- `cqim-app/go-gateway/internal/gateway/session.go`: `d75d8291ad5a17fba5bb3a0834363dbab07b5250c2ac1ebd0a79b924add15c74`
- `cqim-app/go-gateway/internal/gateway/presence.go`: `4b831c40de2b3b829ad4968519c818ce3d0f8e1859cb09a6f0b3062e5ad51993`
- `cqim-app/go-gateway/internal/gateway/revocation_integration_test.go`: `ac3cbc237d1be40adcd194b1ffd8679979e9bf293176c2f0d57d7ffefe090dd6`
