package store

import (
	"errors"
	"testing"
)

func TestMigratingMovesOnFirstReadAndDeletesBoth(t *testing.T) {
	to, from := NewMemSecrets(), NewMemSecrets()
	from.Set("old", "t1")
	s := Migrating{To: to, From: from}

	if v, err := s.Get("old"); err != nil || v != "t1" {
		t.Fatalf("first read: %q %v", v, err)
	}
	if v, err := to.Get("old"); err != nil || v != "t1" {
		t.Fatalf("not moved: %q %v", v, err)
	}
	if err := s.Set("new", "t2"); err != nil {
		t.Fatal(err)
	}
	if _, err := from.Get("new"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Set wrote to From: %v", err)
	}
	if err := s.Delete("old"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("old"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("deleted secret came back: %v", err)
	}
}
