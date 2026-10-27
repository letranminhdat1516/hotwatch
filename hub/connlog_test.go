package hub

import (
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"hotwatch/model"
	"hotwatch/source"
)

func logStates(l []LogEntry) []ConnState {
	var out []ConnState
	for _, e := range l {
		out = append(out, e.State)
	}
	return out
}

func TestConnLogRecordsTransitionsWithReasons(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newCounting()
		h, v := start(t, 2*time.Second, map[string]source.Source{"a": a})
		defer h.Stop()
		h.Select("a", "overview")
		advance(3 * time.Second) // online; the poll at 2s adds nothing
		setErr(a.Fake, &model.APIError{Code: "unauthorized", Message: "bad token"})
		advance(2 * time.Second)
		l := v.get("a").Log
		want := []ConnState{Connecting, Online, Unauthorized}
		if got := logStates(l); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("log states = %v, want %v", got, want)
		}
		if !strings.Contains(l[2].Detail, "bad token") {
			t.Fatalf("no reason on the failure: %+v", l[2])
		}
		if l[2].Time.Before(l[1].Time) {
			t.Fatalf("entries out of order: %+v", l)
		}
		setErr(a.Fake, nil)
		h.Retry("a")
		advance(time.Second)
		if got := logStates(v.get("a").Log); got[len(got)-1] != Online || got[len(got)-2] != Connecting {
			t.Fatalf("retry not logged: %v", got)
		}
	})
}

func TestConnLogKeepsNewestAndSurvivesReplaceSource(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newCounting()
		h, v := start(t, 2*time.Second, map[string]source.Source{"a": a})
		defer h.Stop()
		h.Select("a", "overview")
		advance(time.Second)
		for range maxLog {
			h.Retry("a") // Connecting then Online: two entries each
			advance(100 * time.Millisecond)
		}
		l := v.get("a").Log
		if len(l) != maxLog {
			t.Fatalf("len = %d, want %d", len(l), maxLog)
		}
		h.ReplaceSource("a", newCounting())
		synctest.Wait()
		advance(time.Second)
		l2 := v.get("a").Log
		if len(l2) != maxLog || l2[len(l2)-1].State != Online || l2[0].Time.Before(l[0].Time) {
			t.Fatalf("log after replace: len %d last %+v", len(l2), l2[len(l2)-1])
		}
	})
}

func TestConnLogRepeatsAFailureOnlyWhenItsReasonChanges(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newCounting()
		a.Err = &model.APIError{Code: "unavailable", Message: "down"}
		h, v := start(t, 2*time.Second, map[string]source.Source{"a": a})
		defer h.Stop()
		h.Select("a", "overview")
		advance(30 * time.Second) // settled in Offline, retrying every 16-30s
		n := len(v.get("a").Log)
		advance(120 * time.Second) // more retries, same reason
		if l := v.get("a").Log; len(l) != n || l[n-1].State != Offline {
			t.Fatalf("repeated failure logged again: %v", logStates(l))
		}
		setErr(a.Fake, &model.APIError{Code: "unavailable", Message: "refused"})
		advance(65 * time.Second)
		l := v.get("a").Log
		if len(l) != n+1 || !strings.Contains(l[n].Detail, "refused") {
			t.Fatalf("changed reason not logged: %+v", l)
		}
	})
}
