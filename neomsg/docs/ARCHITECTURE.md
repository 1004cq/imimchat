# NeoMsg 技术方案

> 类 Telegram 高性能 IM 系统完整设计文档  
> 版本：v1.0 · 2026-06-26

---

## 1. 整体架构图

```mermaid
flowchart TB
    subgraph Clients["客户端层"]
        iOS["iOS (Swift/SwiftUI)"]
        Android["Android (Kotlin)"]
        Web["Web (未来)"]
        Desktop["Desktop (未来)"]
    end

    subgraph Edge["接入层"]
        LB["L4/L7 Load Balancer"]
        WAF["WAF / Rate Limit"]
        CDN["CDN / Edge Cache"]
    end

    subgraph Gateways["网关层"]
        TCPGW["TCP Gateway\n(Protobuf 长连接)"]
        WSGW["WebSocket Gateway\n(Protobuf over WS)"]
        APIGW["HTTP API Gateway\n(REST + gRPC)"]
    end

    subgraph Services["核心业务服务 (Go)"]
        AuthSvc["Auth Service\n注册/登录/2FA/设备"]
        MsgSvc["Message Service\n路由/存储/扇出"]
        DialogSvc["Dialog Service\n会话/已读/同步"]
        MediaSvc["Media Service\n分片上传/CDN"]
        GroupSvc["Group Service\n群/超级群/频道"]
        BotSvc["Bot Service\nWebhook/Polling"]
        E2EESvc["E2EE Relay\nSecret Chat 密文转发"]
        SearchSvc["Search Service\n全文索引"]
        PushSvc["Push Service\nAPNs/FCM"]
        PresenceSvc["Presence Service\n在线/输入状态"]
    end

    subgraph Comm["实时通信"]
        NATS["NATS JetStream\n消息总线"]
        Redis["Redis Cluster\n在线映射/限流/缓存"]
    end

    subgraph Data["数据层"]
        PG["PostgreSQL\n用户/消息/群/设备"]
        ES["OpenSearch\n消息全文索引"]
        S3["MinIO / S3\n媒体对象存储"]
    end

    subgraph Security["安全与治理"]
        TLS["TLS 1.3 传输加密"]
        E2EE["Secret Chat E2EE\nX3DH + Double Ratchet"]
        Audit["审计日志 / 风控"]
    end

    Clients --> Edge
    Edge --> Gateways
    TCPGW --> MsgSvc
    WSGW --> MsgSvc
    APIGW --> AuthSvc
    APIGW --> MediaSvc
    APIGW --> GroupSvc
    APIGW --> BotSvc
    APIGW --> SearchSvc

    MsgSvc --> NATS
    MsgSvc --> PG
    MsgSvc --> Redis
    DialogSvc --> PG
    MediaSvc --> S3
    MediaSvc --> CDN
    SearchSvc --> ES
    PushSvc --> NATS
    PresenceSvc --> Redis
    E2EESvc --> Redis
    E2EESvc --> NATS

    Gateways --> TLS
    E2EESvc --> E2EE
    Services --> Audit
```

### 消息流转（单聊）

```mermaid
sequenceDiagram
    participant C1 as Client A
    participant GW as Gateway
    participant MS as Message Service
    participant PG as PostgreSQL
    participant NATS as NATS
    participant C2 as Client B

    C1->>GW: Envelope(send_message) [TLS]
    GW->>MS: 鉴权 + 路由
    MS->>PG: 持久化消息
    MS->>NATS: publish dialog.{id}
    NATS->>GW: 扇出到在线设备
    GW->>C2: Envelope(new_message)
    C2->>GW: Envelope(ack_read)
    GW->>MS: 更新已读游标
    MS->>PG: 写入 read_receipt
    MS->>NATS: publish read.{dialog}
    NATS->>GW: 推送已读回执
    GW->>C1: Envelope(read_receipt)
```

---

## 2. 技术选型与理由

| 组件 | 选型 | 理由 |
|------|------|------|
| **后端语言** | **Go 1.22+** | 高并发 goroutine 适合百万连接网关；编译快、部署简单。 |
| **iOS 客户端** | **Swift 5.9 + SwiftUI** | 原生性能、Keychain 安全存储、Background Tasks。 |
| **Android** | Kotlin (Phase 2) | 与 iOS 共享 Protobuf 协议。 |
| **传输协议** | **Protobuf 3** | 比 JSON 体积小 3-5x；跨语言代码生成。 |
| **长连接** | WebSocket + TCP | WS 穿透性好；TCP 移动端省电。 |
| **关系数据库** | **PostgreSQL 16** | ACID、JSONB、分区表支持海量消息。 |
| **缓存** | **Redis 7** | 在线状态、会话映射、限流。 |
| **消息队列** | **NATS JetStream** | 轻量持久化、至少一次投递。 |
| **对象存储** | **MinIO (S3)** | 媒体分片上传、CDN 回源。 |
| **E2EE** | **X3DH + Double Ratchet** | Signal Protocol 同款；服务端只转发密文。 |

---

## 3. 项目目录结构

见仓库根目录 `neomsg/backend/` 与 `neomsg/clients/ios/`。

---

## 4. 数据库表结构

见 `backend/migrations/001_init.sql`。

---

## 5. 通信协议

见 `proto/neomsg/v1/envelope.proto` 与 `messages.proto`。

---

## 6. 核心代码框架

见 `backend/internal/` 与 `clients/ios/NeoMsg/NeoMsg/Core/`。

---

## 7. 部署

```bash
cd neomsg/deploy && docker compose up -d
```

---

## 8. 迭代路线图

| 阶段 | 目标 | 周期 |
|------|------|------|
| Phase 1 MVP | 单聊/群聊/媒体/推送/iOS Alpha | ~10 周 |
| Phase 2 TG Core | 频道/Bot/搜索/Android/超级群 | ~12 周 |
| Phase 3 Advanced | E2EE/音视频/全球部署/桌面 | ~16 周 |
