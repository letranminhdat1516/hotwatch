package appicon

import (
	"image"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestFakePendingThenDelivered(t *testing.T) {
	f := &Fake{Icons: map[string]*ui.Bitmap{"/Applications/A.app": ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 2, 2)))},
		Pending: map[string]bool{"/Applications/A.app": true}}
	if b, ok := f.Icon("/Applications/A.app"); ok || b != nil {
		t.Fatal("pending icon answered")
	}
	f.Deliver("/Applications/A.app")
	if _, ok := f.Icon("/Applications/A.app"); !ok {
		t.Fatal("delivered icon missing")
	}
	if _, ok := f.Icon("/nope.app"); ok {
		t.Fatal("unknown bundle answered")
	}
	if !slices.Equal(f.Asked, []string{"/Applications/A.app", "/Applications/A.app", "/nope.app"}) {
		t.Fatalf("asked %v", f.Asked)
	}
	var _ Source = f
}

func TestNoneNeverAnswers(t *testing.T) {
	if b, ok := (None{}).Icon("/Applications/A.app"); ok || b != nil {
		t.Fatal("None answered")
	}
}

func TestBundlePathRefusesUntrustedPaths(t *testing.T) {
	for p, want := range map[string]bool{
		"/Applications/Safari.app":           true,
		"/Users/x/Applications/A.app":        true,
		"Safari.app":                         false,
		"/etc/passwd":                        false,
		"/Applications/../../etc/x.app":      false,
		"/Applications/A.app/Contents/MacOS": false,
		"":                                   false,
	} {
		if got := BundlePath(p); got != want {
			t.Errorf("BundlePath(%q) = %v, want %v", p, got, want)
		}
	}
}
