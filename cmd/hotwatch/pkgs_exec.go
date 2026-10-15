package main

import (
	"context"
	"fmt"

	"hotwatch/hub"
	"hotwatch/mac/pkgs"
)

// packageExecutor runs "package" actions on this Mac's brew or mise: the
// verb and the "kind:name" target go to the manager, every output line to
// the action's log. Only the local host has packages.
func packageExecutor(m pkgs.Manager) hub.Executor {
	return func(ctx context.Context, a *hub.Action, log func(string)) error {
		if a.HostID != "local" {
			return fmt.Errorf("packages: %s has no package manager here", a.HostID)
		}
		lines, wait, err := m.Run(ctx, a.Verb, a.Target)
		if err != nil {
			return err
		}
		for l := range lines {
			log(l)
		}
		return wait()
	}
}
