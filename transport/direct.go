package transport

import (
	"context"
	"net"
	"strconv"
	"time"
)

const directDialTimeout = 3 * time.Second

type direct struct {
	address string
	port    int
}

// NewDirect returns a Dialer that connects over plain TCP to address:port
// (a Tailscale or LAN address). It holds no pooled state.
func NewDirect(address string, port int) Dialer {
	return &direct{address: address, port: port}
}

func (d *direct) DialAgent(ctx context.Context) (net.Conn, error) {
	dialer := net.Dialer{Timeout: directDialTimeout}
	return dialer.DialContext(ctx, "tcp", net.JoinHostPort(d.address, strconv.Itoa(d.port)))
}

func (d *direct) Close() error { return nil }
