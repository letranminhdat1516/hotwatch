package main

import (
	"context"
	"slices"
	"testing"

	"hotwatch/hub"
	"hotwatch/mac/apps"
)

func TestUninstallExecutorTrashesOnlyLocalBundles(t *testing.T) {
	f := &apps.Fake{List: []apps.App{{Name: "A", Bundle: "/Applications/A.app"}}}
	ex := uninstallExecutor(f)
	if err := ex(context.Background(), &hub.Action{HostID: "vps", Target: "/Applications/A.app"}, func(string) {}); err == nil {
		t.Fatal("a remote host must not uninstall")
	}
	var lines []string
	if err := ex(context.Background(), &hub.Action{HostID: "local", Target: "/Applications/A.app"}, func(s string) { lines = append(lines, s) }); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.Removed, []string{"/Applications/A.app"}) || len(lines) != 1 {
		t.Fatalf("removed %v lines %v", f.Removed, lines)
	}
}
