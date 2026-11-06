package gateway

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cqim/go-gateway/internal/auth"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

// Runs real WebSocket transports on two gateway nodes, real PostgreSQL/Redis,
// and a separately built Go admin API. No production service or data is used.
func TestAdminRevocationIntegration(t *testing.T) {
	dsn, addr, binary := os.Getenv("ADMIN_TEST_DATABASE_URL"), os.Getenv("ADMIN_TEST_REDIS_ADDR"), os.Getenv("ADMIN_TEST_API_BINARY")
	if dsn == "" || addr == "" || binary == "" {
		t.Skip("requires disposable DB/Redis and ADMIN_TEST_API_BINARY")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Path != "/imim_admin_test" {
		t.Fatal("refusing non-test database")
	}
	root, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	schema := "gateway_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = root.Exec(`CREATE SCHEMA "` + schema + `"`); err != nil {
		t.Fatal(err)
	}
	defer root.Exec(`DROP SCHEMA "` + schema + `" CASCADE`)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	runSQL := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	files, _ := filepath.Glob("../../../prisma/migrations/*/migration.sql")
	if len(files) == 0 {
		t.Fatal("missing migrations")
	}
	for _, p := range files {
		b, e := os.ReadFile(p)
		if e != nil {
			t.Fatal(e)
		}
		runSQL(string(b))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rc := redis.NewClient(&redis.Options{Addr: addr, DB: 15})
	defer rc.Close()
	uid, adminID, adminToken := uuid.NewString(), uuid.NewString(), uuid.NewString()
	runSQL(`INSERT INTO "User"("id","username","password","updatedAt") VALUES($1,'fixture','unused',NOW())`, uid)
	runSQL(`INSERT INTO "AdminAccount"("id","username","password","updatedAt") VALUES($1,'fixture-admin','unused',NOW())`, adminID)
	runSQL(`INSERT INTO "AdminSession"("id","adminId","token","expiresAt") VALUES($1,$2,$3,NOW()+INTERVAL '1 hour')`, uuid.NewString(), adminID, adminToken)
	session := func() string {
		token := uuid.NewString()
		runSQL(`INSERT INTO "UserSession"("id","userId","token","expiresAt") VALUES($1,$2,$3,NOW()+INTERVAL '1 hour')`, uuid.NewString(), uid, token)
		return token
	}
	// Exercise Prisma schema URL compatibility too.
	vURL := *u
	vq := vURL.Query()
	vq.Del("search_path")
	vq.Set("schema", schema)
	vURL.RawQuery = vq.Encode()
	verifier, err := auth.NewPostgresVerifier(vURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer verifier.Close()
	var nodeCleanups []func()
	defer func() {
		for _, cleanup := range nodeCleanups {
			cleanup()
		}
		cancel()
	}()
	newNode := func(subscribe bool) (*Gateway, string) {
		g := New(verifier, nil, rc, ctx, schema+uuid.NewString(), nil)
		if subscribe {
			if err := g.StartPushSubscriber(ctx); err != nil {
				t.Fatal(err)
			}
		}
		ts := httptest.NewServer(http.HandlerFunc(g.HandleWS))
		nodeCleanups = append(nodeCleanups, func() {
			g.mu.RLock()
			cs := []*Client{}
			for _, c := range g.clients {
				cs = append(cs, c)
			}
			g.mu.RUnlock()
			for _, c := range cs {
				g.unregister(c)
			}
			ts.Close()
			rc.Del(context.Background(), gwAliveKey(g.id), gwUsersKey(g.id))
		})
		return g, "ws" + strings.TrimPrefix(ts.URL, "http")
	}
	g1, url1 := newNode(true)
	g2, url2 := newNode(true)
	dial := func(endpoint, token string) *websocket.Conn {
		t.Helper()
		header := http.Header{"Authorization": []string{"Bearer " + token}}
		c, _, err := websocket.DefaultDialer.Dial(endpoint, header)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		return c
	}
	ping := func(c *websocket.Conn) {
		t.Helper()
		if err := c.WriteJSON(map[string]string{"type": "ping"}); err != nil {
			t.Fatal(err)
		}
		c.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, b, err := c.ReadMessage()
		if err != nil || !bytes.Contains(b, []byte(`"pong"`)) {
			t.Fatalf("ping failed: %v", err)
		}
	}
	closed := func(c *websocket.Conn) {
		t.Helper()
		c.SetReadDeadline(time.Now().Add(3 * time.Second))
		for {
			_, _, err := c.ReadMessage()
			if err != nil {
				if !websocket.IsCloseError(err, websocket.ClosePolicyViolation) {
					t.Fatalf("expected policy close, got %v", err)
				}
				break
			}
		}
	}
	rejected := func(endpoint, token string) {
		t.Helper()
		c, res, e := websocket.DefaultDialer.Dial(endpoint, http.Header{"Authorization": []string{"Bearer " + token}})
		if c != nil {
			c.Close()
		}
		if e == nil || res == nil || res.StatusCode != 401 {
			t.Fatal("revoked token reconnected")
		}
		res.Body.Close()
	}
	waitAbsent := func(g *Gateway) {
		t.Helper()
		limit := time.Now().Add(3 * time.Second)
		for time.Now().Before(limit) {
			g.mu.RLock()
			n := len(g.clients)
			g.mu.RUnlock()
			if n == 0 {
				return
			}
			time.Sleep(time.Millisecond * 10)
		}
		t.Fatal("client remains registered")
	}
	// Start only the API process, with an isolated search_path and Redis database.
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	apiAddr := listener.Addr().String()
	listener.Close()
	cmd := exec.Command(binary)
	cmd.Env = append(os.Environ(), "DATABASE_URL="+u.String(), "REDIS_URL=redis://"+addr+"/15", "GO_API_LISTEN_ADDR="+apiAddr)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	httpClient := &http.Client{Timeout: 5 * time.Second}
	ready := false
	for i := 0; i < 100; i++ {
		r, e := httpClient.Get("http://" + apiAddr + "/api/health")
		if e == nil {
			r.Body.Close()
			if r.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		t.Fatal("test API not healthy")
	}
	ban := func(value bool) {
		t.Helper()
		body := fmt.Sprintf(`{"ban":%t,"reason":"integration"}`, value)
		req, _ := http.NewRequest("POST", "http://"+apiAddr+"/api/admin/users/"+uid+"/ban", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")
		r, e := httpClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			b, _ := io.ReadAll(r.Body)
			t.Fatalf("ban status %d: %s", r.StatusCode, b)
		}
	}
	token1, token2 := session(), session()
	c1, c2 := dial(url1, token1), dial(url2, token2)
	ping(c1)
	ping(c2)
	// A stale node's heartbeat must not steal the newer node's ownership.
	owner := rc.Get(ctx, userOnlineKey(uid)).Val()
	g1.renewAllPresence()
	if rc.Get(ctx, userOnlineKey(uid)).Val() != owner {
		t.Fatal("old heartbeat stole presence")
	}
	g1.mu.RLock()
	old1 := g1.clients[uid]
	g1.mu.RUnlock()
	g2.mu.RLock()
	old2 := g2.clients[uid]
	g2.mu.RUnlock()
	group := schema + "group"
	g1.joinGroupOnline(group, old1)
	g2.joinGroupOnline(group, old2)
	defer rc.Del(ctx, "group:online:"+group, userOnlineKey(uid))
	// Fan-out races with ban: no send-on-closed-channel panic or data race.
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				g1.PushToUser(uid, []byte(`{"type":"fixture"}`))
				time.Sleep(time.Millisecond)
			}
		}
	}()
	var stopOnce sync.Once
	stopFanout := func() { stopOnce.Do(func() { close(stop); wg.Wait() }) }
	defer stopFanout()
	ban(true)
	closed(c1)
	closed(c2)
	stopFanout()
	waitAbsent(g1)
	waitAbsent(g2)
	if rc.Exists(ctx, userOnlineKey(uid)).Val() != 0 || rc.SIsMember(ctx, "group:online:"+group, uid).Val() {
		t.Fatal("revoked presence remains")
	}
	g1.refreshClient(old1)
	g2.refreshClient(old2)
	g1.renewAllPresence()
	g2.renewAllPresence()
	if rc.Exists(ctx, userOnlineKey(uid)).Val() != 0 {
		t.Fatal("revoked heartbeat recreated presence")
	}
	rejected(url1, token1)
	rejected(url2, token2)
	t.Log("admin HTTP ban closed both real sockets; revoked tokens and heartbeats rejected")
	ban(false)
	freshToken := session()
	fresh := dial(url1, freshToken)
	ping(fresh)
	event, _ := json.Marshal(map[string]any{"userId": uid, "payload": map[string]string{"type": "session_revoked"}})
	rc.Publish(ctx, imPushChannel, event)
	g1.revalidateUser(uid)
	ping(fresh)
	rejected(url1, token1)
	t.Log("unban requires new session; delayed revoke does not close a fresh valid session")
	// Replacement cleanup cannot delete the new same-node connection/presence.
	replacement := dial(url1, freshToken)
	ping(replacement)
	fresh.Close()
	ping(replacement)
	if rc.Get(ctx, userOnlineKey(uid)).Val() != g1.id {
		t.Fatal("old cleanup deleted replacement presence")
	}
	// Deliberately omit subscriber to prove database revalidation is authoritative.
	g3, url3 := newNode(false)
	missedToken := session()
	missed := dial(url3, missedToken)
	ping(missed)
	runSQL(`DELETE FROM "UserSession" WHERE "token"=$1`, missedToken)
	// If auth is bypassed this reaches a nil store: the test must instead see close.
	missed.WriteJSON(map[string]any{"type": "group_send", "data": map[string]string{"groupId": "fixture", "content": "ciphertext"}})
	closed(missed)
	waitAbsent(g3)
	idleToken := session()
	idle := dial(url3, idleToken)
	ping(idle)
	runSQL(`DELETE FROM "UserSession" WHERE "token"=$1`, idleToken)
	g3.renewAllPresence()
	closed(idle)
	waitAbsent(g3)
	t.Log("missed Pub/Sub: commands and periodic presence sweep close revoked sessions")
}
