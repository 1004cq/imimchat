-- NeoMsg 核心数据库 Schema
-- PostgreSQL 16+

-- ============ 用户 ============

CREATE TABLE users (
    id              BIGSERIAL PRIMARY KEY,
    username        VARCHAR(32) UNIQUE NOT NULL,
    phone           VARCHAR(20) UNIQUE,
    email           VARCHAR(255) UNIQUE,
    password_hash   VARCHAR(255) NOT NULL,
    nickname        VARCHAR(64) NOT NULL DEFAULT '',
    avatar_url      TEXT,
    bio             VARCHAR(500) DEFAULT '',
    is_bot          BOOLEAN NOT NULL DEFAULT FALSE,
    is_banned       BOOLEAN NOT NULL DEFAULT FALSE,
    two_factor_secret VARCHAR(64),
    two_factor_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    pts             BIGINT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_users_phone ON users(phone);
CREATE INDEX idx_users_username ON users(username);

-- ============ 设备 ============

CREATE TABLE devices (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id       VARCHAR(64) NOT NULL,
    platform        VARCHAR(16) NOT NULL,  -- ios | android | web
    app_version     VARCHAR(32),
    model           VARCHAR(64),
    push_token      TEXT,
    push_type       VARCHAR(16),  -- apns | fcm
    last_active_at  TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, device_id)
);

CREATE INDEX idx_devices_user ON devices(user_id);

-- ============ 登录会话 ============

CREATE TABLE auth_sessions (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id       VARCHAR(64) NOT NULL,
    token_hash      VARCHAR(64) NOT NULL UNIQUE,
    expires_at      TIMESTAMPTZ NOT NULL,
    ip_address      INET,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_auth_sessions_user ON auth_sessions(user_id);
CREATE INDEX idx_auth_sessions_token ON auth_sessions(token_hash);

-- ============ 会话 (Dialog) ============

CREATE TYPE dialog_type AS ENUM ('private', 'group', 'supergroup', 'channel', 'secret');

CREATE TABLE dialogs (
    id              BIGSERIAL PRIMARY KEY,
    type            dialog_type NOT NULL,
    title           VARCHAR(255),
    avatar_url      TEXT,
    creator_id      BIGINT REFERENCES users(id),
    is_secret       BOOLEAN NOT NULL DEFAULT FALSE,
    last_msg_id     BIGINT DEFAULT 0,
    last_msg_preview TEXT,
    last_msg_at     TIMESTAMPTZ,
    member_count    INT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE dialog_members (
    dialog_id       BIGINT NOT NULL REFERENCES dialogs(id) ON DELETE CASCADE,
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role            VARCHAR(16) NOT NULL DEFAULT 'member',  -- owner | admin | member
    last_read_id    BIGINT NOT NULL DEFAULT 0,
    is_pinned       BOOLEAN NOT NULL DEFAULT FALSE,
    is_muted        BOOLEAN NOT NULL DEFAULT FALSE,
    joined_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (dialog_id, user_id)
);

CREATE INDEX idx_dialog_members_user ON dialog_members(user_id);

-- ============ 消息（分区表，按月） ============

CREATE TABLE messages (
    id              BIGSERIAL,
    dialog_id       BIGINT NOT NULL,
    sender_id       BIGINT NOT NULL REFERENCES users(id),
    msg_type        SMALLINT NOT NULL DEFAULT 1,
    content         BYTEA,           -- 文本 UTF-8 或 E2EE 密文
    content_text    TEXT,            -- 明文索引用（非密聊）
    client_msg_id   VARCHAR(64),
    reply_to_id     BIGINT,
    media_file_id   VARCHAR(64),
    ttl_seconds     INT DEFAULT 0,
    expire_at       TIMESTAMPTZ,
    is_revoked      BOOLEAN NOT NULL DEFAULT FALSE,
    seq             BIGINT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

-- 创建初始分区（按月，部署时由 worker 自动创建）
CREATE TABLE messages_2026_06 PARTITION OF messages
    FOR VALUES FROM ('2026-06-01') TO ('2026-07-01');

CREATE INDEX idx_messages_dialog_seq ON messages(dialog_id, seq DESC);
CREATE INDEX idx_messages_sender ON messages(sender_id);
CREATE INDEX idx_messages_content_text ON messages USING gin(to_tsvector('simple', content_text));

-- ============ 群组 ============

CREATE TABLE groups (
    id              BIGINT PRIMARY KEY REFERENCES dialogs(id) ON DELETE CASCADE,
    username        VARCHAR(32) UNIQUE,
    description     TEXT,
    max_members     INT NOT NULL DEFAULT 200,
    slow_mode_secs  INT NOT NULL DEFAULT 0,
    is_public       BOOLEAN NOT NULL DEFAULT FALSE
);

-- ============ 频道 ============

CREATE TABLE channels (
    id              BIGINT PRIMARY KEY REFERENCES dialogs(id) ON DELETE CASCADE,
    username        VARCHAR(32) UNIQUE NOT NULL,
    description     TEXT,
    subscriber_count INT NOT NULL DEFAULT 0,
    is_public       BOOLEAN NOT NULL DEFAULT TRUE
);

-- ============ 媒体 ============

CREATE TABLE media_objects (
    id              VARCHAR(64) PRIMARY KEY,
    uploader_id     BIGINT NOT NULL REFERENCES users(id),
    mime_type       VARCHAR(128) NOT NULL,
    size_bytes      BIGINT NOT NULL,
    storage_key     TEXT NOT NULL,
    width           INT,
    height          INT,
    duration_secs   INT,
    thumbnail_key   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE upload_sessions (
    id              VARCHAR(64) PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id),
    mime_type       VARCHAR(128),
    total_size      BIGINT NOT NULL,
    part_size       INT NOT NULL DEFAULT 5242880,  -- 5MB
    parts_received  INT NOT NULL DEFAULT 0,
    storage_key     TEXT,
    status          VARCHAR(16) NOT NULL DEFAULT 'pending',
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ============ Bot ============

CREATE TABLE bots (
    id              BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    owner_id        BIGINT NOT NULL REFERENCES users(id),
    token_hash      VARCHAR(64) NOT NULL UNIQUE,
    webhook_url     TEXT,
    webhook_secret  VARCHAR(64),
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ============ E2EE (Secret Chat) ============

CREATE TABLE e2ee_sessions (
    id              BIGSERIAL PRIMARY KEY,
    dialog_id       BIGINT NOT NULL REFERENCES dialogs(id) ON DELETE CASCADE,
    initiator_id    BIGINT NOT NULL REFERENCES users(id),
    responder_id    BIGINT NOT NULL REFERENCES users(id),
    status          VARCHAR(16) NOT NULL DEFAULT 'pending',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(dialog_id)
);

CREATE TABLE prekeys (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key_id          INT NOT NULL,
    public_key      BYTEA NOT NULL,
    is_used         BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, key_id)
);

CREATE INDEX idx_prekeys_user_unused ON prekeys(user_id) WHERE is_used = FALSE;

-- ============ 推送 ============

CREATE TABLE push_tokens (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id       VARCHAR(64) NOT NULL,
    token           TEXT NOT NULL,
    platform        VARCHAR(16) NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, device_id)
);

-- ============ pts 事件日志（同步用） ============

CREATE TABLE pts_log (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    pts             BIGINT NOT NULL,
    event_type      VARCHAR(32) NOT NULL,
    event_data      JSONB NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_pts_log_user_pts ON pts_log(user_id, pts);
