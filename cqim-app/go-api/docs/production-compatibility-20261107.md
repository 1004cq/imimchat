# Production gateway compatibility — 2026-11-07

Deployment inspection found additional gateway features in the production host source that were
absent from the admin branch. The release preserves node-specific Redis routing, JSON node/connection
presence records, delivery deduplication, edge-mode metadata, fan-out safeguards, and queue draining.
Session revocation and atomic ownership checks are integrated with those features, not applied by
replacing the production implementation with an older gateway snapshot.

Compatibility source snapshot: SHA-256 of the downloaded source archive is listed below. It contains
only go-gateway source; no Node source, Nginx config, database data or credentials are committed.
The merge was prepared in an isolated directory, then copied into this checkout after tests passed.

Validation: real PostgreSQL/Redis admin-ban integration and existing routing tests passed under
`go test -race ./... -count=3`. API/Gateway startup, live configuration and production authentication
remain separate deployment acceptance checks; do not infer them from local tests.

Production source archive SHA-256: `103d6ba64292f2a2aca2d0155aa396b2c08649725f0e78c75a5c6a503790708d`.
