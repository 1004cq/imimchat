package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	GatewayID             string
	GatewayPort           int
	GatewayHost           string
	CorsOrigins           []string
	RedisAddr             string
	RedisPassword         string
	RedisDB               int
	DBPath                string
	DatabaseURL           string
	LargeGroupThreshold   int
	LargeGroupConcurrency int
	SmallGroupConcurrency int
	FanoutShardSize       int
	MergeWindowMs         int
	MergeMaxBatch         int
	WsBackpressureBytes   int
	DBBatchSize           int
	DBFlushIntervalMs     int
	QueueMaxSize          int
}

func Load() *Config {
	gatewayID := getEnv("GATEWAY_ID", "")
	if gatewayID == "" {
		hostname, err := os.Hostname()
		if err != nil || hostname == "" {
			gatewayID = "gateway-unknown"
		} else {
			gatewayID = hostname
		}
	}

	return &Config{
		GatewayID:     gatewayID,
		GatewayPort:   getEnvInt("GATEWAY_PORT", 8081),
		GatewayHost:   getEnv("GATEWAY_HOST", "0.0.0.0"),
		CorsOrigins:   parseCSV(getEnv("CORS_ORIGINS", "")),
		RedisAddr:     getEnv("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       getEnvInt("REDIS_DB", 0),
		// Empty default is intentional: Validate() refuses implicit SQLite.
		// Do not fall back to DATABASE_URL (Prisma/API Postgres — never Mongo, never a shared .db).
		DBPath:                getEnv("DB_PATH", ""),
		DatabaseURL:           getEnv("DATABASE_URL", ""),
		LargeGroupThreshold:   getEnvInt("LARGE_GROUP_THRESHOLD", 500),
		LargeGroupConcurrency: getEnvInt("LARGE_GROUP_CONCURRENCY", 30),
		SmallGroupConcurrency: getEnvInt("SMALL_GROUP_CONCURRENCY", 200),
		FanoutShardSize:       getEnvInt("FANOUT_SHARD_SIZE", 200),
		MergeWindowMs:         getEnvInt("MERGE_WINDOW_MS", 80),
		MergeMaxBatch:         getEnvInt("MERGE_MAX_BATCH", 20),
		WsBackpressureBytes:   getEnvInt("WS_BACKPRESSURE_BYTES", 65536),
		DBBatchSize:           getEnvInt("DB_BATCH_SIZE", 100),
		DBFlushIntervalMs:     getEnvInt("DB_FLUSH_INTERVAL_MS", 50),
		QueueMaxSize:          getEnvInt("QUEUE_MAX_SIZE", 100000),
	}
}

// Validate refuses an implicit or historically NFS-shared SQLite path.
// Gateway is still sqlite-only for local auth/store; there is no Postgres driver here.
func (c *Config) Validate() error {
	path := strings.TrimSpace(c.DBPath)
	if path == "" {
		return fmt.Errorf("DB_PATH is required: Gateway still uses node-local SQLite for auth/store and will not start on an implicit path. Do not NFS-share a .db. Prisma/API uses PostgreSQL via DATABASE_URL (never Mongo, never file:/app/data/cqim.db)")
	}
	if looksLikeSharedSQLite(path) {
		return fmt.Errorf("DB_PATH=%q is a known shared/NFS SQLite path; dual-node must not share SQLite. Use a node-local file (not cqim.db)", path)
	}
	return nil
}

func looksLikeSharedSQLite(path string) bool {
	p := strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
	p = strings.TrimPrefix(p, "file:")
	if strings.Contains(p, "cqim_shared") {
		return true
	}
	return p == "cqim.db" || strings.HasSuffix(p, "/cqim.db")
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return defaultVal
}

func parseCSV(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
