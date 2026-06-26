package config

import "os"

type Config struct {
	PostgresDSN string
	RedisAddr   string
	NATSUrl     string
	S3Endpoint  string
	S3AccessKey string
	S3SecretKey string
	S3Bucket    string
	JWTSecret   string
	WSAddr      string
	TCPAddr     string
	APIAddr     string
}

func Load() *Config {
	return &Config{
		PostgresDSN: env("POSTGRES_DSN", "postgres://neomsg:neomsg@localhost:5432/neomsg?sslmode=disable"),
		RedisAddr:   env("REDIS_ADDR", "localhost:6379"),
		NATSUrl:     env("NATS_URL", "nats://localhost:4222"),
		S3Endpoint:  env("S3_ENDPOINT", "localhost:9000"),
		S3AccessKey: env("S3_ACCESS_KEY", "minioadmin"),
		S3SecretKey: env("S3_SECRET_KEY", "minioadmin"),
		S3Bucket:    env("S3_BUCKET", "neomsg-media"),
		JWTSecret:   env("JWT_SECRET", "change-me-in-production"),
		WSAddr:      env("WS_ADDR", ":8080"),
		TCPAddr:     env("TCP_ADDR", ":5222"),
		APIAddr:     env("API_ADDR", ":8090"),
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
