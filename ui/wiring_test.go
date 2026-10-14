package ui_test

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/store"
	hwui "hotwatch/ui"
)

type selection struct{ host, page string }

func twoHostApp(picked *[]selection) *hwui.App {
	hosts := []store.Host{{ID: "local", Name: "This Mac", Kind: "local"}, {ID: "h2", Name: "VPS", Kind: "agent"}}
	return hwui.NewApp(func(string) hub.HostView { return hub.HostView{} }, hosts, &hub.FakeRunner{},
		func(h, p string) { *picked = append(*picked, selection{h, p}) })
}

func TestSelectHostSwitchesHost(t *testing.T) {
	var picked []selection
	app := twoHostApp(&picked)
	app.SelectHost("nope") // unknown: ignored
	app.SelectHost("h2")
	app.SelectHost("h2") // already selected: no second call
	if want := []selection{{"h2", "overview"}}; !slices.Equal(picked, want) {
		t.Fatalf("onSelect %v, want %v", picked, want)
	}
	tt := ui.NewTester(app.View, 1200, 800)
	if !tt.HasText("VPS") {
		t.Fatalf("selected host not shown: %q", tt.Texts())
	}
}

func TestSetHostsDropsRemovedSelectedHost(t *testing.T) {
	var picked []selection
	app := twoHostApp(&picked)
	app.SelectHost("h2")
	app.SetHosts([]store.Host{{ID: "local", Name: "This Mac", Kind: "local"}})
	if want := []selection{{"h2", "overview"}, {"local", "overview"}}; !slices.Equal(picked, want) {
		t.Fatalf("onSelect %v, want %v", picked, want)
	}
	// A renamed and an added host show; the selection stays put.
	app.SetHosts([]store.Host{{ID: "local", Name: "Studio", Kind: "local"}, {ID: "h3", Name: "NAS", Kind: "agent"}})
	if len(picked) != 2 {
		t.Fatalf("selection moved: %v", picked)
	}
	tt := ui.NewTester(app.View, 1200, 800)
	if !tt.HasText("Studio") || !tt.HasText("NAS") || tt.HasText("VPS") {
		t.Fatalf("hosts not replaced: %q", tt.Texts())
	}
}

func TestNoticeShowsUntilDismissed(t *testing.T) {
	app := testApp(hub.HostView{})
	app.SetNotice("config was unreadable; started with defaults")
	tt := ui.NewTester(app.View, 1200, 800)
	if !tt.HasText("config was unreadable; started with defaults") {
		t.Fatalf("notice missing: %q", tt.Texts())
	}
	if err := tt.Click("Dismiss"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("config was unreadable; started with defaults") {
		t.Fatalf("notice still shown after Dismiss: %q", tt.Texts())
	}
}

type answer struct {
	pass string
	ok   bool
}

func TestPassphrasePromptAnswersOnce(t *testing.T) {
	app := testApp(hub.HostView{})
	var got []answer
	app.AskPassphrase("/Users/me/.ssh/id_ed25519", func(p string, ok bool) { got = append(got, answer{p, ok}) })
	app.AskPassphrase("/Users/me/.ssh/other", func(p string, ok bool) { got = append(got, answer{p, ok}) })
	tt := ui.NewTester(app.View, 1200, 800)
	if !tt.HasText("/Users/me/.ssh/id_ed25519") || tt.HasText("/Users/me/.ssh/other") {
		t.Fatalf("first prompt not shown alone: %q", tt.Texts())
	}
	tt.Type("s3cret")
	if err := tt.Click("Unlock"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("s3cret") {
		t.Fatal("passphrase shown in clear text")
	}
	// The second prompt follows; Cancel answers it as declined.
	if !tt.HasText("/Users/me/.ssh/other") {
		t.Fatalf("second prompt not shown: %q", tt.Texts())
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if want := []answer{{"s3cret", true}, {"", false}}; !slices.Equal(got, want) {
		t.Fatalf("answers %v, want %v", got, want)
	}
	if tt.HasText("Unlock SSH key") {
		t.Fatalf("prompt still open: %q", tt.Texts())
	}
}

func TestFrameHookSeesEachFrameStart(t *testing.T) {
	app := testApp(hub.HostView{})
	at := time.Unix(1000, 0)
	hwui.SetClock(app, func() time.Time { return at })
	var starts []time.Time
	app.SetFrameHook(func(start time.Time) { starts = append(starts, start) })
	tt := ui.NewTester(app.View, 1200, 800)
	tt.Frame()
	if len(starts) < 2 || !starts[0].Equal(at) {
		t.Fatalf("frame hook calls %v", starts)
	}
}

func TestPassphrasePromptCancelledByCaller(t *testing.T) {
	app := testApp(hub.HostView{})
	called := false
	cancel := app.AskPassphrase("/k/id_gone", func(string, bool) { called = true })
	tt := ui.NewTester(app.View, 1200, 800)
	if !tt.HasText("/k/id_gone") {
		t.Fatalf("prompt not shown: %q", tt.Texts())
	}
	cancel()
	cancel() // idempotent
	tt.Frame()
	if tt.HasText("/k/id_gone") || tt.HasText("Unlock SSH key") || called {
		t.Fatalf("cancelled prompt still shown (%v) or answered (%v)", tt.Texts(), called)
	}
}

func TestRetryButtonOnFailedHost(t *testing.T) {
	app := testApp(hub.HostView{Conn: hub.Conn{State: hub.Unauthorized}})
	tt := ui.NewTester(app.View, 1200, 800)
	if tt.HasText("Retry") {
		t.Fatal("Retry shown without a retry hook")
	}
	var got []string
	app.SetRetry(func(id string) { got = append(got, id) })
	tt.Frame()
	if err := tt.Click("Retry"); err != nil {
		t.Fatalf("%v: %q", err, tt.Texts())
	}
	if !slices.Equal(got, []string{"h1"}) {
		t.Fatalf("retried %v", got)
	}
	online := testApp(hub.HostView{Conn: hub.Conn{State: hub.Online}})
	online.SetRetry(func(string) {})
	if ui.NewTester(online.View, 1200, 800).HasText("Retry") {
		t.Fatal("Retry shown on an online host")
	}
}
