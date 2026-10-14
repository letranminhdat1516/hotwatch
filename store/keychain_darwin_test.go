//go:build darwin

package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
)

// TestKeychainRoundTrip uses the login Keychain under a throwaway service
// name and deletes what it wrote.
func TestKeychainRoundTrip(t *testing.T) {
	var b [6]byte
	rand.Read(b[:])
	k, err := NewKeychain("hotwatch-test-" + hex.EncodeToString(b[:]))
	if err != nil {
		t.Fatal(err)
	}
	key := TokenKey("h-test")
	t.Cleanup(func() { k.Delete(key) })

	if _, err := k.Get(key); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Get before Set: %v, want ErrSecretNotFound", err)
	}
	if err := k.Set(key, "first-token-value"); err != nil {
		if errors.Is(err, errKeychainInteraction) {
			t.Skip("login Keychain is locked or needs interaction")
		}
		t.Fatal(err)
	}
	if v, err := k.Get(key); err != nil || v != "first-token-value" {
		t.Fatalf("Get = %q, %v", v, err)
	}
	if err := k.Set(key, "second-token-value"); err != nil {
		t.Fatal(err)
	}
	if v, err := k.Get(key); err != nil || v != "second-token-value" {
		t.Fatalf("Get after overwrite = %q, %v", v, err)
	}
	if err := k.Delete(key); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Get(key); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Get after Delete: %v", err)
	}
	if err := k.Delete(key); err != nil {
		t.Fatalf("deleting a missing key: %v", err)
	}
}
