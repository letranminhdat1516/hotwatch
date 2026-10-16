// Command hotwatch-agent serves the hotwatch /v1 API for one host: Linux,
// Windows (desktop or Server, as a service) or a BSD.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "run" {
		// Under the Windows service manager: run until it stops the service.
		handled, err := runService(func(ctx context.Context) error { return runCtx(ctx, args[1:]) })
		if handled {
			if err != nil {
				fmt.Fprintln(os.Stderr, "hotwatch-agent:", err)
				os.Exit(1)
			}
			return
		}
	}
	cmd := "run"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "init":
		err = cmdInit(args, os.Stdout, os.Stderr, tailscaleIP)
	case "run":
		err = cmdRun(args)
	case "check":
		err = cmdCheck(args, os.Stdout)
	case "version":
		fmt.Println("hotwatch-agent", version)
	default:
		err = fmt.Errorf("unknown command %q (want init, run, check or version)", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "hotwatch-agent:", err)
		os.Exit(1)
	}
}
