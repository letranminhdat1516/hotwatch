package ui_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/store"
	hwui "hotwatch/ui"
)

var macCaps = model.Caps{GPU: true, Battery: true, Docker: true, Processes: true, Apps: true, Packages: true}
var vpsCaps = model.Caps{Docker: true, Processes: true}

// count is how many texts of the last frame are exactly s.
func count(tt *ui.Tester, s string) int {
	n := 0
	for _, x := range tt.Texts() {
		if x == s {
			n++
		}
	}
	return n
}

func TestShellRendersEachPageTitle(t *testing.T) {
	var picked []string
	hosts := []store.Host{{ID: "h1", Name: "Mac", Kind: "local"}}
	v := hub.HostView{Info: model.HostInfo{Caps: macCaps}}
	app := hwui.NewApp(func(string) hub.HostView { return v }, hosts, &hub.FakeRunner{},
		func(_, page string) { picked = append(picked, page) })
	tt := ui.NewTester(app.View, 1400, 800)
	titles := []string{"Overview", "CPU", "Memory", "Disk", "Network", "GPU", "Energy",
		"Processes", "Docker", "Apps", "Packages", "Settings"}
	for _, title := range titles {
		if err := tt.Click(title); err != nil {
			t.Fatalf("%s: %v (texts %q)", title, err, tt.Texts())
		}
		// one in the top bar, one as the page title
		if n := count(tt, title); n != 2 {
			t.Errorf("%s: %d exact texts, want 2 (nav + title): %q", title, n, tt.Texts())
		}
	}
	want := []string{"cpu", "memory", "disk", "network", "gpu", "energy", "processes", "docker", "apps", "packages", "settings"}
	if !slices.Equal(picked, want) {
		t.Errorf("onSelect pages %q, want %q", picked, want)
	}
}

func TestShellTitleComesBeforeContent(t *testing.T) {
	app := testApp(hub.HostView{})
	tt := ui.NewTester(app.View, 1200, 800)
	texts := tt.Texts()
	title := slices.Index(texts, "Overview")
	// the second "Overview" is the page title, right before the empty state
	if i := slices.Index(texts[title+1:], "Overview"); i < 0 {
		t.Fatalf("no page title: %q", texts)
	}
	if !slices.Contains(texts, "Waiting for data…") {
		t.Fatalf("no empty state: %q", texts)
	}
}

func TestShellHidesPagesTheHostCannotShow(t *testing.T) {
	cases := []struct {
		name   string
		caps   model.Caps
		shown  []string
		hidden []string
	}{
		{"vps", vpsCaps, []string{"Overview", "CPU", "Processes", "Docker", "Settings"}, []string{"GPU", "Energy", "Apps", "Packages"}},
		{"mac", macCaps, []string{"GPU", "Energy", "Docker", "Apps", "Packages", "Settings"}, nil},
		{"none", model.Caps{}, []string{"Memory", "Disk", "Network", "Processes"}, []string{"GPU", "Energy", "Docker", "Apps", "Packages"}},
	}
	for _, tc := range cases {
		tt := ui.NewTester(testApp(hub.HostView{Info: model.HostInfo{Caps: tc.caps}}).View, 1400, 800)
		for _, s := range tc.shown {
			if !slices.Contains(tt.Texts(), s) {
				t.Errorf("%s: missing %q in %q", tc.name, s, tt.Texts())
			}
		}
		for _, s := range tc.hidden {
			if slices.Contains(tt.Texts(), s) {
				t.Errorf("%s: %q shown: %q", tc.name, s, tt.Texts())
			}
		}
	}
}

func TestShellFallsBackToOverviewWhenPageHidden(t *testing.T) {
	hosts := []store.Host{{ID: "mac", Name: "Mac"}, {ID: "vps", Name: "VPS"}}
	views := map[string]hub.HostView{
		"mac": {Info: model.HostInfo{Caps: macCaps}},
		"vps": {Info: model.HostInfo{Caps: vpsCaps}},
	}
	app := hwui.NewApp(func(id string) hub.HostView { return views[id] }, hosts, &hub.FakeRunner{}, func(string, string) {})
	tt := ui.NewTester(app.View, 1400, 800)
	if err := tt.Click("Energy"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Mac"); err != nil { // open the host switcher
		t.Fatal(err)
	}
	if err := tt.Click("VPS"); err != nil {
		t.Fatal(err)
	}
	if count(tt, "Overview") != 2 || slices.Contains(tt.Texts(), "Energy") {
		t.Fatalf("hidden page still open: %q", tt.Texts())
	}
}

