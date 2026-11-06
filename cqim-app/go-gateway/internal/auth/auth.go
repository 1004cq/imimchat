package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/mattn/go-sqlite3"
)

// Verifier checks the authoritative session table, including bans, on every
// operation. A positive TTL cache would allow revoked sessions to reconnect.
type Verifier struct {
	db       *sql.DB
	postgres bool
}

// DATABASE_URL opts into the shared Node/Go API PostgreSQL session store.
// With no DATABASE_URL, legacy SQLite installations retain their existing source.
// An invalid/unavailable PostgreSQL configuration never falls back to SQLite.
func NewVerifier(dbPath string) (*Verifier, error) {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return NewPostgresVerifier(dsn)
	}
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&mode=ro")
	if err != nil {
		return nil, errors.New("open session database failed")
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(3)
	return &Verifier{db: db}, nil
}
func NewPostgresVerifier(dsn string) (*Verifier, error) {
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return nil, errors.New("invalid PostgreSQL session configuration")
	}
	q := u.Query()
	// Prisma's schema option is not a PostgreSQL startup parameter.
	if schema := q.Get("schema"); schema != "" {
		q.Del("schema")
		q.Set("search_path", `"`+strings.ReplaceAll(schema, `"`, `""`)+`"`)
	}
	u.RawQuery = q.Encode()
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		return nil, errors.New("open session database failed")
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(3)
	return &Verifier{db: db, postgres: true}, nil
}
func (v *Verifier) Verify(token string) (string, error) {
	return v.VerifyContext(context.Background(), token)
}
func (v *Verifier) VerifyContext(ctx context.Context, token string) (string, error) {
	if token == "" {
		return "", errors.New("invalid session")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	parameter := "?"
	if v.postgres {
		parameter = "$1"
	}
	var userID string
	var expiresAt time.Time
	err := v.db.QueryRowContext(ctx, `SELECT s."userId",s."expiresAt" FROM "UserSession" s JOIN "User" u ON u."id"=s."userId" WHERE s."token"=`+parameter+` AND u."isBanned"=false LIMIT 1`, token).Scan(&userID, &expiresAt)
	if err != nil || !time.Now().Before(expiresAt) {
		return "", errors.New("session unavailable, revoked or expired")
	}
	return userID, nil
}
func (v *Verifier) Close() error { return v.db.Close() }
