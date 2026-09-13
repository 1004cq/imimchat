package store

import "testing"

func TestNewRefusesEmptyDBPath(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Fatal("expected New(\"\") to fail")
	}
	if _, err := New("   "); err == nil {
		t.Fatal("expected New(whitespace) to fail")
	}
}
