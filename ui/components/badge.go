package components

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// Tone is the colour family of a state.
type Tone int

const (
	Neutral   Tone = iota // grey: read-only, connecting, created
	Good                  // green: online, running
	Attention             // amber: stale, restarting, paused, needs confirmation
	Bad                   // red: offline, error, exited
)

type stateLook struct {
	tone  Tone
	label string
}

// states maps lower-cased connection, container, action, memory pressure
// interface and power states to their
// tone and plain label.
var states = map[string]stateLook{
	"online":          {Good, "Online"},
	"running":         {Good, "Running"},
	"stale":           {Attention, "Stale"},
	"restarting":      {Attention, "Restarting"},
	"needs-confirm":   {Attention, "Needs confirmation"},
	"hostkeyunknown":  {Attention, "Verify host key"},
	"offline":         {Bad, "Offline"},
	"error":           {Bad, "Error"},
	"exited":          {Bad, "Exited"},
	"dead":            {Bad, "Dead"},
	"unauthorized":    {Bad, "Not authorized"},
	"incompatible":    {Bad, "Incompatible agent"},
	"hostkeymismatch": {Bad, "Host key changed"},
	"paused":          {Attention, "Paused"},
	"read-only":       {Neutral, "Read-only"},
	"connecting":      {Neutral, "Connecting"},
	"created":         {Neutral, "Created"},
	"normal":          {Good, "Normal"},
	"warn":            {Attention, "Warning"},
	"critical":        {Bad, "Critical"},
	"up":              {Good, "Up"},
	"down":            {Neutral, "Down"},
	"charging":        {Good, "Charging"},
	"on battery":      {Attention, "On battery"},
	"on ac":           {Neutral, "On AC"},
	"nominal":         {Good, "Normal"},
	"fair":            {Attention, "Fair"},
	"serious":         {Bad, "Serious"},
}

func look(state string) stateLook {
	if l, ok := states[strings.ToLower(state)]; ok {
		return l
	}
	label := state
	if label != "" {
		label = strings.ToUpper(label[:1]) + label[1:]
	}
	return stateLook{Neutral, label}
}

// ToneColor is the theme token of a tone.
func ToneColor(t *ui.Theme, tone Tone) ui.Color {
	switch tone {
	case Good:
		return t.Success
	case Attention:
		return t.Warning
	case Bad:
		return t.Danger
	}
	return t.TextMuted
}

// StateColor is the theme token a state's badge uses: green online/running,
// amber stale/needs confirmation, red offline/error, grey read-only/created.
func StateColor(t *ui.Theme, state string) ui.Color { return ToneColor(t, look(state).tone) }

// Badge shows a state as a tinted pill with a dot and a plain label.
func Badge(c *ui.Context, state string) ui.Element {
	l := look(state)
	col := ToneColor(c.Theme(), l.tone)
	return ui.Row(c).Gap(6).Padding(2, Grid+2, 2, Grid).Radius(999).Background(col.Alpha(0.14)).Shrink(0).Children(func() {
		ui.Box(c).Size(6, 6).Radius(3).Background(col)
		ui.Text(c, l.label).FontSize(12).FontWeight(600).TextColor(col).NoWrap()
	})
}
