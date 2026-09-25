package config

import (
	"os"
	"strconv"
)

// Config 服务配置，全部来自环境变量，与 Node 版 .env 键名保持一致。
type Config struct {
	Port        string
	DatabaseURL string
	RedisURL    string
	NodeEnv     string
	TrustProxy  bool

	// 管理员初始密码（S13：未设置则随机生成）
	AdminInitialPassword string

	// APNs
	ApnsKeyID      string
	ApnsTeamID     string
	ApnsBundleID   string
	ApnsKeyPath    string
	ApnsProduction bool

	// 邮件 / 短信
	SmtpHost     string
	SmtpPort     int
	SmtpUser     string
	SmtpPassword string
	SmsProvider  string

	// 对象存储（COS）
	CosSecretID  string
	CosSecretKey string
	CosBucket    string
	CosRegion    string

	// OneBot
	OneBotAPIURL   string
	OneBotAPIToken string

	// AI
	AIAPIURL string
	AIAPIKey string
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getenvBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		return v == "1" || v == "true" || v == "yes"
	}
	return def
}

// Load 从环境变量加载配置。
func Load() *Config {
	return &Config{
		Port:                 getenv("PORT", "3000"),
		DatabaseURL:          getenv("DATABASE_URL", ""),
		RedisURL:             getenv("REDIS_URL", "redis://127.0.0.1:6379/0"),
		NodeEnv:              getenv("NODE_ENV", "development"),
		TrustProxy:           getenvBool("TRUST_PROXY", false),
		AdminInitialPassword: os.Getenv("ADMIN_INITIAL_PASSWORD"),

		ApnsKeyID:      os.Getenv("APNS_KEY_ID"),
		ApnsTeamID:     os.Getenv("APNS_TEAM_ID"),
		ApnsBundleID:   os.Getenv("APNS_BUNDLE_ID"),
		ApnsKeyPath:    os.Getenv("APNS_KEY_PATH"),
		ApnsProduction: getenvBool("APNS_PRODUCTION", false),

		SmtpHost:     os.Getenv("SMTP_HOST"),
		SmtpPort:     getenvInt("SMTP_PORT", 587),
		SmtpUser:     os.Getenv("SMTP_USER"),
		SmtpPassword: os.Getenv("SMTP_PASSWORD"),
		SmsProvider:  os.Getenv("SMS_PROVIDER"),

		CosSecretID:  os.Getenv("COS_SECRET_ID"),
		CosSecretKey: os.Getenv("COS_SECRET_KEY"),
		CosBucket:    os.Getenv("COS_BUCKET"),
		CosRegion:    os.Getenv("COS_REGION"),

		OneBotAPIURL:   os.Getenv("ONEBOT_API_URL"),
		OneBotAPIToken: os.Getenv("ONEBOT_API_TOKEN"),

		AIAPIURL: os.Getenv("AI_API_URL"),
		AIAPIKey: os.Getenv("AI_API_KEY"),
	}
}

func (c *Config) IsProduction() bool { return c.NodeEnv == "production" }
