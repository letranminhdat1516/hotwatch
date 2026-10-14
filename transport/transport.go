// Package transport opens connections from the app to a host's agent.
package transport

import (
	"context"
	"fmt"
	"net"

	"golang.org/x/crypto/ssh"
)

// Dialer opens connections to one host's agent HTTP port.
type Dialer interface {
	DialAgent(ctx context.Context) (net.Conn, error) // connection to the agent's HTTP port
	Close() error
}

// HostKeyError reports an SSH host key that is unknown or has changed.
type HostKeyError struct {
	Host        string        // known_hosts form: "host" or "[host]:port"
	Addr        string        // "host:port" exactly as dialed; trust binds to this
	Fingerprint string        // "SHA256:…"
	Mismatch    bool          // true = key changed (hard stop); false = unknown (ask user)
	Key         ssh.PublicKey // the key the server presented; pass it to TrustHostKey after approval
}

func (e *HostKeyError) Error() string {
	if e.Mismatch {
		return fmt.Sprintf("SSH host key for %s changed: %s", e.Host, e.Fingerprint)
	}
	return fmt.Sprintf("unknown SSH host key for %s: %s", e.Host, e.Fingerprint)
}
