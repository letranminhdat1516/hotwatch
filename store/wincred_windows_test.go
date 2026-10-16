package store

import (
	"errors"
	"fmt"
	"os"
	"testing"
)

func TestCredentialManagerRoundTrip(t *testing.T) {
	s := NewCredentialManager(fmt.Sprintf("HotwatchTest%d", os.Getpid()))
	key := "hotwatch/host/test"
	if _, err := s.Get(key); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if err := s.Set(key, "token-1"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Delete(key) })
	if err := s.Set(key, "token-2"); err != nil {
		t.Fatal(err)
	}
	if v, err := s.Get(key); err != nil || v != "token-2" {
		t.Fatalf("get %q %v", v, err)
	}
	if err := s.Delete(key); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(key); err != nil {
		t.Fatalf("deleting a missing key: %v", err)
	}
}
