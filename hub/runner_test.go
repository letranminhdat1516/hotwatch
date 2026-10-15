package hub

import "testing"

func TestFakeRunner(t *testing.T) {
	r := &FakeRunner{}
	a := r.Start(Action{Kind: "signal", Verb: "TERM"})
	b := r.Start(Action{Kind: "container", Verb: "restart"})
	if a != 1 || b != 2 {
		t.Fatalf("ids %d %d", a, b)
	}
	if len(r.Started) != 2 || r.Get(b).Verb != "restart" || r.Get(b).ID != b {
		t.Fatalf("got %+v", r.Started)
	}
	var _ Runner = r
}
