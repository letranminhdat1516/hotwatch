package pages_test

import (
	"testing"

	"hotwatch/model"
	"hotwatch/ui/pages"
)

func scopeProcs() []model.Process {
	return []model.Process{
		{PID: 1, Name: "Safari", AppBundle: "/Applications/Safari.app", User: "cicca", CPUPct: 3},
		{PID: 2, Name: "node", User: "cicca", CPUPct: 60, RSS: 3 << 30},
		{PID: 3, Name: "launchd", User: "root", CPUPct: 1},
		{PID: 4, Name: "mDNSResponder", User: "_mdnsresponder", CPUPct: 1},
	}
}

func TestProcessesScopesFilterTheList(t *testing.T) {
	pages.SetCurrentUser("cicca")
	e := newEnv(scopeProcs())
	has(t, e.tt, "Safari", "node", "launchd", "mDNSResponder", "4 processes")
	e.click(t, "Apps")
	has(t, e.tt, "Safari", "1 of 4 processes")
	lacks(t, e.tt, "node", "launchd", "mDNSResponder")
	e.click(t, "Mine")
	has(t, e.tt, "Safari", "node", "2 of 4 processes")
	lacks(t, e.tt, "launchd", "mDNSResponder")
	e.click(t, "System")
	has(t, e.tt, "launchd", "mDNSResponder", "2 of 4 processes")
	lacks(t, e.tt, "Safari", "node")
	e.click(t, "All")
	has(t, e.tt, "4 processes")
	lacks(t, e.tt, "4 of 4 processes")
}

func TestProcessesScopeCombinesWithSearch(t *testing.T) {
	pages.SetCurrentUser("cicca")
	e := newEnv(scopeProcs())
	e.click(t, "Mine")
	e.click(t, "Search")
	e.tt.Type("saf")
	has(t, e.tt, "Safari", "1 of 4 processes")
	lacks(t, e.tt, "node")
}
