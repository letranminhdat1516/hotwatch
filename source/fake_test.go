package source

import (
	"context"
	"errors"
	"testing"
	"time"

	"hotwatch/model"
)

func TestFakeSequenceAndError(t *testing.T) {
	f := &Fake{Snaps: []model.Snapshot{{CPU: model.CPU{Total: 1}}, {CPU: model.CPU{Total: 2}}}}
	ctx := context.Background()
	for _, want := range []float64{1, 2, 2} {
		s, err := f.Snapshot(ctx)
		if err != nil || s.CPU.Total != want {
			t.Fatalf("got %v %v, want %v", s.CPU.Total, err, want)
		}
	}
	f.Err = errors.New("down")
	if _, err := f.Snapshot(ctx); err == nil {
		t.Fatal("want error")
	}
	var _ Source = f
}

func TestFakeRecordsAndLogs(t *testing.T) {
	f := &Fake{Logs: []model.LogLine{{Text: "a"}, {Text: "b"}}}
	ctx := context.Background()
	if err := f.Signal(ctx, 42, time.Time{}, model.SigTerm); err != nil {
		t.Fatal(err)
	}
	if err := f.ContainerAction(ctx, "c1", model.ActionRestart); err != nil {
		t.Fatal(err)
	}
	if f.Signals[0] != "42:TERM" || f.Actions[0] != "c1:restart" {
		t.Fatalf("records: %v %v", f.Signals, f.Actions)
	}
	ch, err := f.ContainerLogs(ctx, "c1", 10, false)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for l := range ch {
		got = append(got, l.Text)
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("logs: %v", got)
	}
}

func TestFakeDelayHonoursContext(t *testing.T) {
	f := &Fake{Delay: time.Hour}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := f.Processes(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
}
