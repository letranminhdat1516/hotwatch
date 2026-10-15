package pages_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/mac/apps"
	hwui "hotwatch/ui"
	"hotwatch/ui/pages"
)

type appsEnv struct {
	tt   *ui.Tester
	page *pages.Apps
	run  *hub.FakeRunner
	acts *pages.Actions
	scan *apps.Fake
}

func newAppsEnv(list []apps.App, height int) *appsEnv {
	e := &appsEnv{run: &hub.FakeRunner{}, scan: &apps.Fake{List: list, Sizes: map[string]uint64{}}}
	for _, a := range list {
		e.scan.Sizes[a.Bundle] = uint64(len(a.Name)) << 20
	}
	e.acts = pages.NewActions(e.run)
	e.page = pages.NewApps(e.scan, func(f func()) { f() }) // deliveries run inline…
	e.page.Spawn = func(f func()) { f() }                  // …and so does the work, on the test's thread
	e.page.Open = func(string) {}
	e.tt = ui.NewTester(func(c *ui.Context) {
		c.SetTheme(hwui.Theme(false))
		e.page.Render(c, pages.AppsInput{HostID: "local", Now: time.Now(), Actions: e.acts, Width: 1200})
	}, 1300, height)
	e.tt.Frame() // the scan lands on the second frame
	return e
}

func sampleApps() []apps.App {
	return []apps.App{
		{Name: "Safari", Bundle: "/Applications/Safari.app", BundleID: "com.apple.Safari", Version: "26.1", Arch: "Universal"},
		{Name: "Visual Studio Code", Bundle: "/Applications/Visual Studio Code.app", BundleID: "com.microsoft.VSCode", Version: "1.49.2", Arch: "Universal", Source: "Homebrew",
			LastOpened: time.Date(2026, 10, 15, 8, 55, 0, 0, time.Local)},
		{Name: "Xcode", Bundle: "/Applications/Xcode.app", BundleID: "com.apple.dt.Xcode", Version: "26.0.1", Arch: "Apple silicon", Source: "App Store"},
	}
}

func TestAppsListsSearchesAndInspects(t *testing.T) {
	e := newAppsEnv(sampleApps(), 900)
	for _, want := range []string{"Name", "Version", "Size", "Kind", "Last opened", "Source",
		"Safari", "26.1", "Universal", "Visual Studio Code", "Homebrew", "Xcode", "App Store", "Apple silicon", "3 apps · 29.0 MB"} {
		if !slices.Contains(e.tt.Texts(), want) {
			t.Errorf("missing %q in %q", want, e.tt.Texts())
		}
	}
	if err := e.tt.Click("Search apps"); err != nil {
		t.Fatal(err)
	}
	e.tt.Type("code")
	if slices.Contains(e.tt.Texts(), "Safari") || !slices.Contains(e.tt.Texts(), "Visual Studio Code") {
		t.Fatalf("search did not narrow: %q", e.tt.Texts())
	}
	if err := e.tt.Click("Visual Studio Code"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"com.microsoft.VSCode", "/Applications/Visual Studio Code.app", "Uninstall", "Open"} {
		if !slices.Contains(e.tt.Texts(), want) {
			t.Errorf("inspector missing %q in %q", want, e.tt.Texts())
		}
	}
	if err := e.tt.Click("Uninstall"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(e.tt.Texts(), "Move “Visual Studio Code” to the Trash?") {
		t.Fatalf("no confirmation: %q", e.tt.Texts())
	}
	if len(e.run.Started) != 0 {
		t.Fatal("an action started before the confirmation")
	}
	if err := e.tt.Click("Move to Trash"); err != nil {
		t.Fatal(err)
	}
	if len(e.run.Started) != 1 || e.run.Started[0].Kind != "uninstall" || e.run.Started[0].Target != "/Applications/Visual Studio Code.app" || e.run.Started[0].HostID != "local" {
		t.Fatalf("started %+v", e.run.Started)
	}
}

func TestAppsSizesOnlyVisibleRows(t *testing.T) {
	var list []apps.App
	for i := 0; i < 200; i++ {
		list = append(list, apps.App{Name: fmt.Sprintf("App %03d", i), Bundle: fmt.Sprintf("/Applications/App %03d.app", i)})
	}
	e := newAppsEnv(list, 400)
	e.tt.Frame()
	e.tt.Frame()
	e.scan.Mu.Lock()
	n := len(e.scan.SizeCalls)
	e.scan.Mu.Unlock()
	if n == 0 || n >= 100 {
		t.Fatalf("%d size calls for a 400 px window: want only the visible rows", n)
	}
	// "App 000" is 7 characters: the fake sizes it at 7 MB, delivered inline.
	if !slices.Contains(e.tt.Texts(), "7.0 MB") {
		t.Fatalf("no size shown after delivery: %q", e.tt.Texts()[:20])
	}
}

func TestAppsWithoutAScannerExplains(t *testing.T) {
	page := pages.NewApps(nil, nil)
	tt := ui.NewTester(func(c *ui.Context) {
		page.Render(c, pages.AppsInput{HostID: "vps", Now: time.Now(), Actions: pages.NewActions(&hub.FakeRunner{}), Width: 1200})
	}, 1300, 600)
	if !tt.HasText("Apps are listed for this Mac only") {
		t.Fatalf("texts %q", tt.Texts())
	}
}
