package auth

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/neomsg/neomsg/backend/internal/store/postgres"
	redisstore "github.com/neomsg/neomsg/backend/internal/store/redis"
)

type Service struct {
	pg        *postgres.Store
	redis     *redisstore.Store
	jwtSecret string
}

func NewService(pg *postgres.Store, redis *redisstore.Store, jwtSecret string) *Service {
	return &Service{pg: pg, redis: redis, jwtSecret: jwtSecret}
}

type registerRequest struct {
	Username string `json:"username"`
	Phone    string `json:"phone"`
	Password string `json:"password"`
	Nickname string `json:"nickname"`
}

type loginRequest struct {
	Phone    string `json:"phone"`
	Password string `json:"password"`
	DeviceID string `json:"device_id"`
	Platform string `json:"platform"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	UserID       int64  `json:"user_id"`
}

func (s *Service) HandleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	// TODO: 密码哈希 + 写入 users 表
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "user_id": 1})
}

func (s *Service) HandleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	// TODO: 验证密码 + 签发 JWT
	json.NewEncoder(w).Encode(tokenResponse{
		AccessToken:  "eyJ...",
		RefreshToken: "eyJ...",
		ExpiresIn:    int64(24 * time.Hour.Seconds()),
		UserID:       1,
	})
}

func (s *Service) HandleRefresh(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(tokenResponse{AccessToken: "eyJ...", ExpiresIn: 3600})
}

func (s *Service) HandleListDevices(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]any{"devices": []any{}})
}

func (s *Service) HandleRevokeDevice(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (s *Service) HandleEnable2FA(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]any{"secret": "BASE32SECRET", "qr_url": "otpauth://..."})
}

func (s *Service) HandleVerify2FA(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
