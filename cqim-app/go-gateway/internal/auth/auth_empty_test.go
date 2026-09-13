package auth

import "testing"

func TestNewVerifierRefusesEmptyDBPath(t *testing.T) {
	if _, err := NewVerifier(""); err == nil {
		t.Fatal("expected NewVerifier(\"\") to fail")
	}
	if _, err := NewVerifier("   "); err == nil {
		t.Fatal("expected NewVerifier(whitespace) to fail")
	}
}
