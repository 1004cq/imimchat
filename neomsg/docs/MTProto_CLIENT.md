# NeoMsg 客户端接入指南

## 1. 认证（JWT）

所有长连接（WebSocket / MTProto）需先通过 HTTP API 登录：

```bash
curl -s -X POST http://localhost:8090/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"phone":"13800000000","password":"password","device_id":"my-device-1"}'
```

响应：

```json
{
  "access_token": "eyJ...",
  "refresh_token": "eyJ...",
  "expires_in": 86400,
  "user_id": 1
}
```

`bindSession` / WebSocket 连接使用 `access_token`，JWT 内包含 `uid` 与 `did`，服务端会校验一致性。

---

## 2. Wire WebSocket 客户端（推荐 Web / 脚本）

**帧格式：** `[4 字节大端长度][WirePacket protobuf]`

### Go 示例

```bash
cd neomsg/backend
go run ./cmd/wire-client-demo/ \
  -api http://localhost:8090 \
  -ws ws://localhost:8080/ws \
  -device demo-go-1 \
  -chat 1 \
  -text "hello"
```

### Python 示例

```bash
pip install -r neomsg/examples/mtproto-client-python/requirements.txt
python neomsg/examples/mtproto-client-python/wire_ws_client.py
```

### 连接 URL

```
ws://localhost:8080/ws?token=<JWT>&device_id=<ID>&platform=web
```

---

## 3. MTProto TCP 客户端（类 Telegram 原生）

**端口：** `10443`（Abridged 首字节 `0xef`）

### 流程

```
TCP → DH 握手 → bindSession(JWT) → invokeWire(WirePacket) → wireResult(MessageAck)
                                                      ↓
                                            pushWire (其他设备)
```

### NeoMsg Bridge TL

| 方法 | Constructor | 说明 |
|------|-------------|------|
| bindSession | `0x6e656f01` | user_id, device_id, access_token |
| bindOk | `0x6e656f11` | bool |
| invokeWire | `0x6e656f02` | WirePacket bytes |
| wireResult | `0x6e656f12` | 多帧 Wire 响应 |
| pushWire | `0x6e656f13` | 服务端推送 |

### 获取 RSA 配置

```bash
curl -s http://localhost:10444/config
```

```json
{
  "rsa_fingerprint": 123456789,
  "rsa_public_pem": "-----BEGIN PUBLIC KEY-----\n..."
}
```

### Go MTProto 完整示例

> 开发环境 DH 演示需使用与服务端相同的 RSA 私钥 PEM（`MTPROTO_RSA_KEY`）

```bash
cd neomsg/backend
go run ./cmd/mtproto-client-demo/ \
  -api http://localhost:8090 \
  -mtproto localhost:10443 \
  -rsa-pem /path/to/rsa.pem \
  -device demo-mtproto-1 \
  -chat 1
```

### Python 骨架

```bash
python neomsg/examples/mtproto-client-python/mtproto_client.py
```

完整加密握手请参考 `pkg/mtprotoclient` Go 实现。

---

## 4. WirePacket 消息结构

```protobuf
message Message {
  int64 id = 1;
  int64 chat_id = 2;
  int64 from_id = 3;
  int64 to_id = 4;
  string content = 5;
  int32 msg_type = 6;   // 0=text, 1=image, 2=voice, 3=file
  bytes media_key = 7;
  int64 seq_id = 8;
  int64 timestamp = 9;
  bool is_secret = 10;
}
```

同步：

```protobuf
message SyncRequest {
  int64 user_id = 1;
  int64 last_seq = 2;
  int64 chat_id = 3;  // 0 = 全量
}
```

---

## 5. 部署验证

```bash
cd neomsg/deploy
docker compose -f docker-compose.yaml -f docker-compose.mtproto.yaml up -d --build

# 健康检查
curl http://localhost:8080/health
curl http://localhost:10444/health
curl http://localhost:10444/config
```

---

## 6. 参考代码路径

| 组件 | 路径 |
|------|------|
| JWT 签发/校验 | `backend/internal/auth/jwt.go` |
| Message Engine | `backend/internal/message/engine.go` |
| MTProto Bridge | `backend/internal/mtproto/bridge/` |
| Go MTProto 客户端 | `backend/pkg/mtprotoclient/` |
| Go Wire 客户端 | `backend/pkg/wireclient/` |
| Wire 协议定义 | `proto/neomsg/v1/wire.proto` |
