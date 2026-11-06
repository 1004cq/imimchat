package auth

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestVerifierRejectsRevocationBanAndExpiry(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	path := filepath.Join(t.TempDir(), "auth.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE "User"("id" TEXT PRIMARY KEY,"isBanned" BOOLEAN NOT NULL DEFAULT false);CREATE TABLE "UserSession"("token" TEXT PRIMARY KEY,"userId" TEXT,"expiresAt" DATETIME)`)
	exec(`INSERT INTO "User"("id") VALUES('u')`)
	v, err := NewVerifier(path)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	insert := func(exp time.Time) { exec(`INSERT OR REPLACE INTO "UserSession" VALUES('test-session','u',?)`, exp) }
	insert(time.Now().Add(time.Hour))
	if id, err := v.Verify("test-session"); err != nil || id != "u" {
		t.Fatal("valid session rejected", err)
	}
	exec(`DELETE FROM "UserSession"`)
	if _, err := v.Verify("test-session"); err == nil {
		t.Fatal("cached revoked session accepted")
	}
	insert(time.Now().Add(time.Hour))
	exec(`UPDATE "User" SET "isBanned"=true`)
	if _, err := v.Verify("test-session"); err == nil {
		t.Fatal("banned session accepted")
	}
	exec(`UPDATE "User" SET "isBanned"=false`)
	insert(time.Now().Add(-time.Second))
	if _, err := v.Verify("test-session"); err == nil {
		t.Fatal("expired session accepted")
	}
	t.Setenv("DATABASE_URL", "invalid")
	if _, err := NewVerifier(path); err == nil {
		t.Fatal("invalid PostgreSQL must not fall back to SQLite")
	}
}
