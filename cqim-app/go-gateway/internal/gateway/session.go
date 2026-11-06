package gateway

import (
	"context"
	"time"

	"github.com/gorilla/websocket"
)

type sessionVerifier interface {
	VerifyContext(context.Context, string) (string, error)
}

func (c *Client) closed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}
func (c *Client) enqueue(data []byte) bool {
	if c.closed() {
		return false
	}
	// send is never closed: disconnect can race concurrent fan-out safely.
	select {
	case <-c.done:
		return false
	case c.send <- data:
		return true
	default:
		return false
	}
}
func (c *Client) stop(code int, reason string) {
	c.stopOnce.Do(func() {
		close(c.done)
		// WriteControl and Close may run concurrently with the single write pump.
		_ = c.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(100*time.Millisecond))
		_ = c.conn.Close()
	})
}

// Caller holds opMu. Checks every inbound command, upgrade and heartbeat against
// the authoritative database; missed Pub/Sub cannot keep a revoked session usable.
func (gw *Gateway) authorizeClient(c *Client) bool {
	gw.mu.RLock()
	current := gw.clients[c.userID] == c
	gw.mu.RUnlock()
	if !current || c.closed() {
		return false
	}
	ctx, cancel := context.WithTimeout(gw.ctx, 2*time.Second)
	defer cancel()
	id, err := gw.verifier.VerifyContext(ctx, c.token)
	if err != nil || id != c.userID {
		c.stop(websocket.ClosePolicyViolation, "session revoked or unavailable")
		gw.unregister(c)
		return false
	}
	return !c.closed()
}

func (gw *Gateway) refreshClient(c *Client) bool {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if !gw.authorizeClient(c) {
		return false
	}
	gw.refreshPresence(c)
	return true
}
func (gw *Gateway) refreshPresence(c *Client) {
	// Serialize the final presence write with unregister/register. An old refresh
	// cannot re-create a key after revocation or delete a replacement connection.
	gw.mu.Lock()
	defer gw.mu.Unlock()
	if gw.clients[c.userID] != c || c.closed() {
		return
	}
	gw.renewOnline(c.userID)
}

func (gw *Gateway) revalidateUser(userID string) {
	gw.mu.RLock()
	c := gw.clients[userID]
	gw.mu.RUnlock()
	if c == nil {
		return
	}
	c.opMu.Lock()
	defer c.opMu.Unlock()
	// A delayed revocation event must not kick a newly authenticated session.
	gw.authorizeClient(c)
}

func (gw *Gateway) activatePresence(c *Client) {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	if gw.clients[c.userID] != c || c.closed() {
		return
	}
	gw.setOnline(c.userID)
}
