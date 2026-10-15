package main

import (
	"context"
	"fmt"
	"time"

	"hotwatch/hub"
	"hotwatch/mac/apps"
	"hotwatch/mac/exec"
)

// uninstallExecutor runs "uninstall" actions: the target is the bundle path
// and the scanner moves it to the Trash. Only the local host has apps.
func uninstallExecutor(s apps.Scanner) hub.Executor {
	return func(ctx context.Context, a *hub.Action, log func(string)) error {
		if a.HostID != "local" {
			return fmt.Errorf("uninstall: %s has no applications", a.HostID)
		}
		log("Moving " + a.Target + " to the Trash")
		return s.Uninstall(ctx, a.Target)
	}
}

// openBundle launches an application bundle through `open` (argv, 10 s).
func (c *core) openBundle(bundle string) {
	go func() {
		if _, err := exec.System(10*time.Second)(context.Background(), "open", bundle); err != nil {
			c.log.Warn("open", "bundle", bundle, "err", err)
		}
	}()
}
