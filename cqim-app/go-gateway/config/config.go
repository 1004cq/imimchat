package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	GatewayID   string
	GatewayPort int
	GatewayHost string
	CorsOrigins []string
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	DBPath        string
	DatabaseURL   string
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
		GatewayID:             gatewayID,
		GatewayPort:           getEnvInt("GATEWAY_PORT", 8081),
		GatewayHost:           getEnv("GATEWAY_HOST", "0.0.0.0"),
		CorsOrigins:           parseCSV(getEnv("CORS_ORIGINS", "")),
		RedisAddr:             getEnv("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword:         getEnv("REDIS_PASSWORD", ""),
		RedisDB:               getEnvInt("REDIS_DB", 0),
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
