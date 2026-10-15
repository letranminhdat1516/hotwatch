package pkgs

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestFakeListsAndStreamsAFailingTask(t *testing.T) {
	f := &Fake{Pkgs: []Package{{Name: "go", Kind: "formula", Version: "1.27.0", Newest: "1.27.1", Outdated: true}},
		Log: []string{"==> Upgrading go", "Error: no bottle"}, Fail: true, Brew: true}
	ctx := context.Background()
	got, err := f.List(ctx)
	if err != nil || len(got) != 1 || !got[0].Outdated {
		t.Fatalf("List = %+v %v", got, err)
	}
	if brew, mise := f.Available(); !brew || mise {
		t.Fatalf("Available = %v %v", brew, mise)
	}
	lines, wait, err := f.Run(ctx, "upgrade", "go")
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	for l := range lines {
		seen = append(seen, l)
	}
	if !slices.Equal(seen, f.Log) {
		t.Fatalf("lines %q", seen)
	}
	if werr := wait(); werr == nil || !strings.Contains(werr.Error(), "exit status 1") {
		t.Fatalf("wait = %v", werr)
	}
	if !slices.Equal(f.Ran, []string{"upgrade go"}) {
		t.Fatalf("ran %v", f.Ran)
	}
	var _ Manager = f
}
