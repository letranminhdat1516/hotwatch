package collect

import (
	"context"
	"errors"
	"testing"

	"hotwatch/model"
)

func TestFake(t *testing.T) {
	f := &Fake{Snap: model.Snapshot{CPU: model.CPU{Total: 7}}, Procs: []model.Process{{PID: 1}}}
	ctx := context.Background()
	s, err := f.Sample(ctx)
	if err != nil || s.CPU.Total != 7 {
		t.Fatalf("sample: %v %v", s.CPU.Total, err)
	}
	p, err := f.Processes(ctx)
	if err != nil || len(p) != 1 || p[0].PID != 1 {
		t.Fatalf("procs: %v %v", p, err)
	}
	f.Err = errors.New("boom")
	if _, err := f.Sample(ctx); err == nil {
		t.Fatal("want sample error")
	}
	if _, err := f.Processes(ctx); err == nil {
		t.Fatal("want processes error")
	}
}
