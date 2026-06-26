package auth_test

import (
	"testing"
	"time"

	"github.com/neomsg/neomsg/backend/internal/auth"
)

func TestJWTIssueAndValidate(t *testing.T) {
	svc := auth.NewService(nil, nil, "test-secret-key-32bytes-long!!")
	token, err := svc.IssueAccessToken(42, "iphone-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	uid, did, err := svc.ValidateAccessToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if uid != 42 || did != "iphone-1" {
		t.Fatalf("got uid=%d did=%s", uid, did)
	}
}

func TestValidateBindSession(t *testing.T) {
	svc := auth.NewService(nil, nil, "test-secret-key-32bytes-long!!")
	token, _ := svc.IssueAccessToken(7, "dev-1", time.Hour)
	if err := svc.ValidateBindSession(token, 7, "dev-1"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ValidateBindSession(token, 8, "dev-1"); err == nil {
		t.Fatal("expected user mismatch")
	}
}
