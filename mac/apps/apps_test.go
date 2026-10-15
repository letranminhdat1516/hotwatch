package apps

import (
	"context"
	"slices"
	"testing"
)

func TestFakeListsSizesAndRecords(t *testing.T) {
	f := &Fake{List: []App{{Name: "Safari", Bundle: "/Applications/Safari.app"}, {Name: "Xcode", Bundle: "/Applications/Xcode.app"}},
		Sizes: map[string]uint64{"/Applications/Xcode.app": 14 << 30}}
	ctx := context.Background()
	got, err := f.Apps(ctx)
	if err != nil || len(got) != 2 || got[0].Size != 0 {
		t.Fatalf("Apps = %+v %v: the list must come back fast, without sizes", got, err)
	}
	if n, err := f.Size(ctx, "/Applications/Xcode.app"); err != nil || n != 14<<30 {
		t.Fatalf("Size = %d %v", n, err)
	}
	if _, err := f.Size(ctx, "/Applications/Nope.app"); err == nil {
		t.Fatal("unknown bundle must fail")
	}
	if err := f.Uninstall(ctx, "/Applications/Xcode.app"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.SizeCalls, []string{"/Applications/Xcode.app", "/Applications/Nope.app"}) || !slices.Equal(f.Removed, []string{"/Applications/Xcode.app"}) {
		t.Fatalf("calls %v removed %v", f.SizeCalls, f.Removed)
	}
	var _ Scanner = f
}
