// Package remote is the source.Source for a hotwatch-agent reached through a
// dialer. It adds protocol-version gating to agentclient; transport and HTTP
// errors are left intact for hub.Classify.
package remote

import (
	"context"
	"net"

	"hotwatch/agentclient"
	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/source"
)

// Source wraps an agentclient.Client.
type Source struct {
	*agentclient.Client
}

var _ source.Source = (*Source)(nil)

// New returns a Source that reaches the agent through dial with token.
func New(dial func(ctx context.Context) (net.Conn, error), token string) *Source {
	return &Source{agentclient.New(dial, token)}
}

// Info returns the agent's info. When its protocol major version differs it
// returns the info together with *hub.IncompatibleError so callers can still
// show the agent's version.
func (s *Source) Info(ctx context.Context) (model.HostInfo, error) {
	info, err := s.Client.Info(ctx)
	if err != nil {
		return info, err
	}
	if !model.Compatible(info.ProtocolVersion) {
		return info, &hub.IncompatibleError{Version: info.ProtocolVersion}
	}
	return info, nil
}
