package store

import "errors"

// Migrating reads and writes To, and moves a secret only From still holds
// into To the first time it is read. Delete clears both, so a moved-out
// secret cannot come back.
type Migrating struct{ To, From Secrets }

var _ Secrets = Migrating{}

func (s Migrating) Get(key string) (string, error) {
	v, err := s.To.Get(key)
	if !errors.Is(err, ErrSecretNotFound) {
		return v, err
	}
	if v, err = s.From.Get(key); err != nil {
		return "", err
	}
	return v, s.To.Set(key, v)
}

func (s Migrating) Set(key, value string) error { return s.To.Set(key, value) }

func (s Migrating) Delete(key string) error {
	return errors.Join(s.To.Delete(key), s.From.Delete(key))
}