func TestShellTopBarShowsHostAndConnection(t *testing.T) {
	tt := ui.NewTester(testApp(hub.HostView{Conn: hub.Conn{State: hub.Online}}).View, 1200, 800)
	for _, s := range []string{"Mac", "Online"} {
		if !slices.Contains(tt.Texts(), s) {
			t.Errorf("missing %q in %q", s, tt.Texts())
		}
	}
}

func TestShellBanner(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		conn hub.Conn
		want string
	}{
		{hub.Conn{State: hub.Offline, LastOK: now.Add(-2 * time.Minute)}, "Offline · last seen 2m ago"},
		{hub.Conn{State: hub.Offline}, "Offline · never connected"},
		{hub.Conn{State: hub.Stale, LastOK: now.Add(-12 * time.Second)}, "Stale · updated 12s ago"},
	}
	for _, tc := range cases {
		app := testApp(hub.HostView{Conn: tc.conn, Snap: &model.Snapshot{}})
		hwui.SetClock(app, func() time.Time { return now })
		tt := ui.NewTester(app.View, 1200, 800)
		if !slices.Contains(tt.Texts(), tc.want) {
			t.Errorf("missing banner %q in %q", tc.want, tt.Texts())
		}
	}
	app := testApp(hub.HostView{Conn: hub.Conn{State: hub.Online, LastOK: now}, Snap: &model.Snapshot{}})
	hwui.SetClock(app, func() time.Time { return now })
	tt := ui.NewTester(app.View, 1200, 800)
	for _, s := range tt.Texts() {
		if strings.HasPrefix(s, "Offline ·") || strings.HasPrefix(s, "Stale ·") {
			t.Fatalf("banner shown while online: %q", tt.Texts())
		}
	}
}

func TestShellBannerAgeIsComputedAtRender(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	clock := now
	app := testApp(hub.HostView{Conn: hub.Conn{State: hub.Offline, LastOK: now.Add(-2 * time.Minute)}})
	hwui.SetClock(app, func() time.Time { return clock })
	tt := ui.NewTester(app.View, 1200, 800)
	clock = now.Add(3 * time.Minute)
	tt.Frame()
	if !slices.Contains(tt.Texts(), "Offline · last seen 5m ago") {
		t.Fatalf("age not recomputed: %q", tt.Texts())
	}
}

func TestShellDarkModeSwitchesTokens(t *testing.T) {
	app := testApp(hub.HostView{Snap: &model.Snapshot{}})
	var surface, bg ui.Color
	view := func(c *ui.Context) {
		app.View(c)
		surface, bg = c.Theme().Surface, c.Theme().Background
	}
	tt := ui.NewTester(view, 1200, 800)
	light, dark := hwui.Theme(false), hwui.Theme(true)
	if surface != light.Surface || bg != light.Background {
		t.Fatalf("light frame uses %v/%v, want %v/%v", surface, bg, light.Surface, light.Background)
	}
	tt.SetDark(true)
	tt.Frame()
	if surface != dark.Surface || bg != dark.Background {
		t.Fatalf("dark frame uses %v/%v, want %v/%v", surface, bg, dark.Surface, dark.Background)
	}
	if light.Surface == dark.Surface || light.Background == dark.Background || light.Accent == dark.Accent {
		t.Fatal("light and dark tokens must differ")
	}
}

func TestShellNarrowWindowKeepsEveryPageReachable(t *testing.T) {
	// The sidebar keeps every page as a labelled row even in a small window.
	tt := ui.NewTester(testApp(hub.HostView{Info: model.HostInfo{Caps: macCaps}}).View, 900, 700)
	if _, ok := tt.Find("Packages"); !ok {
		t.Fatalf("Packages row not found: %q", tt.Texts())
	}
	if err := tt.Click("Packages"); err != nil {
		t.Fatal(err)
	}
	if count(tt, "Packages") != 2 {
		t.Fatalf("Packages not open: %q", tt.Texts())
	}
}

func TestCPUSubtitleNamesTheChipAndCoreKinds(t *testing.T) {
	v := hub.HostView{Info: model.HostInfo{Machine: model.Machine{Chip: "Apple M5", PerfCores: 4, EffCores: 6}},
		Snap: &model.Snapshot{CPU: model.CPU{PerCore: make([]float64, 10)}}}
	if got := hwui.PageSubtitle("cpu", v); got != "Apple M5 · 4 Performance + 6 Efficiency cores" {
		t.Fatalf("subtitle %q", got)
	}
	v.Info.Machine = model.Machine{}
	if got := hwui.PageSubtitle("cpu", v); got != "10 cores" {
		t.Fatalf("without the split: %q", got)
	}
}
