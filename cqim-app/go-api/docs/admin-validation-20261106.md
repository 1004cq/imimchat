# Go admin validation — 2026-11-06

Checkout: `imimchat-e2ee-fix`, branch `fix/e2ee-key-lifecycle`.
Base commit: `314a4b4a0dc563ea3324714c07fe281562f15804`.
Initial admin implementation was confined to `cqim-app/go-api/`; the requested follow-up
revocation fix additionally changes `cqim-app/go-gateway/`.
No production deployment, Nginx change, frontend change, schema change, commit or push was performed.

## Implemented

16 specified route/method combinations, shared AdminSession authentication, two-hour login TTL,
bcrypt/legacy upgrade, role checks, transactionally coupled audit and writes, user moderation,
Moments/comments moderation, real dashboard aggregates and explicit partial-failure reporting.
Unmigrated authenticated admin endpoints remain JSON 501.
User projections exclude password hashes; chat tables are queried only for aggregate metadata.

## Executed checks

- Go 1.27.3 (darwin/arm64), `go build ./cmd/go-api`: passed.
- `go vet ./...`: passed.
- `go test ./... -count=1`: passed, including disposable database integration.
- `go test -race ./... -count=1`: passed, including database integration.
- Final expanded `go test ./internal/api -run TestAdmin -v -count=1`: passed (credentials,
  UTC JSON serialization, and six integration subtests). Final additions verify nonzero message
  aggregates do not expose stored content and null profile edits correctly detect no-op updates.
- `git diff --check`: passed.

The PostgreSQL 16 / Redis 7 integration services ran locally and in isolation. Tests load existing
Prisma migrations into a random schema in `imim_admin_test`; the schema is removed after each run.
They test two-hour expiration, cookie/Bearer precedence, external-format sessions, SHA-256 upgrade,
login failure/throttle audit, password reset revocation, uniqueness and literal search, permissions,
audit-failure rollback, ban session/presence cleanup, Redis revocation publication, multi-page SCAN,
Redis failure, missing SQL metric, UTC day boundary, media/comment deletion and cascade, logout.
No production database, Redis, MinIO, app data or credentials were used.

## Actual Node/Go comparison

The unmodified Node `server/admin.ts` router ran with Express/Prisma against a separate local
comparison schema, while Go listened locally on port 8089 against that same schema and Redis.
With the same Node-issued Bearer token, the following response bodies matched after UTC date
normalization and exclusion of the Go-only `unavailableMetrics` field:

- GET /api/admin/me
- GET /api/admin/users?page=1
- GET /api/admin/users/:id
- GET /api/admin/moments?page=1
- GET /api/admin/dashboard (also included a local-midnight fixture)
- GET /api/admin/logs?page=1

A Go-issued token was accepted by Node. Logout through Go invalidated the token in both services
(both subsequently returned 401). Credentials were generated for the disposable fixture and were
not printed or stored in this document.

Comparison caught and fixed timestamp-without-time-zone JSON missing the UTC marker; daily
window parameters are also converted to UTC before querying Prisma Timestamp(3) columns.
Write routes were exercised against PostgreSQL/Redis by the Go integration suite, not exhaustively
paired against Node. Node does not implement the newly requested moment detail/comments routes.
The comparison harness selected fixture user fields explicitly because existing Prisma migrations
lack `User.webPushSubscription`, although the current schema declares it. No schema migration was
introduced to hide this existing mismatch.

## Follow-up gateway validation

The initial go-api-only implementation published revocation without an active gateway close
consumer. The subsequent requested fix implements that consumer and authoritative per-command
session checks. A real HTTP ban closed sockets on two local gateway nodes using isolated
PostgreSQL/Redis. See [gateway verification](gateway-revocation-20261106.md).

Production deployment and real-app acceptance remain unperformed. The API still returns
`disconnectConfirmed:false` because it has no per-node acknowledgement protocol.

## Initial admin implementation SHA-256 (before follow-up documentation edits)

- `internal/api/admin.go`: `5e53f0633a7858004f5eaa624d7684b40f2bb37afccc40a44fa247d98669119f`
- `internal/api/admin_write.go`: `789edda8c94b571dc5a955e71ae971669418d1fb7a1d323501065413513828fa`
- `internal/api/admin_moments.go`: `461c707cd472f679d8b7a3ebe7a4d77bd232ada070bf41cb4e23ba2cfcef3492`
- `internal/api/admin_test.go`: `dfb2b97c00cc232127ff308c599abeb2f6e2fd8e702af88c9797195b95103016`
- `internal/api/legacy.go`: `d6cf704ff3c8bef777019309f3d393800f1a01b9aaeda3490719c2f077d3538e`
- `README.md`: `7a2fc27855b090a4790eeb6192ecd7395d9fc2ebce950e93ac38548dfb81020e`

Reference Node admin.ts: `5919694e0cb5e24a08566f9d90f6a4b94f8bf29533abd0419a08c1094435d9d1`.
