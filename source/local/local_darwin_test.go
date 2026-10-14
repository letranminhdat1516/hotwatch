//go:build darwin

package local

import (
	"context"
	"testing"
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
}
