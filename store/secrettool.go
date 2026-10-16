package store

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// errSecretToolMissing is secret-tool's "no such item" (exit status 1 and
// no output).
var errSecretToolMissing = errors.New("secret-tool: no matching item")

// SecretTool stores secrets in the desktop's Secret Service (GNOME Keyring,
// KWallet) through libsecret's secret-tool. Secrets go through stdin and
// stdout, never the command line.
type SecretTool struct {
	service string
	run     func(stdin string, args ...string) (string, error)
}

// NewSecretTool returns the store, or false when secret-tool is not
// installed.
func NewSecretTool(service string) (*SecretTool, bool) {
	path, err := exec.LookPath("secret-tool")
	if err != nil {
		return nil, false
	}
	return &SecretTool{service: service, run: func(stdin string, args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, path, args...)
		cmd.Stdin = strings.NewReader(stdin)
		var out bytes.Buffer
		cmd.Stdout = &out
		err := cmd.Run()
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 && out.Len() == 0 && args[0] == "lookup" {
			return "", errSecretToolMissing
		}
		return out.String(), err
	}}, true
}

func (s *SecretTool) attrs(key string) []string { return []string{"service", s.service, "key", key} }

func (s *SecretTool) Get(key string) (string, error) {
	v, err := s.run("", append([]string{"lookup"}, s.attrs(key)...)...)
	if errors.Is(err, errSecretToolMissing) {
		return "", ErrSecretNotFound
	}
	return strings.TrimSuffix(v, "\n"), err
}

func (s *SecretTool) Set(key, value string) error {
	_, err := s.run(value, append([]string{"store", "--label=" + s.service + " " + key}, s.attrs(key)...)...)
	return err
}

func (s *SecretTool) Delete(key string) error {
	_, err := s.run("", append([]string{"clear"}, s.attrs(key)...)...)
	return err
}
