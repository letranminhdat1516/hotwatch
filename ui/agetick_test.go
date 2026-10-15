package ui_test

import (
	"testing"
	"time"

	"hotwatch/hub"
	hwui "hotwatch/ui"
)

func TestAgesTickOnlyWhileAHostIsBehind(t *testing.T) {
	ok := time.Now()
	for _, tc := range []struct {
		conn hub.Conn
		want bool
	}{
		{hub.Conn{State: hub.Online, LastOK: ok}, false}, // every poll redraws it anyway
		{hub.Conn{State: hub.Stale, LastOK: ok}, true},
		{hub.Conn{State: hub.Offline, LastOK: ok}, true},
		{hub.Conn{State: hub.Offline}, false}, // never connected: nothing ages
	} {
		if got := hwui.AgesTick(tc.conn); got != tc.want {
			t.Errorf("%s (lastOK %v): %v, want %v", tc.conn.State, !tc.conn.LastOK.IsZero(), got, tc.want)
		}
	}
}
