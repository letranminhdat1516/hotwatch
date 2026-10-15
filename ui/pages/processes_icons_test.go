package pages_test

import (
	"fmt"
	"image"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/mac/appicon"
	"hotwatch/model"
	"hotwatch/ui/pages"
)

func TestProcessesShowInitialsUntilTheIconArrives(t *testing.T) {
	chrome := "/Applications/Google Chrome.app"
	ps := []model.Process{proc(1, "Google Chrome", 5), proc(2, "node", 1)}
	ps[0].AppBundle = chrome
	f := &appicon.Fake{Icons: map[string]*ui.Bitmap{chrome: ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 2, 2)))},
		Pending: map[string]bool{chrome: true}}
	pages.SetIconSource(f)
	defer pages.SetIconSource(nil)
	e := newEnv(ps)
	for _, want := range []string{"GC", "N"} {
		if !slices.Contains(e.tt.Texts(), want) {
			t.Fatalf("initials %q missing while the icon is pending: %q", want, e.tt.Texts())
		}
	}
	if _, ok := e.tt.Find("Google Chrome icon"); ok {
		t.Fatal("an image showed before the icon was delivered")
	}
	f.Deliver(chrome)
	e.tt.Frame()
	if r, ok := e.tt.Find("Google Chrome icon"); !ok || r.W != 20 || r.H != 20 {
		t.Fatalf("delivered icon: found %v %v, want a 20×20 image", r, ok)
	}
	if slices.Contains(e.tt.Texts(), "GC") {
		t.Fatalf("initials still shown after the icon arrived: %q", e.tt.Texts())
	}
	if !slices.Contains(e.tt.Texts(), "N") {
		t.Fatalf("a process without a bundle keeps its initials: %q", e.tt.Texts())
	}
}

func TestProcessesRenderFastWith3000RowsAndPendingIcons(t *testing.T) {
	ps := fixtureProcs(3000)
	for i := range ps {
		ps[i].AppBundle = fmt.Sprintf("/Applications/App %d.app", i)
	}
	pages.SetIconSource(&appicon.Fake{}) // never answers
	defer pages.SetIconSource(nil)
	e := newEnv(ps)
	start := time.Now()
	for i := 0; i < 20; i++ {
		e.tt.Frame()
	}
	if per := time.Since(start) / 20; per > 100*time.Millisecond {
		t.Fatalf("frame took %v", per)
	}
}
