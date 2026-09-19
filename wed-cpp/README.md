# wed-cpp

`wed-cpp` is the C++ replacement for the runtime currently hosted by `cqim-app/server`.
It uses Drogon 1.9.13 and has no Node.js runtime dependency. The existing web client can
remain a static bundle served by Nginx; this executable owns `/api` and WebSocket traffic
only after each compatibility stage is verified.

## Current executable

- `GET /api/health` confirms that the C++ runtime is reachable.
- `GET /api/migration-status` reports that production cutover is intentionally disabled.

It binds to `127.0.0.1:8081` by default. Set `WED_BIND` and `WED_PORT` for a private
reverse-proxy listener. Never expose it publicly before authentication and route parity
are complete.

## Build host prerequisites

- CMake 3.24 or newer
- C++20 compiler
- Drogon 1.9.13, built with PostgreSQL support
- PostgreSQL client development library and Redis client library for later adapters

```bash
cmake -S wed-cpp -B build/wed-cpp -DCMAKE_BUILD_TYPE=Release
cmake --build build/wed-cpp --parallel
WED_BIND=127.0.0.1 WED_PORT=8081 ./build/wed-cpp/wed_cpp
curl http://127.0.0.1:8081/api/health
```

## Migration contract

The following behavior is preserved while porting:

- Message `content` remains an opaque E2EE envelope. C++ does not decrypt, rewrite or
  derive message plaintext.
- PostgreSQL remains the datastore and Redis remains the presence/unread/pubsub layer.
- APNs uses the existing P8 environment variables on the server. No private key is
  committed to this repository.
- HTTP request and WebSocket message contracts must stay backward compatible with iOS
  and web clients.

## Cutover order

1. Implement shared C++ adapters: JWT/session validation, PostgreSQL, Redis, rate limits
   and structured logs.
2. Port `/api/auth`, `/api/profile`, `/api/presence`, push-token registration and health.
3. Port private chat persistence plus the WebSocket `private_send`, receipt, typing,
   recall and heartbeat messages.
4. Port group, MLS, media, Moments, friends, channels, calls and administration.
5. Run protocol regression tests against iOS and web clients. Route a canary to C++ via
   Nginx, then route all `/api` and `/ws` traffic to C++.
6. Stop Node only after API/WebSocket parity, push delivery and rollback checks pass.

Until step 6, do not remove the current Node service: this directory is a parallel,
non-production foundation rather than a partial replacement.
