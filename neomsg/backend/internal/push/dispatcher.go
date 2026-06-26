package push

import (
	"context"
	"fmt"

	redisstore "github.com/neomsg/neomsg/backend/internal/store/redis"
)

// Dispatcher 离线推送调度
type Dispatcher struct {
	redis *redisstore.Store
}

func NewDispatcher(redis *redisstore.Store) *Dispatcher {
	return &Dispatcher{redis: redis}
}

type PushPayload struct {
	UserID   int64
	Title    string
	Body     string
	DialogID int64
	Badge    int
}

// Dispatch 检查用户是否在线，离线则发推送
func (d *Dispatcher) Dispatch(ctx context.Context, p *PushPayload) error {
	online, err := d.redis.IsOnline(ctx, p.UserID)
	if err != nil {
		return err
	}
	if online {
		return nil // 在线用户由实时通道送达
	}
	// TODO: 查询 push_tokens，调用 APNs/FCM
	fmt.Printf("[Push] offline push to user=%d: %s\n", p.UserID, p.Body)
	return nil
}
