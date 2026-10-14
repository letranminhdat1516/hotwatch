package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"hotwatch/agent"
)

var (
	loopback  = netip.MustParseAddr("127.0.0.1")
	tailnet   = netip.MustParsePrefix("100.64.0.0/10")
	portDigit = regexp.MustCompile(`^[0-9]{1,5}$`)
)

// validateListen accepts only 127.0.0.1:<port> or <tailscale-range IPv4>:<port>.
// Hostnames, wildcards, IPv6 and every other address are refused; there is no
// override.
func validateListen(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("listen %q: %w", addr, err)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.Is4() {
		return fmt.Errorf("listen %q: host must be an IPv4 address literal", addr)
	}
	if ip != loopback && !tailnet.Contains(ip) {
		return fmt.Errorf("listen %q: only 127.0.0.1 or a Tailscale address (100.64.0.0/10) is allowed", addr)
	}
	if !portDigit.MatchString(port) {
		return fmt.Errorf("listen %q: invalid port", addr)
	}
	if p, _ := strconv.Atoi(port); p < 1 || p > 65535 {
		return fmt.Errorf("listen %q: port out of range", addr)
	}
	return nil
}

// checkAssigned re-validates addr and, for a non-loopback address, requires it
// to be assigned to a local interface right now.
func checkAssigned(addr string, localIPs func() ([]net.IP, error)) error {
	if err := validateListen(addr); err != nil {
		return err
	}
	host, _, _ := net.SplitHostPort(addr)
	ip := netip.MustParseAddr(host)
	if ip == loopback {
		return nil
	}
	ips, err := localIPs()
	if err != nil {
		return fmt.Errorf("listing interface addresses: %w", err)
	}
	for _, l := range ips {
		if a, ok := netip.AddrFromSlice(l); ok && a.Unmap() == ip {
			return nil
		}
	}
	return fmt.Errorf("listen address %s is not assigned to a local interface", ip)
}

func interfaceIPs() ([]net.IP, error) {
	as, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	var ips []net.IP
	for _, a := range as {
		if n, ok := a.(*net.IPNet); ok {
			ips = append(ips, n.IP)
		}
	}
	return ips, nil
}

// tailscaleIP returns the output of `tailscale ip -4`.
func tailscaleIP() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tailscale", "ip", "-4").Output()
	return string(out), err
}

// cmdInit writes a fresh config and prints the new token (only) to stdout.
func cmdInit(args []string, stdout, stderr io.Writer, tsIP func() (string, error)) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	mode := fs.String("mode", "", "tailscale or ssh (default: tailscale if available, else ssh)")
	port := fs.Int("port", 7171, "TCP port")
	readOnly := fs.Bool("read-only", false, "reject every POST (no signals, no container actions)")
	rotate := fs.Bool("rotate", false, "replace an existing config with a new token")
	path := fs.String("config", agent.DefaultConfigPath, "config file to write")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if *port < 1 || *port > 65535 {
		return errors.New("port must be 1-65535")
	}

	tailscaleListen := func() (string, error) {
		out, err := tsIP()
		if err != nil {
			return "", fmt.Errorf("tailscale ip -4: %w", err)
		}
		l := net.JoinHostPort(strings.TrimSpace(out), strconv.Itoa(*port))
		if err := validateListen(l); err != nil {
			return "", err
		}
		if strings.HasPrefix(l, "127.") {
			return "", errors.New("tailscale ip -4 returned a loopback address")
		}
		return l, nil
	}
	var listen string
	var err error
	switch *mode {
	case "ssh":
		listen = net.JoinHostPort("127.0.0.1", strconv.Itoa(*port))
	case "tailscale":
		listen, err = tailscaleListen()
	case "":
		if listen, err = tailscaleListen(); err != nil {
			listen, err = net.JoinHostPort("127.0.0.1", strconv.Itoa(*port)), nil
		}
	default:
		return fmt.Errorf("unknown mode %q (want tailscale or ssh)", *mode)
	}
	if err != nil {
		return err
	}
	if err := validateListen(listen); err != nil {
		return err
	}
	if _, err := os.Stat(*path); err == nil && !*rotate {
		return fmt.Errorf("%s already exists; use --rotate to replace it with a new token", *path)
	}
	if err := os.MkdirAll(filepath.Dir(*path), 0o750); err != nil {
		return err
	}
	token := agent.NewToken()
	cfg := agent.FileConfig{Listen: listen, TokenSHA256: agent.SHA256Hex(token), ReadOnly: *readOnly, IntervalMs: agent.DefaultIntervalMs}
	if err := agent.WriteConfig(*path, cfg, *rotate); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "hotwatch-agent: wrote %s (listen %s). The token below is shown once; store it in the app.\n", *path, listen)
	fmt.Fprintln(stdout, token)
	return nil
}
