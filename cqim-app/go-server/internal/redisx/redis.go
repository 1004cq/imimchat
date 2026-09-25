package redisx

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Client Redis 客户端封装。Redis 不可用时调用方应降级处理（fail-open 读 / fail-closed 写由业务决定）。
type Client struct {
	RDB *redis.Client
}

// New 根据 REDIS_URL 创建客户端。URL 为空时返回可用但会报错的客户端由调用方降级。
func New(redisURL string) *Client {
	if redisURL == "" {
		redisURL = "redis://127.0.0.1:6379/0"
	}
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		opt = &redis.Options{Addr: "127.0.0.1:6379"}
	}
	return &Client{RDB: redis.NewClient(opt)}
}

func (c *Client) Close() error { return c.RDB.Close() }

func (c *Client) Ping(ctx context.Context) error {
	return c.RDB.Ping(ctx).Err()
}

func isRedisErr(err error) bool { return err != nil && err != redis.Nil }

// GetString 读取字符串，key 不存在返回 ("", false, nil)。
func (c *Client) GetString(ctx context.Context, key string) (string, bool, error) {
	v, err := c.RDB.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (c *Client) SetEX(ctx context.Context, key, value string, ttl time.Duration) error {
	return c.RDB.Set(ctx, key, value, ttl).Err()
}

func (c *Client) Del(ctx context.Context, keys ...string) error {
	return c.RDB.Del(ctx, keys...).Err()
}

func (c *Client) Incr(ctx context.Context, key string) (int64, error) {
	return c.RDB.Incr(ctx, key).Result()
}

func (c *Client) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return c.RDB.Expire(ctx, key, ttl).Err()
}

func (c *Client) Publish(ctx context.Context, channel, message string) error {
	return c.RDB.Publish(ctx, channel, message).Err()
}

func (c *Client) SAdd(ctx context.Context, key string, members ...any) error {
	return c.RDB.SAdd(ctx, key, members...).Err()
}

func (c *Client) SRem(ctx context.Context, key string, members ...any) error {
	return c.RDB.SRem(ctx, key, members...).Err()
}

func (c *Client) SMembers(ctx context.Context, key string) ([]string, error) {
	return c.RDB.SMembers(ctx, key).Result()
}

func (c *Client) HSet(ctx context.Context, key, field, value string) error {
	return c.RDB.HSet(ctx, key, field, value).Err()
}

func (c *Client) HDel(ctx context.Context, key string, fields ...string) error {
	return c.RDB.HDel(ctx, key, fields...).Err()
}

func (c *Client) ZAdd(ctx context.Context, key string, members ...redis.Z) error {
	return c.RDB.ZAdd(ctx, key, members...).Err()
}

// Eval 执行 Lua 脚本（用于需要原子性的复合操作，如验证码消费、分布式限流）。
func (c *Client) Eval(ctx context.Context, script string, keys []string, args ...any) (any, error) {
	return c.RDB.Eval(ctx, script, keys, args...).Result()
}
