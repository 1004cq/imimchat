# AGENTS.md

## Cursor Cloud specific instructions

This repo is a **deployment/ops repo** for the `imimchat` IM system (WuKongIM + TangSengDaoDao).
The full application runs as prebuilt Docker images via `docker/docker-compose.yaml`. The only
source code developed in this repo is `register-service/` (a Python Flask middleware) and the
`frontend/` static customization files (`imim_adaptive.js`, `mobile.css`). There is no test suite
or linter configured. Standard run/ops commands live in `README.md`, `docs/handover_en.md`, and
`scripts/manage.sh`.

### Docker daemon (must start manually)
This VM has no systemd, so Docker is not auto-started. Start it before using compose:
`sudo dockerd` (run it in a background tmux session; logs go to the session).
`/etc/docker/daemon.json` is already configured for this nested VM (storage-driver
`fuse-overlayfs` and `containerd-snapshotter: false`, required for Docker 29 + fuse-overlayfs).
Pulled images and the daemon config persist in the VM snapshot; the running `dockerd` process does not, so restart it each session.

### Bringing up the full stack
- `docker/.env` is gitignored. Create it once: `cp docker/.env.example docker/.env`, then set
  `EXTERNAL_IP=127.0.0.1` and real values for `MYSQL_ROOT_PASSWORD`, `MINIO_ROOT_PASSWORD`,
  `WK_JWT_SECRET`, `TS_ADMINPWD`.
- Start: `cd docker && sudo docker compose up -d`.
- **Gotcha:** the first `up` often ends with `tangsengdaodaoserver is unhealthy` and
  web/manager not started. The server is actually fine — its healthcheck just needs ~30-40s.
  Wait until `curl http://localhost:8090/v1/ping` returns `{"status":200}`, then re-run
  `docker compose up -d` to start `tangsengdaodaoweb` / `tangsengdaodaomanager` (they
  `depend_on: service_healthy`).
- Images come from `registry.cn-shanghai.aliyuncs.com` (Aliyun Shanghai); pulls work from this VM
  but can be slow on a cold cache.

### Ports
web `82`, admin/manager `83`, business API `8090`, WuKongIM ws `5200` / tcp `5100` / monitor
`5300`, MinIO `9000`/`9001`, adminer `8306` (localhost only). WuKongIM internal HTTP API `5001`
is only reachable from inside the compose network (e.g. `docker exec docker-wukongim-1 ...`).

### Web login zone gotcha (important for GUI testing)
The public `tangsengdaodaoweb` image defaults the phone country code to **`0086`**, so it logs in
with `username = "0086" + phone`. The backend stores `username = zone + phone`. To log in through
the web UI at `:82`, register/login users with **`zone: "0086"`** (then the web username matches,
e.g. phone `13800000001` → username `008613800000001`). Direct API calls instead typically use
`zone: "86"`. The repo's `frontend/imim_adaptive.js` rewrites `0086→86` to fix this, but it is only
wired into the project's custom-built web image, not the public image used here. The web container
proxies browser calls at `/api/` → `tangsengdaodaoserver:8090`.

### register-service (custom Flask middleware)
Not part of docker-compose. Runs standalone and defaults to `TSDD_API=http://localhost:8090`.
Deps live in a venv at `~/.venvs/register-service` (recreated by the startup update script).
Run: `SMS_PROVIDER=mock ~/.venvs/register-service/bin/gunicorn -w 1 -b 0.0.0.0:9091 app:app`
(from `register-service/`). In `mock` mode the SMS code is printed to the service log, not sent.
Backend registration uses a fixed SMS code `123456` (`TS_SMSCODE`); the middleware does the real
random-code verification and forwards the fixed code to the backend.

### Sending a chat message
Two friends are required before messaging (whitelist is on). Friend flow via API:
`POST /v1/friend/apply` (with target's `vercode` from `/v1/user/search`) → target lists
`GET /v1/friend/apply?api_version=1` → target `POST /v1/friend/sure` with the apply `token`.
A message can be injected server-side via the WuKongIM internal API:
`POST http://localhost:5001/message/send` (channel_type `1` = person, `payload` is base64 of
`{"type":1,"content":"..."}`). In the web UI, send by typing in the bottom editor and pressing Enter.
