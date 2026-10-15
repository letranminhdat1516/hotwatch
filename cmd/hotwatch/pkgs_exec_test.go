package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	"hotwatch/hub"
	"hotwatch/mac/pkgs"
)

func TestPackageExecutorStreamsLinesAndReportsTheExit(t *testing.T) {
	f := &pkgs.Fake{Brew: true, Log: []string{"==> Upgrading go", "Error: no bottle"}, Fail: true}
	ex := packageExecutor(f)
	var lines []string
	err := ex(context.Background(), &hub.Action{HostID: "local", Verb: "upgrade", Target: "formula:go"}, func(s string) { lines = append(lines, s) })
	if err == nil || !strings.Contains(err.Error(), "exit status 1") {
		t.Fatalf("err = %v", err)
	}
	if !slices.Equal(lines, f.Log) || !slices.Equal(f.Ran, []string{"upgrade formula:go"}) {
		t.Fatalf("lines %v ran %v", lines, f.Ran)
	}
	if err := ex(context.Background(), &hub.Action{HostID: "vps", Verb: "upgrade", Target: "formula:go"}, func(string) {}); err == nil {
		t.Fatal("a remote host must not run package tasks")
	}
}
