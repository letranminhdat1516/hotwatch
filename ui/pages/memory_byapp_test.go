package pages

import (
	"testing"

	"hotwatch/model"
)

func TestByAppGroupsHelpersUnderTheirAppAndCountsFootprint(t *testing.T) {
	chrome := "/Applications/Google Chrome.app"
	helper := chrome + "/Contents/Frameworks/Google Chrome Framework.framework/Versions/141/Helpers/Google Chrome Helper (Renderer).app"
	ps := []model.Process{
		{PID: 1, Name: "Google Chrome", AppBundle: chrome, RSS: 300 << 20, Footprint: 400 << 20},
		{PID: 2, Name: "Google Chrome Helper (Renderer)", AppBundle: helper, RSS: 100 << 20, Footprint: 900 << 20},
		{PID: 3, Name: "Google Chrome Helper (Renderer)", AppBundle: helper, RSS: 100 << 20}, // footprint unreadable: RSS
		{PID: 4, Name: "node", RSS: 50 << 20, Footprint: 60 << 20},
	}
	apps := byApp(ps)
	if len(apps) != 2 || apps[0].name != "Google Chrome" || apps[0].procs != 3 || apps[0].rss != (400+900+100)<<20 || apps[0].bundle != chrome {
		t.Fatalf("apps %+v", apps)
	}
	if apps[1].name != "node" || apps[1].rss != 60<<20 {
		t.Fatalf("node %+v", apps[1])
	}
}
