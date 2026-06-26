package redisstore

import (
	"context"
	"fmt"
	"strings"
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

// DeviceSession 在线设备会话
type DeviceSession struct {
	DeviceID  string
	SessionID int64
	Platform  string
}

func (s *Store) RegisterDeviceSession(ctx context.Context, userID int64, device DeviceSession) error {
	key := fmt.Sprintf("device:%d:%s", userID, device.DeviceID)
	pipe := s.client.Pipeline()
	pipe.HSet(ctx, key, map[string]interface{}{
		"session_id": device.SessionID,
		"platform":   device.Platform,
	})
	pipe.SAdd(ctx, fmt.Sprintf("online:%d", userID), device.DeviceID)
	pipe.SAdd(ctx, fmt.Sprintf("user:devices:%d", userID), device.DeviceID)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *Store) UnregisterDeviceSession(ctx context.Context, userID int64, deviceID string) error {
	key := fmt.Sprintf("device:%d:%s", userID, deviceID)
	pipe := s.client.Pipeline()
	pipe.Del(ctx, key)
	pipe.SRem(ctx, fmt.Sprintf("online:%d", userID), deviceID)
	pipe.SRem(ctx, fmt.Sprintf("user:devices:%d", userID), deviceID)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *Store) ListUserDevices(ctx context.Context, userID int64) ([]DeviceSession, error) {
	deviceIDs, err := s.client.SMembers(ctx, fmt.Sprintf("user:devices:%d", userID)).Result()
	if err != nil {
		return nil, err
	}
	out := make([]DeviceSession, 0, len(deviceIDs))
	for _, deviceID := range deviceIDs {
		key := fmt.Sprintf("device:%d:%s", userID, deviceID)
		vals, err := s.client.HGetAll(ctx, key).Result()
		if err != nil {
			continue
		}
		var sessionID int64
		fmt.Sscanf(vals["session_id"], "%d", &sessionID)
		out = append(out, DeviceSession{
			DeviceID:  deviceID,
			SessionID: sessionID,
			Platform:  vals["platform"],
		})
	}
	return out, nil
}

func (s *Store) SetChatSeq(ctx context.Context, chatID, seq int64) error {
	return s.client.Set(ctx, fmt.Sprintf("chat:seq:%d", chatID), seq, 0).Err()
}

func (s *Store) GetChatSeq(ctx context.Context, chatID int64) (int64, error) {
	return s.client.Get(ctx, fmt.Sprintf("chat:seq:%d", chatID)).Int64()
}

// MTProto AuthKey → 用户会话绑定
func (s *Store) BindMTProtoSession(ctx context.Context, authKeyID, userID int64, deviceID string) error {
	key := fmt.Sprintf("mtproto:auth:%d", authKeyID)
	return s.client.HSet(ctx, key, map[string]interface{}{
		"user_id":   userID,
		"device_id": deviceID,
	}).Err()
}

func (s *Store) GetMTProtoSession(ctx context.Context, authKeyID int64) (userID int64, deviceID string, err error) {
	key := fmt.Sprintf("mtproto:auth:%d", authKeyID)
	vals, err := s.client.HGetAll(ctx, key).Result()
	if err != nil {
		return 0, "", err
	}
	if len(vals) == 0 {
		return 0, "", fmt.Errorf("session not bound")
	}
	fmt.Sscanf(vals["user_id"], "%d", &userID)
	deviceID = vals["device_id"]
	return userID, deviceID, nil
}

func (s *Store) UnbindMTProtoSession(ctx context.Context, authKeyID int64) error {
	return s.client.Del(ctx, fmt.Sprintf("mtproto:auth:%d", authKeyID)).Err()
}

// PushToken 设备推送令牌
type PushToken struct {
	Token    string
	Platform string
	PushType string
	DeviceID string
}

func (s *Store) SavePushToken(ctx context.Context, userID int64, tok PushToken) error {
	key := fmt.Sprintf("push:%d:%s", userID, tok.DeviceID)
	return s.client.HSet(ctx, key, map[string]interface{}{
		"token":     tok.Token,
		"platform":  tok.Platform,
		"push_type": tok.PushType,
	}).Err()
}

func (s *Store) ListPushTokens(ctx context.Context, userID int64) ([]PushToken, error) {
	pattern := fmt.Sprintf("push:%d:*", userID)
	var cursor uint64
	var out []PushToken
	for {
		keys, next, err := s.client.Scan(ctx, cursor, pattern, 50).Result()
		if err != nil {
			return nil, err
		}
		for _, key := range keys {
			vals, err := s.client.HGetAll(ctx, key).Result()
			if err != nil {
				continue
			}
			parts := strings.Split(key, ":")
			deviceID := ""
			if len(parts) >= 3 {
				deviceID = parts[2]
			}
			out = append(out, PushToken{
				Token:    vals["token"],
				Platform: vals["platform"],
				PushType: vals["push_type"],
				DeviceID: deviceID,
			})
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return out, nil
}
