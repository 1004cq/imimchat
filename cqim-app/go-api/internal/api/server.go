package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const defaultListenAddr = "127.0.0.1:8089"

type Server struct {
	listenAddr string
	db         *pgxpool.Pool
	redis      redis.UniversalClient
}

type user struct {
	ID            string     `json:"id"`
	DialogID      *string    `json:"dialogId"`
	Username      string     `json:"username"`
	Nickname      *string    `json:"nickname"`
	Phone         *string    `json:"phone"`
	Email         *string    `json:"email"`
	Avatar        *string    `json:"avatar"`
	Bio           *string    `json:"bio"`
	Gender        string     `json:"gender"`
	Region        string     `json:"region"`
	Birthday      string     `json:"birthday"`
	IsBot         bool       `json:"isBot"`
	PhoneVerified bool       `json:"phoneVerified"`
	EmailVerified bool       `json:"emailVerified"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     int64      `json:"updatedAt"`
}

func NewServerFromEnv() (*Server, func(), error) {
	server := &Server{listenAddr: envOrDefault("GO_API_LISTEN_ADDR", defaultListenAddr)}
	if databaseURL := os.Getenv("DATABASE_URL"); databaseURL != "" {
		pool, err := pgxpool.New(context.Background(), normalizeDatabaseURL(databaseURL))
		if err != nil {
			return nil, nil, fmt.Errorf("create PostgreSQL pool: %w", err)
		}
		server.db = pool
	}

	if client, err := redisFromEnv(); err != nil {
		return nil, nil, err
	} else {
		server.redis = client
	}

	return server, func() {
		if server.db != nil {
			server.db.Close()
		}
		if server.redis != nil {
			_ = server.redis.Close()
		}
	}, nil
}

func (s *Server) ListenAddr() string { return s.listenAddr }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/me", s.requireUser(s.me))
	return mux
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	postgresOK := s.db != nil && s.db.Ping(ctx) == nil
	redisOK := s.redis != nil && s.redis.Ping(ctx).Err() == nil
	status := http.StatusOK
	if !postgresOK || !redisOK {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]bool{"postgres": postgresOK, "redis": redisOK})
}

func (s *Server) requireUser(next func(http.ResponseWriter, *http.Request, user)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r.Header.Get("Authorization"))
		if token == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录"})
			return
		}

		currentUser, err := s.findSessionUser(r.Context(), token)
		if errors.Is(err, errInvalidSession) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "登录已过期"})
			return
		}
		if errors.Is(err, errBanned) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "账号已被封禁"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "认证服务不可用"})
			return
		}
		next(w, r, currentUser)
	}
}

func (s *Server) me(w http.ResponseWriter, _ *http.Request, currentUser user) {
	writeJSON(w, http.StatusOK, map[string]user{"user": currentUser})
}

var (
	errInvalidSession = errors.New("invalid session")
	errBanned         = errors.New("banned user")
)

func (s *Server) findSessionUser(ctx context.Context, token string) (user, error) {
	if s.db == nil {
		return user{}, errors.New("DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	const query = `
		SELECT u."id", u."dialogId", u."username", u."nickname", u."phone", u."email",
		       u."avatar", u."bio", COALESCE(u."gender", ''), COALESCE(u."region", ''),
		       COALESCE(u."birthday", ''), u."isBot", u."phoneVerified", u."emailVerified",
		       u."createdAt", u."updatedAt", u."isBanned"
		FROM "UserSession" AS s
		JOIN "User" AS u ON u."id" = s."userId"
		WHERE s."token" = $1 AND s."expiresAt" > NOW()
		LIMIT 1`

	var currentUser user
	var updatedAt time.Time
	var banned bool
	err := s.db.QueryRow(ctx, query, token).Scan(
		&currentUser.ID, &currentUser.DialogID, &currentUser.Username, &currentUser.Nickname,
		&currentUser.Phone, &currentUser.Email, &currentUser.Avatar, &currentUser.Bio,
		&currentUser.Gender, &currentUser.Region, &currentUser.Birthday, &currentUser.IsBot,
		&currentUser.PhoneVerified, &currentUser.EmailVerified, &currentUser.CreatedAt,
		&updatedAt, &banned,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return user{}, errInvalidSession
		}
		return user{}, fmt.Errorf("query user session: %w", err)
	}
	if banned {
		return user{}, errBanned
	}
	currentUser.UpdatedAt = updatedAt.UnixMilli()
	return currentUser, nil
}

func bearerToken(authorization string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(authorization, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(authorization, prefix))
}

func redisFromEnv() (redis.UniversalClient, error) {
	if rawURL := os.Getenv("REDIS_URL"); rawURL != "" {
		opts, err := redis.ParseURL(rawURL)
		if err != nil {
			return nil, fmt.Errorf("parse REDIS_URL: %w", err)
		}
		return redis.NewClient(opts), nil
	}
	if address := os.Getenv("REDIS_ADDR"); address != "" {
		return redis.NewClient(&redis.Options{Addr: address}), nil
	}
	return nil, nil
}

// normalizeDatabaseURL accepts the same Prisma URL that Node uses. Prisma's
// schema query parameter is not a PostgreSQL connection parameter and must not
// be sent by pgx during startup.
func normalizeDatabaseURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := parsed.Query()
	query.Del("schema")
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
