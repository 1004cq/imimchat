package redisstore

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Store struct {
	client *redis.Client
}

func New(addr string) (*Store, error) {
	client := redis.NewClient(&redis.Options{Addr: addr})
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return &Store{client: client}, nil
}

func (s *Store) Close() error { return s.client.Close() }

// 在线状态
func (s *Store) SetOnline(ctx context.Context, userID int64, deviceID string) error {
	key := fmt.Sprintf("online:%d", userID)
	return s.client.SAdd(ctx, key, deviceID).Err()
}

func (s *Store) SetOffline(ctx context.Context, userID int64, deviceID string) error {
	key := fmt.Sprintf("online:%d", userID)
	return s.client.SRem(ctx, key, deviceID).Err()
}

func (s *Store) IsOnline(ctx context.Context, userID int64) (bool, error) {
	key := fmt.Sprintf("online:%d", userID)
	n, err := s.client.SCard(ctx, key).Result()
	return n > 0, err
}

// 连接映射：userId -> gateway node + connection id
func (s *Store) RegisterConnection(ctx context.Context, userID int64, deviceID, nodeID string) error {
	key := fmt.Sprintf("conn:%d:%s", userID, deviceID)
	return s.client.Set(ctx, key, nodeID, 24*time.Hour).Err()
}

func (s *Store) UnregisterConnection(ctx context.Context, userID int64, deviceID string) error {
	key := fmt.Sprintf("conn:%d:%s", userID, deviceID)
	return s.client.Del(ctx, key).Err()
}

// 限流
func (s *Store) CheckRateLimit(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	pipe := s.client.Pipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, window)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, err
	}
	return incr.Val() <= int64(limit), nil
}

// 正在输入
func (s *Store) SetTyping(ctx context.Context, dialogID, userID int64) error {
	key := fmt.Sprintf("typing:%d:%d", dialogID, userID)
	return s.client.Set(ctx, key, "1", 5*time.Second).Err()
}
