package gateway

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cqim/go-gateway/internal/auth"
	"github.com/cqim/go-gateway/internal/store"
	"github.com/redis/go-redis/v9"
)

func redisClientOrSkip(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis unavailable at %s: %v", addr, err)
	}
	return rdb
}

func newTestGateway(t *testing.T, rdb *redis.Client, nodeID string) *Gateway {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "gw.db")
	// Touch a writable sqlite file for store; auth opens read-only so reuse same path after create.
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	v, err := auth.NewVerifier(dbPath)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return New(v, s, rdb, ctx, nodeID, nil)
}

func TestOnlineRecordRoundTripRedis(t *testing.T) {
	rdb := redisClientOrSkip(t)
	gw := newTestGateway(t, rdb, "test-gw-a")
	userID := "user-route-1"
	t.Cleanup(func() {
		_ = rdb.Del(context.Background(), userOnlineKey(userID), gwUsersKey(gw.id)).Err()
	})

	gw.setOnline(userID, "conn-xyz")
	rec, ok := gw.LookupOnline(userID)
	if !ok || rec.NodeID != "test-gw-a" || rec.ConnID != "conn-xyz" {
		t.Fatalf("lookup after set: %+v ok=%v", rec, ok)
	}

	ttl, err := rdb.TTL(context.Background(), userOnlineKey(userID)).Result()
	if err != nil || ttl <= 0 || ttl > onlineTTL {
		t.Fatalf("ttl=%v err=%v", ttl, err)
	}

	gw.clearOnline(userID, "conn-xyz")
	if _, ok := gw.LookupOnline(userID); ok {
		t.Fatal("expected route cleared")
	}
}

func TestClearOnlineDoesNotDeleteOtherConn(t *testing.T) {
	rdb := redisClientOrSkip(t)
	gw := newTestGateway(t, rdb, "test-gw-b")
	userID := "user-route-2"
	t.Cleanup(func() {
		_ = rdb.Del(context.Background(), userOnlineKey(userID), gwUsersKey(gw.id)).Err()
	})

	gw.setOnline(userID, "conn-new")
	gw.clearOnline(userID, "conn-old")
	rec, ok := gw.LookupOnline(userID)
	if !ok || rec.ConnID != "conn-new" {
		t.Fatalf("should keep newer conn, got %+v ok=%v", rec, ok)
	}
}

func TestDeliverToUserCrossNodePubSub(t *testing.T) {
	rdb := redisClientOrSkip(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	gw1 := newTestGateway(t, rdb, "test-gw-src")
	gw2 := newTestGateway(t, rdb, "test-gw-dst")
	gw2.StartPushSubscriber(ctx)
	// Wait until Redis subscription is active (Channel() is async).
	deadline := time.Now().Add(2 * time.Second)
	for {
		n, err := rdb.PubSubNumSub(ctx, nodePushChannel(gw2.id)).Result()
		if err == nil && n[nodePushChannel(gw2.id)] > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for Pub/Sub subscription")
		}
		time.Sleep(20 * time.Millisecond)
	}

	userID := "user-cross-1"
	msgID := "m-cross-" + time.Now().Format("150405.000")
	t.Cleanup(func() {
		_ = rdb.Del(
			context.Background(),
			userOnlineKey(userID),
			gwUsersKey(gw1.id),
			gwUsersKey(gw2.id),
			recentPushKey(userID, msgID),
			recentPushKey(userID, "rx:"+msgID),
		).Err()
	})
	// Clear any leftover dedupe keys from prior runs with the same fixture ids.
	_ = rdb.Del(context.Background(), recentPushKey(userID, msgID), recentPushKey(userID, "rx:"+msgID)).Err()

	// Simulate user online on gw2 with a local client buffer.
	client := &Client{
		userID:  userID,
		connID:  "c-dst",
		send:    make(chan []byte, 4),
		gateway: gw2,
	}
	gw2.mu.Lock()
	gw2.clients[userID] = client
	gw2.mu.Unlock()
	gw2.setOnline(userID, client.connID)

	payload := []byte(`{"type":"group_message","msgId":"` + msgID + `","content":"hi"}`)
	if !gw1.DeliverToUser(userID, payload, msgID) {
		t.Fatal("DeliverToUser should succeed via Pub/Sub")
	}

	select {
	case got := <-client.send:
		if string(got) != string(payload) {
			t.Fatalf("payload mismatch: %s", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for cross-node delivery")
	}
}

func TestDeliverToUserOfflineReturnsFalse(t *testing.T) {
	rdb := redisClientOrSkip(t)
	gw := newTestGateway(t, rdb, "test-gw-off")
	userID := "user-offline-1"
	_ = rdb.Del(context.Background(), userOnlineKey(userID)).Err()
	if gw.DeliverToUser(userID, []byte(`{"msgId":"x"}`), "x") {
		t.Fatal("expected false when offline")
	}
}
