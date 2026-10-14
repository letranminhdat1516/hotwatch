// Command hotwatch-agent serves the hotwatch /v1 API for one Linux host.
package main

import (
	"fmt"
	"os"
	"strings"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	args := os.Args[1:]
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
	case "version":
		fmt.Println("hotwatch-agent", version)
	default:
		err = fmt.Errorf("unknown command %q (want init, run or version)", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "hotwatch-agent:", err)
		os.Exit(1)
	}
}
