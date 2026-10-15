package pages

import (
	"runtime"
	"runtime/debug"
	"slices"

	"github.com/egoist/mygo/ui"

	"hotwatch/store"
)

var (
	intervalLabels = []string{"0.5 s", "1 s", "2 s", "5 s"}
	intervalMs     = []int{500, 1000, 2000, 5000}
	unitLabels     = []string{"Bytes/s", "Bits/s"}
	nookLabels     = []string{"Show", "Hide"}
	lookLabels     = []string{"System", "Light", "Dark"}
	lookValues     = []string{"", "light", "dark"}
)

// nearestInterval is the index of the option closest to ms.
func nearestInterval(ms int) int {
	best := 0
	for i, v := range intervalMs {
		if abs(v-ms) < abs(intervalMs[best]-ms) {
			best = i
		}
	}
	return best
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// SettingsState is the Settings page's bound widget state. The App keeps
// one between frames: a segmented control writes its choice to the pointer
// it was built with, applied before the next frame, so a per-frame local
// would never see it.
type SettingsState struct {
	interval, units, nook int
	look                  int
	syncedMs              int
	syncedUnits           string
	syncedNookOff         bool
	syncedLook            string
	init                  bool
}

// sync points the controls at s when the settings changed elsewhere.
func (st *SettingsState) sync(s store.Settings) {
	if st.init && st.syncedMs == s.IntervalMs && st.syncedUnits == s.NetUnits && st.syncedNookOff == s.NookOff &&
		st.syncedLook == s.Appearance {
		return
	}
	st.look = max(slices.Index(lookValues, s.Appearance), 0)
	st.syncedLook = s.Appearance
	st.nook = 0
	if s.NookOff {
		st.nook = 1
	}
	st.syncedNookOff = s.NookOff
	st.interval = nearestInterval(s.IntervalMs)
	st.units = 0
	if s.NetUnits == "bits" {
		st.units = 1
	}
	st.syncedMs, st.syncedUnits, st.init = s.IntervalMs, s.NetUnits, true
}

// Settings builds the Settings page (UI-DESIGN.md, "Settings"): the poll
// interval, network units, the nook, the keyboard shortcuts and the build. set is
// called with the changed settings when the user picks an option; the
// caller persists and applies them. Options without an implementation
// behind them (language, menu bar) are not offered.
func Settings(c *ui.Context, st *SettingsState, s store.Settings, set func(store.Settings)) {
	t := c.Theme()
	st.sync(s)
	pageColumn(c, func() {
		section(c, "Appearance", func() {
			ui.Text(c, "Light or dark").FontSize(12).TextColor(t.TextMuted)
			ui.Row(c).Children(func() { ui.Segmented(c, &st.look, lookLabels...) })
			if v := lookValues[st.look]; v != s.Appearance {
				s.Appearance, st.syncedLook = v, v
				set(s)
				c.Invalidate()
			}
			note(c, "System follows macOS. The button beside Pause in the header switches it too.")
		})
		section(c, "Updates", func() {
			ui.Text(c, "Update every").FontSize(12).TextColor(t.TextMuted)
			ui.Row(c).Children(func() { ui.Segmented(c, &st.interval, intervalLabels...) })
			if ms := intervalMs[st.interval]; ms != s.IntervalMs && st.interval != nearestInterval(s.IntervalMs) {
				s.IntervalMs, st.syncedMs = ms, ms
				set(s)
				c.Invalidate() // redraw now: the sidebar already drew this frame
			}
			note(c, "How often the selected host is polled; the other hosts update every 15 s.")
		})
		section(c, "Units", func() {
			ui.Text(c, "Network speeds in").FontSize(12).TextColor(t.TextMuted)
			ui.Row(c).Children(func() { ui.Segmented(c, &st.units, unitLabels...) })
			units := ""
			if st.units == 1 {
				units = "bits"
			}
			if units != s.NetUnits {
				s.NetUnits, st.syncedUnits = units, units
				set(s)
				c.Invalidate() // redraw now: the sidebar already drew this frame
			}
		})
		section(c, "Nook", func() {
			ui.Text(c, "Notch bar and island").FontSize(12).TextColor(t.TextMuted)
			ui.Row(c).Children(func() { ui.Segmented(c, &st.nook, nookLabels...) })
			if off := st.nook == 1; off != s.NookOff {
				s.NookOff, st.syncedNookOff = off, off
				set(s)
				c.Invalidate()
			}
			note(c, "A thin CPU and memory bar under the notch; point at the notch for the island. Hidden on Macs without a notch.")
		})
		section(c, "Keyboard", func() {
			for _, line := range []string{
				"⌘1 – ⌘9   switch pages",
				"⌘,   open Settings",
				"⌥⌘↑ / ⌥⌘↓   previous / next host",
				"⌘F   search the processes",
			} {
				ui.Text(c, line).FontSize(13)
			}
		})
		section(c, "About", func() {
			kv(c, "Hotwatch", appVersion())
			kv(c, "Go", runtime.Version())
		})
	})
}

// appVersion is the main module's version from the build info; "dev" for a
// build from a working tree.
func appVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}
