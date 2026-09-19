# wed-cpp

C++ 并行后端，**不替代** 现网 Node。默认 `127.0.0.1:8088`，勿把生产 `/api` 整站切过来。

已有：
- `GET /api/health`
- `GET /api/ready`（有 libpq/hiredis 时探 PG + Redis）
- `GET /api/migration-status`（`cutoverReady=false`）
- `POST /api/presence`（需 Bearer，**尚未校验 Node JWT**）

```bash
sudo apt-get install -y g++ cmake libpq-dev libhiredis-dev
cmake -S wed-cpp -B build/wed-cpp
cmake --build build/wed-cpp -j
DATABASE_URL=postgresql://cqim:cqim@127.0.0.1:5432/cqim \
REDIS_HOST=127.0.0.1 WED_PORT=8088 ./build/wed-cpp/wed_cpp
```

下一步才是：校验 JWT、私聊落库、WebSocket 与 Node 字段对齐。做完之前不要停 Node。
