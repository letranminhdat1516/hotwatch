package pages_test

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/mac/pkgs"
	"hotwatch/model"
	hwui "hotwatch/ui"
	"hotwatch/ui/pages"
)

type pkgsEnv struct {
	tt   *ui.Tester
	page *pages.Packages
	run  *hub.FakeRunner
	acts *pages.Actions
	mgr  *pkgs.Fake
}

func newPkgsEnv(mgr *pkgs.Fake) *pkgsEnv {
	e := &pkgsEnv{run: &hub.FakeRunner{}, mgr: mgr}
	e.acts = pages.NewActions(e.run)
	e.page = pages.NewPackages(mgr, func(f func()) { f() })
	e.page.Spawn = func(f func()) { f() }
	e.tt = ui.NewTester(func(c *ui.Context) {
		c.SetTheme(hwui.Theme(false))
		e.page.Render(c, pages.PackagesInput{HostID: "local", Now: time.Now(), Actions: e.acts, Width: 1200})
	}, 1300, 900)
	e.tt.Frame()
	return e
}

func samplePkgs() []pkgs.Package {
	return []pkgs.Package{
		{Name: "go", Kind: "formula", Version: "1.27.0", Newest: "1.27.1", Outdated: true},
		{Name: "node", Kind: "formula", Version: "24.9.0", Newest: "24.10.0", Outdated: true},
		{Name: "ripgrep", Kind: "formula", Version: "15.1.0", Newest: "15.1.0"},
		{Name: "visual-studio-code", Kind: "cask", Version: "1.49.2", Newest: "1.53.0", Outdated: true},
		{Name: "python", Kind: "mise", Version: "3.13.5", Newest: "3.13.5"},
	}
}

func TestPackagesListsOutdatedAndStartsAnUpdate(t *testing.T) {
	e := newPkgsEnv(&pkgs.Fake{Pkgs: samplePkgs(), Brew: true, Mise: true})
	for _, want := range []string{"Homebrew", "mise", "3 outdated", "go", "1.27.0", "1.27.1", "visual-studio-code", "cask", "Update All"} {
		if !slices.Contains(e.tt.Texts(), want) {
			t.Errorf("missing %q in %q", want, e.tt.Texts())
		}
	}
	if slices.Contains(e.tt.Texts(), "python") {
		t.Fatalf("a mise tool is listed under the Homebrew tab: %q", e.tt.Texts())
	}
	// The first "Update" button belongs to the first outdated row (go).
	if err := e.tt.Click("Update"); err != nil {
		t.Fatal(err)
	}
	if len(e.run.Started) != 1 || e.run.Started[0].Kind != "package" || e.run.Started[0].Verb != "upgrade" || e.run.Started[0].Target != "formula:go" {
		t.Fatalf("started %+v", e.run.Started)
	}
	// The runner streams log lines; the page shows them and, on success,
	// reloads the list, which the fake now reports as current.
	e.mgr.Mu.Lock()
	e.mgr.Pkgs[0].Outdated, e.mgr.Pkgs[0].Version = false, "1.27.1"
	e.mgr.Mu.Unlock()
	id := e.run.Started[0].ID
	e.acts.Apply(hub.Action{ID: id, Kind: "package", HostID: "local", Verb: "upgrade", Target: "formula:go", State: "running", Seq: 1,
		Log: []string{"==> Upgrading go", "  1.27.0 -> 1.27.1"}})
	e.tt.Frame()
	if !slices.Contains(e.tt.Texts(), "==> Upgrading go") {
		t.Fatalf("log line not shown: %q", e.tt.Texts())
	}
	e.acts.Apply(hub.Action{ID: id, Kind: "package", HostID: "local", Verb: "upgrade", Target: "formula:go", State: "succeeded", Seq: 2,
		Log: []string{"==> Upgrading go", "  1.27.0 -> 1.27.1"}})
	e.tt.Frame()
	e.tt.Frame()
	if !slices.Contains(e.tt.Texts(), "2 outdated") {
		t.Fatalf("badge did not decrement after the upgrade: %q", e.tt.Texts())
	}
}

func TestPackagesShowsAFailureWithItsExitCode(t *testing.T) {
	e := newPkgsEnv(&pkgs.Fake{Pkgs: samplePkgs(), Brew: true})
	e.acts.Apply(hub.Action{ID: 7, Kind: "package", HostID: "local", Verb: "upgrade", Target: "formula:node", State: "failed", Seq: 1,
		Err: &model.APIError{Code: "failed", Message: "brew: exit status 1"}, Log: []string{"==> Upgrading node", "Error: no bottle available"}})
	e.tt.Frame()
	for _, want := range []string{"exit status 1", "Error: no bottle available"} {
		if !slices.Contains(e.tt.Texts(), want) {
			t.Errorf("missing %q in %q", want, e.tt.Texts())
		}
	}
}

func TestPackagesWithoutHomebrewSaysSo(t *testing.T) {
	e := newPkgsEnv(&pkgs.Fake{})
	if !e.tt.HasText("Homebrew is not installed") {
		t.Fatalf("texts %q", e.tt.Texts())
	}
}

func TestPackagesMiseTab(t *testing.T) {
	e := newPkgsEnv(&pkgs.Fake{Pkgs: samplePkgs(), Brew: true, Mise: true})
	if err := e.tt.Click("mise"); err != nil {
		t.Fatal(err)
	}
	e.tt.Frame()
	if !slices.Contains(e.tt.Texts(), "python") || slices.Contains(e.tt.Texts(), "ripgrep") {
		t.Fatalf("mise tab: %q", e.tt.Texts())
	}
}
