package store

import (
	"errors"
	"sync"
)

// ErrSecretNotFound is returned by Secrets.Get for an absent key.
var ErrSecretNotFound = errors.New("secret not found")

// Secrets holds host tokens outside config.json (Keychain in the app).
type Secrets interface {
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error // deleting a missing key is not an error
}

// TokenKey is the Secrets key for a host's agent token.
func TokenKey(hostID string) string { return "hotwatch/host/" + hostID }

// MemSecrets is an in-memory Secrets for tests.
type MemSecrets struct {
	mu sync.Mutex
	m  map[string]string
}

func NewMemSecrets() *MemSecrets { return &MemSecrets{m: map[string]string{}} }

func (s *MemSecrets) Get(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key]
	if !ok {
		return "", ErrSecretNotFound
	}
	return v, nil
}

func (s *MemSecrets) Set(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = value
	return nil
}

func (s *MemSecrets) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}
