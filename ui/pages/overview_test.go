package pages_test

import (
	"testing"

	"hotwatch/ui/pages"
)

func TestOverviewCardsShowSubLabelsStatsAndAnalytics(t *testing.T) {
	tt := render(pages.Overview, view(snap(), procs(), "darwin"))
	has(t, tt,
		"CPU", "Now", "User", "System", "Load 1m", "1.50",
		"Memory", "In use of 16.0 GB", "App", "Wired", "Compressed", "Warning",
		"Disk", "Reading", "Writing", "Written",
		"Network", "Downloading", "Uploading", "Received", "Sent",
		"Memory by Type", "Memory by App",
	)
}

func TestOverviewPartialSectionsShowUnknownNotZero(t *testing.T) {
	s := snap()
	s.Partial = []string{"cpu", "net"}
	tt := render(pages.Overview, view(s, nil, "linux"))
	has(t, tt, "Now", "Downloading", "—")
	lacks(t, tt, "1.50", "Memory by App") // no load figures; no processes
}
