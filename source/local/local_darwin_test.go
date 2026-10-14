//go:build darwin

package local

import (
	"context"
	"errors"
	"testing"
	"time"

	"hotwatch/model"
)

func TestRealMac(t *testing.T) {
	l := New()
	ctx := context.Background()
	info, err := l.Info(ctx)
	if err != nil || info.Hostname == "" || info.Kernel == "" || info.Arch == "" || info.OS != "darwin" {
		t.Fatalf("info %+v err %v", info, err)
	}
	for i := 0; i < 2; i++ {
		s, err := l.Snapshot(ctx)
		if err != nil || s.Memory.Total == 0 {
			t.Fatalf("snapshot %+v err %v", s, err)
		}
	}
	var ae *model.APIError
	if err := l.Signal(ctx, 2147483000, time.Now(), model.SigTerm); !errors.As(err, &ae) || ae.Code != "not_found" {
		t.Fatalf("missing pid: %v", err)
	}
}
