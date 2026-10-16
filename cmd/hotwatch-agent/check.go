package main

import (
	"flag"
	"fmt"
	"io"

	"hotwatch/agent"
)

// cmdCheck validates the config the way run does (format, digest, access)
// and prints what it found, never the digest.
func cmdCheck(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	path := fs.String("config", agent.DefaultConfigPath, "config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	cfg, err := agent.LoadConfig(*path)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s: ok (listen %s, readOnly=%v, interval %d ms)\n", *path, cfg.Listen, cfg.ReadOnly, cfg.IntervalMs)
	return nil
}
