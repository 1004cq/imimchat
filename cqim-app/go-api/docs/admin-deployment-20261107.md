# Go admin deployment · 2026-11-07

The primary server runs the Go admin API on loopback port 8089 and the updated Go WebSocket gateway on loopback port 8082. The public admin URL continues to reach Node. The release runs from Git commit `7cfbf105bce79e12bcb8ea06bdf3229abbd7b945` on `fix/e2ee-key-lifecycle`.

| Service | Previous image ID (SHA-256) | Running image ID (SHA-256) | Running executable SHA-256 |
| --- | --- | --- | --- |
| Go API | `23cc6a256832327b884c9e1e1feb29addfed353b8d7acff30b84077f3292d927` | `eed942692c491df7c045f240e872c0de071d56b21942d09f906f318e9327c7e1` | `a1193b142a88be8d8fb5393b7356a4357d4e863ac540833fa5e78b6b569889ec` |
| Go gateway | `9c792637a9e82b590bb6b7cdfc954d82212b0c4f8e158efbe794371ae1afdb1c` | `dc8d6ac9cb84d1c29ef06eeab8901a98c310dbdd2a79ab7c700301730dd12112` | `746c7617a99426b9701761ac434b26b8b23b86cee3050ddb53cbd7e80fa773a2` |

The production checks at 11:19 UTC returned HTTP 200 for the public health endpoint and healthy status for both containers. The Go API reached PostgreSQL and Redis. Anonymous admin requests and invalid Bearer and cookie credentials returned 401. The gateway uses the same PostgreSQL session database as Node. Nginx configuration hash and container start times for Node, Nginx, PostgreSQL, Redis and MinIO matched the pre-deployment snapshot. The release performed no migrations.

The [integration report](admin-validation-20261106.md) describes same-token Node/Go comparisons on a disposable database. The production database had zero unexpired administrator sessions at the final check. Authenticated production parity, an actual client disconnect and the secondary gateway still require verification.

The server release directory is `/root/imim-admin-release-20261106-cd19002`. It holds the saved images, preserved containers, runtime manifest, and executable rollback scripts. Run the following there as the release operator. The first command only verifies the deployment and rollback container identities. The second command performs the rollback, exporting the current gateway SQLite state before starting the retained containers.

```sh
cd /root/imim-admin-release-20261106-cd19002
./rollback-with-state.py --check
# When rolling back:
./rollback-with-state.py
```

The identity preflight passed. The live rollback itself was not exercised. This deployment replaced the two running containers directly. A later Compose recreation must select the recorded images and gateway session configuration to reproduce this release.
