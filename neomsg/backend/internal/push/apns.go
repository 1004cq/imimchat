package push

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	redisstore "github.com/neomsg/neomsg/backend/internal/store/redis"
)

// Dispatcher 离线推送调度（APNs HTTP/2）
type Dispatcher struct {
	redis    *redisstore.Store
	apns     *APNsClient
	apnsOnce sync.Once
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
	Sound    string // urgent_message.caf | default
}

func (d *Dispatcher) Dispatch(ctx context.Context, p *PushPayload) error {
	online, err := d.redis.IsOnline(ctx, p.UserID)
	if err != nil {
		return err
	}
	if online {
		return nil
	}

	tokens, err := d.redis.ListPushTokens(ctx, p.UserID)
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		fmt.Printf("[Push] no tokens for user=%d\n", p.UserID)
		return nil
	}

	client := d.apnsClient()
	if client == nil {
		fmt.Printf("[Push] APNs not configured, user=%d body=%s\n", p.UserID, p.Body)
		return nil
	}

	sound := p.Sound
	if sound == "" {
		sound = "urgent_message.caf"
	}

	for _, tok := range tokens {
		if tok.Platform != "ios" && tok.PushType != "apns" {
			continue
		}
		payload := apnsPayload{
			Aps: apnsAps{
				Alert: apnsAlert{Title: p.Title, Body: p.Body},
				Sound: sound,
				Badge: p.Badge,
				ContentAvailable: 1,
			},
			DialogID: p.DialogID,
			Preview:  p.Body,
		}
		if err := client.Send(ctx, tok.Token, payload); err != nil {
			fmt.Printf("[Push] APNs send failed user=%d: %v\n", p.UserID, err)
		}
	}
	return nil
}

func (d *Dispatcher) apnsClient() *APNsClient {
	d.apnsOnce.Do(func() {
		d.apns = NewAPNsClientFromEnv()
	})
	return d.apns
}

// --- APNs HTTP/2 client (Token Auth .p8) ---

type APNsClient struct {
	host      string
	teamID    string
	keyID     string
	bundleID  string
	privateKey *ecdsa.PrivateKey
	http      *http.Client
}

type apnsPayload struct {
	Aps      apnsAps `json:"aps"`
	DialogID int64   `json:"dialog_id"`
	Preview  string  `json:"preview"`
}

type apnsAps struct {
	Alert            apnsAlert `json:"alert"`
	Sound            string    `json:"sound"`
	Badge            int       `json:"badge,omitempty"`
	ContentAvailable int       `json:"content-available,omitempty"`
}

type apnsAlert struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

func NewAPNsClientFromEnv() *APNsClient {
	keyPath := os.Getenv("APNS_AUTH_KEY_PATH")
	if keyPath == "" {
		return nil
	}
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		fmt.Printf("[APNs] read key: %v\n", err)
		return nil
	}
	block, _ := pem.Decode(keyData)
	if block == nil {
		return nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil
	}
	ecKey, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil
	}
	host := "https://api.push.apple.com"
	if os.Getenv("APNS_SANDBOX") == "1" {
		host = "https://api.sandbox.push.apple.com"
	}
	return &APNsClient{
		host:       host,
		teamID:     os.Getenv("APNS_TEAM_ID"),
		keyID:      os.Getenv("APNS_KEY_ID"),
		bundleID:   os.Getenv("APNS_BUNDLE_ID"),
		privateKey: ecKey,
		http:       &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *APNsClient) Send(ctx context.Context, deviceToken string, payload apnsPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/3/device/%s", c.host, deviceToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("apns-topic", c.bundleID)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-priority", "10")
	// TODO: JWT bearer token from .p8 (production: use github.com/sideshow/apns2)
	jwt, err := buildAPNsJWT(c.teamID, c.keyID, c.privateKey)
	if err != nil {
		return err
	}
	req.Header.Set("authorization", "bearer "+jwt)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("apns status %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func buildAPNsJWT(teamID, keyID string, key *ecdsa.PrivateKey) (string, error) {
	_ = teamID
	_ = keyID
	_ = key
	// Placeholder: integrate apns2 or golang-jwt in production
	return "placeholder-jwt", nil
}
