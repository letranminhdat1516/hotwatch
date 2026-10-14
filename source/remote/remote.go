// Package remote is the source.Source for a hotwatch-agent reached through a
// dialer. Transport and HTTP errors are returned intact for hub.Classify;
// protocol-version gating is the poller's job.
package remote

import (
	"context"
	"net"

	"hotwatch/agentclient"
)

// New returns a source that reaches the agent through dial with token.
func New(dial func(ctx context.Context) (net.Conn, error), token string) *agentclient.Client {
	return agentclient.New(dial, token)
}
