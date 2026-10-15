package pages_test

import (
	"testing"
	"time"

	"hotwatch/model"
	"hotwatch/ui/pages"
)

// overviewMac is the reference's Overview: a charging MacBook with die
// temperatures, a battery's health, and processes with power.
func overviewMac() *model.Snapshot {
	s := snap()
	s.CPU.User, s.CPU.System, s.CPU.TempC = fp(18), fp(17), fp(48)
	s.CPU.Total = 34.4
	s.Memory = model.Memory{Total: 24 << 30, Used: 20 << 30, App: 5509 << 20, Wired: 3553 << 20, Compressed: 11468 << 20,
		Cached: 3205 << 20, Pressure: "warn"}
	health, cycles := 96.3, 92
	s.Battery = &model.Battery{ChargePct: 79, Charging: true, OnAC: true, TimeRemaining: dur(63 * time.Minute),
		PowerW: fp(89.6), HealthPct: &health, Cycles: &cycles}
	return s
}

func powerProcs() []model.Process {
	ps := procs()
	ps[0].PowerMW, ps[1].PowerMW, ps[3].PowerMW = fp(437), fp(358), fp(83)
	return ps
}

func TestOverviewCardsFollowTheReference(t *testing.T) {
	tt := render(pages.Overview, view(overviewMac(), powerProcs(), "darwin"))
	has(t, tt,
		"CPU", "Now", "34 %", "User", "18%", "System", "17%", "Temperature", "48 °C",
		"Memory", "In use of 24.0 GB", "20.0 GB", "Elevated", "App", "5.38 GB", "Wired", "3.47 GB", "Compressed", "11.2 GB",
		"Disk", "Reading", "Writing", "Written",
		"Network", "Downloading", "Uploading", "Received", "Sent",
		"Battery", "Charging", "79 %", "Until Full", "1h 3m", "Power Draw", "89.6 W", "Health", "96%",
	)
	lacks(t, tt, "Load 1m", "Remaining", "34.4 %")
}

func TestOverviewAnalyticsRow(t *testing.T) {
	tt := render(pages.Overview, view(overviewMac(), powerProcs(), "darwin"))
	has(t, tt,
		"Memory by Type", "83%", "in use", "App", "Wired", "Compressed", "Cached", "3.13 GB", "Free", "4.00 GB",
		"Memory by App", "Energy by App", "878 mW in all", "python3", "437 mW", "postgres", "358 mW",
	)
}

func TestOverviewWithoutPowerHasNoEnergyByApp(t *testing.T) {
	tt := render(pages.Overview, view(overviewMac(), procs(), "darwin"))
	lacks(t, tt, "Energy by App")
}

func TestOverviewOnALinuxHostKeepsLoadAndRemaining(t *testing.T) {
	tt := render(pages.Overview, view(snap(), procs(), "linux"))
	has(t, tt, "CPU", "41 %", "Load 1m", "1.50")
	lacks(t, tt, "Temperature", "Battery")
}

func TestOverviewPartialSectionsShowUnknownNotZero(t *testing.T) {
	s := snap()
	s.Partial = []string{"cpu", "net"}
	tt := render(pages.Overview, view(s, nil, "linux"))
	has(t, tt, "Now", "Downloading", "—")
	lacks(t, tt, "1.50", "Memory by App") // no load figures; no processes
}
