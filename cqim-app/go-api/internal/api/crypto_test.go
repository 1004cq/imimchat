package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testP256SPKI(t *testing.T) string {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(der)
}

func TestResolveSigningPublicKey(t *testing.T) {
	existing := `{"identityKey":"ik","signingPublicKey":"old-sign"}`

	if got := resolveSigningPublicKey("new-sign", "ik", existing); got == nil || *got != "new-sign" {
		t.Fatalf("incoming key should win, got %#v", got)
	}
	if got := resolveSigningPublicKey(nil, "ik", existing); got == nil || *got != "old-sign" {
		t.Fatalf("omitted field should keep stored key, got %#v", got)
	}
	if got := resolveSigningPublicKey("", "ik", existing); got == nil || *got != "old-sign" {
		t.Fatalf("empty incoming should keep stored key, got %#v", got)
	}
	if got := resolveSigningPublicKey(nil, "new-ik", existing); got != nil {
		t.Fatalf("identity rotation should drop stored key, got %#v", got)
	}
}

func TestVerifyHMAC(t *testing.T) {
	secret := "abc"
	message := "hello"
	sig := generateHMAC(message, secret)
	if !verifyHMAC(message, sig, secret) {
		t.Fatal("expected matching HMAC to verify")
	}
	if verifyHMAC(message, "00"+sig[2:], secret) {
		t.Fatal("expected mismatched HMAC to fail")
	}
	if verifyHMAC(message, "zz", secret) {
		t.Fatal("expected invalid hex to fail")
	}
}

func TestMergePreKeysDedupesByKeyID(t *testing.T) {
	oldKey := testP256SPKI(t)
	keepKey := testP256SPKI(t)
	newKey := testP256SPKI(t)
	addedKey := testP256SPKI(t)
	merged := mergePreKeys(
		[]e2eePreKey{{KeyID: 1.0, PublicKey: oldKey}, {KeyID: 2.0, PublicKey: keepKey}},
		[]e2eePreKey{{KeyID: 1.0, PublicKey: newKey}, {KeyID: 3.0, PublicKey: addedKey}},
	)
	if len(merged) != 3 {
		t.Fatalf("got %d keys", len(merged))
	}
	if merged[0].PublicKey != newKey || merged[1].PublicKey != keepKey || merged[2].PublicKey != addedKey {
		t.Fatalf("unexpected merge %#v", merged)
	}
}

func TestP256PreKeyValidationFiltersLegacyRawKey(t *testing.T) {
	valid := testP256SPKI(t)
	legacyRaw := base64.StdEncoding.EncodeToString(make([]byte, 32))
	if !isValidP256SPKIPublicKey(valid) {
		t.Fatal("expected generated P-256 SPKI to be valid")
	}
	if isValidP256SPKIPublicKey(legacyRaw) {
		t.Fatal("expected legacy raw key to be rejected")
	}
	filtered := filterValidP256PreKeys([]e2eePreKey{
		{KeyID: 1.0, PublicKey: legacyRaw},
		{KeyID: 2.0, PublicKey: valid},
	})
	if len(filtered) != 1 || filtered[0].PublicKey != valid {
		t.Fatalf("unexpected filtered keys %#v", filtered)
	}
}

func TestGetBundleNeverPlainText404(t *testing.T) {
	server := &Server{}
	handler := server.Handler()

	cases := []struct {
		path   string
		status int
		err    string
	}{
		{"/api/crypto/get-bundle", http.StatusBadRequest, "缺少 userId"},
		{"/api/crypto/get-bundle?userId=missing-user", http.StatusInternalServerError, "获取 Bundle 失败"},
		{"/api/crypto/unknown", http.StatusNotFound, "未找到该加密接口"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("%s status=%d body=%s", tc.path, rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Fatalf("%s content-type %q", tc.path, ct)
		}
		if strings.Contains(rec.Body.String(), "404 page not found") {
			t.Fatalf("%s returned net/http default 404: %s", tc.path, rec.Body.String())
		}
		var payload map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("%s json: %v body=%s", tc.path, err, rec.Body.String())
		}
		if payload["error"] != tc.err {
			t.Fatalf("%s error=%q", tc.path, payload["error"])
		}
	}
}

func TestRegisterBundleValidation(t *testing.T) {
	handler := (&Server{}).Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/crypto/register-bundle", strings.NewReader(`{"userId":"u1"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "缺少必要参数") {
		t.Fatalf("body=%s", body)
	}
}

func TestRegisterKeyRejectsInvalidPublicKey(t *testing.T) {
	handler := (&Server{}).Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/crypto/register-key", strings.NewReader(`{"userId":"u1","publicKey":"short"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
