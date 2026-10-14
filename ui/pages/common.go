package pages

import (
	"fmt"
	"slices"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/ui/components"
	"hotwatch/ui/format"
)

// topN is how many processes a "top" table lists.
const topN = 10

// noProcs is the line shown where the process list has not been fetched:
// the hub fetches processes only while the Processes page is selected.
const noProcs = "Process details are on the Processes page"

func g() float32 { return float32(components.Grid) }

// column is one table column: its header, width (0 shares the remaining
// room) and whether it is right-aligned, as numbers are.
type column struct {
	title string
	width float32
	end   bool
}

// section is a card with a small title around body.
func section(c *ui.Context, title string, body func()) ui.Element {
	return components.Surface(c, ui.Column(c).Gap(g()+4)).Children(func() {
		ui.Text(c, title).FontSize(14).FontWeight(600).NoWrap()
		body()
	})
}

// procSection is a section of the process list's facts; without a process
// list it is the muted pointer to the Processes page.
func procSection(c *ui.Context, v hub.HostView, title string, body func()) {
	if len(v.Procs) == 0 {
		note(c, noProcs)
		return
	}
	section(c, title, body)
}

// note is a muted one-line explanation.
func note(c *ui.Context, text string) {
	ui.Text(c, text).FontSize(12).TextColor(c.Theme().TextMuted)
}

// table lays out n rows under a header. cell draws row r, column col.
func table(c *ui.Context, cols []column, n int, cell func(row, col int)) {
	t := c.Theme()
	tracks := make([]ui.Track, len(cols))
	for i, col := range cols {
		tracks[i] = ui.Fr(1)
		if col.width > 0 {
			tracks[i] = ui.Fixed(col.width)
		}
	}
	ui.Grid(c).ColumnTracks(tracks...).GapX(2 * g()).GapY(g()).Children(func() {
		for _, col := range cols {
			h := ui.Text(c, col.title).FontSize(11).FontWeight(600).TextColor(t.TextMuted).SingleLine()
			if col.end {
				h.TextAlign(ui.End)
			}
		}
		for r := 0; r < n; r++ {
			for i := range cols {
				cell(r, i)
			}
		}
	})
}

// text is a table cell.
func text(c *ui.Context, s string, end bool) ui.Element {
	e := ui.Text(c, s).FontSize(13).SingleLine()
	if end {
		e.TextAlign(ui.End)
	}
	return e
}

// points is the History series of name, nil when the host has no history.
func points(v hub.HostView, name string) []hub.Point {
	if v.Hist == nil {
		return nil
	}
	return v.Hist.Series(name)
}

func pageColumn(c *ui.Context, body func()) {
	ui.Column(c).Gap(2 * g()).Children(body)
}

// topProcs returns up to topN processes passing keep, largest by key first;
// ties keep the process list's order.
func topProcs(ps []model.Process, keep func(model.Process) bool, key func(model.Process) float64) []model.Process {
	var out []model.Process
	for _, p := range ps {
		if keep(p) {
			out = append(out, p)
		}
	}
	slices.SortStableFunc(out, func(a, b model.Process) int {
		switch ka, kb := key(a), key(b); {
		case ka > kb:
			return -1
		case ka < kb:
			return 1
		}
		return 0
	})
	if len(out) > topN {
		out = out[:topN]
	}
	return out
}

func pid(p model.Process) string { return fmt.Sprint(p.PID) }

func pct(v float64) string { return format.Percent(&v) }

// waiting reports (and draws) the empty state of a host with no snapshot.
func waiting(c *ui.Context, v hub.HostView) bool {
	if v.Snap != nil {
		return false
	}
	components.EmptyState(c, "Waiting for data…")
	return true
}
