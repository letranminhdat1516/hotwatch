package pages

import (
	"cmp"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"hotwatch/model"
	"hotwatch/ui/components"
	"hotwatch/ui/icons"
)

// The detail pages share one card anatomy (UI-DESIGN.md "Card anatomy"):
// a tinted icon tile and title, optional figures on the right of the header,
// a muted label over a large headline with its unit, a row of stat cells,
// then a chart, bar or list.

// pageCard is a page card: the header (icon, title in the metric's colour,
// right-hand content) and the body.
func pageCard(c *ui.Context, icon *ui.SVG, title, key string, right func(), body func()) ui.Element {
	return components.Surface(c, ui.Column(c).Gap(g())).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(2 * g()).Children(func() {
			overviewSectionHeader(c, icon, title, key)
			ui.Spacer(c)
			if right != nil {
				right()
			}
		})
		body()
	})
}

// fig is one figure of a headline row: a label, the value and an optional
// muted line under it ("since startup").
type fig struct{ label, value, sub string }

// headline is the muted label over the large value and its unit, with the
// stats beside it, bottoms aligned, as the reference lays them out.
func headline(c *ui.Context, label, value, unit string, stats []fig) {
	t := c.Theme()
	ui.Row(c).Gap(5*g()).AlignItems(ui.Start).Wrap().Margin(g()/2, 0, 0, 0).Children(func() {
		ui.Column(c).Gap(0).Children(func() {
			if label != "" {
				ui.Text(c, label).FontSize(14).TextColor(t.TextMuted).SingleLine()
			}
			spans := []ui.Span{{Text: value, Size: 44, Weight: 700}}
			if unit != "" && value != "—" {
				spans = append(spans, ui.Span{Text: " " + unit, Size: 18, Weight: 500, Color: t.TextMuted})
			}
			ui.RichText(c, spans...).SingleLine()
		})
		for _, s := range stats {
			top := float32(28) // the figures start level with the headline's digits
			if label == "" {
				top = 6
			}
			ui.Column(c).Gap(2).MinWidth(14*g()).Padding(top, 0, 0, 0).Children(func() {
				ui.Text(c, s.label).FontSize(13).TextColor(t.TextMuted).SingleLine()
				ui.Text(c, s.value).FontSize(16).FontWeight(650).SingleLine()
				if s.sub != "" {
					ui.Text(c, s.sub).FontSize(12).TextColor(t.TextMuted.Alpha(0.75)).SingleLine()
				}
			})
		}
	})
}

// statGrid lays stats out in n columns, for cards narrower than a headline row.
func statGrid(c *ui.Context, n int, stats []fig) {
	t := c.Theme()
	ui.Grid(c).Columns(n).GapX(2 * g()).GapY(2 * g()).Children(func() {
		for _, s := range stats {
			ui.Column(c).Gap(2).Children(func() {
				ui.Text(c, s.label).FontSize(13).TextColor(t.TextMuted).SingleLine()
				ui.Text(c, s.value).FontSize(16).FontWeight(650).SingleLine()
				if s.sub != "" {
					ui.Text(c, s.sub).FontSize(12).TextColor(t.TextMuted.Alpha(0.75)).SingleLine()
				}
			})
		}
	})
}

// part is one coloured share of a segment bar.
type part struct {
	value float64
	color ui.Color
}

// segBar is a thin rounded bar of parts out of total over a faint track in
// track colour: memory by kind, a volume's used space.
func segBar(c *ui.Context, parts []part, total float64, track ui.Color, height float32) ui.Element {
	return ui.Box(c).Height(height).Radius(height / 2).Background(track).Draw(func(p *ui.Painter, r ui.Rect) {
		if total <= 0 {
			return
		}
		x := r.X
		gap := float32(2)
		for i, pt := range parts {
			w := r.W * float32(min(max(pt.value/total, 0), 1))
			if w <= 0 {
				continue
			}
			if x+w > r.X+r.W {
				w = r.X + r.W - x
			}
			draw := w
			if i < len(parts)-1 && draw > gap*2 {
				draw -= gap // a hairline between parts, as the reference draws them
			}
			p.Fill(ui.Rect{X: x, Y: r.Y, W: max(draw, height), H: r.H}, pt.color, height/2)
			x += w
		}
	})
}

// levelBar is a single-colour bar filled to frac (0–1).
func levelBar(c *ui.Context, frac float64, color ui.Color, height float32) ui.Element {
	return segBar(c, []part{{value: frac, color: color}}, 1, color.Alpha(0.18), height)
}

// labelBar is a muted label, a level bar and the value at the right.
func labelBar(c *ui.Context, label, sub string, frac float64, color ui.Color, value string) {
	t := c.Theme()
	ui.Row(c).Gap(2 * g()).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Width(16 * g()).Gap(0).Children(func() {
			ui.Text(c, label).FontSize(14).TextColor(t.TextMuted).SingleLine()
			if sub != "" {
				ui.Text(c, sub).FontSize(12).TextColor(t.TextMuted.Alpha(0.75)).SingleLine()
			}
		})
		levelBar(c, frac, color, 6).Grow(1)
		ui.Text(c, value).FontSize(14).FontWeight(650).Width(9 * g()).TextAlign(ui.End).SingleLine()
	})
}

// kvRow is a muted label left and a value right, a fact table's row.
func kvRow(c *ui.Context, label, value string) {
	t := c.Theme()
	ui.Row(c).Gap(2 * g()).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(14).TextColor(t.TextMuted).SingleLine().Shrink(0)
		ui.Spacer(c)
		ui.Text(c, value).FontSize(14).FontWeight(600).SingleLine().TextAlign(ui.End)
	})
}

// statusPill is a tinted badge with a check mark when all is well:
// "✓ Normal".
func statusPill(c *ui.Context, text, tone string) {
	t := c.Theme()
	col := t.Success
	mark := "✓ "
	switch tone {
	case "warn":
		col, mark = t.Warning, "! "
	case "bad":
		col, mark = t.Danger, "! "
	}
	ui.Row(c).Padding(4, 10).Radius(12).Background(col.Alpha(0.14)).Children(func() {
		ui.Text(c, mark+text).FontSize(13).FontWeight(650).TextColor(col).SingleLine()
	})
}

// showAll is the "Show All ›" link of a top list, in the metric's colour;
// it opens page.
func showAll(c *ui.Context, key, page string, tint ui.Color) {
	if navigate == nil {
		return
	}
	link := ui.Row(c).Key(key).Gap(4).AlignItems(ui.Center).Cursor(ui.CursorPointer).Label("Show All")
	if link.Clicked() {
		navigate(page)
	}
	link.Children(func() {
		ui.Text(c, "Show All").FontSize(14).FontWeight(600).TextColor(tint).SingleLine()
		components.Glyph(c, icons.ChevronRight, 11, tint)
	})
}

// appTotal is one app's share of a per-process figure: its processes
// (helpers count toward their app), how many there are, and the sum.
type appTotal struct {
	name, bundle string
	user         string // the first process's user: a system process shows the gear
	count        int
	value        float64
}

// groupByApp sums value over the processes of each app (by outermost
// bundle; plain executables by name), keeps the groups above zero and
// returns them largest first.
func groupByApp(ps []model.Process, value func(model.Process) (float64, bool)) []appTotal {
	idx := map[string]int{}
	var out []appTotal
	for _, p := range ps {
		v, ok := value(p)
		if !ok || v <= 0 {
			continue
		}
		key, name, bundle := "exe:"+p.Name, p.Name, ""
		if p.AppBundle != "" {
			bundle = outerBundle(p.AppBundle)
			key, name = bundle, strings.TrimSuffix(path.Base(bundle), ".app")
		}
		i, seen := idx[key]
		if !seen {
			i = len(out)
			idx[key] = i
			out = append(out, appTotal{name: name, bundle: bundle, user: p.User})
		}
		out[i].count++
		out[i].value += v
	}
	slices.SortStableFunc(out, func(a, b appTotal) int { return cmp.Compare(b.value, a.value) })
	return out
}

// topApps is a two-column list of the apps using the most of something:
// icon, name, process count when more than one, and the figure.
func topApps(c *ui.Context, apps []appTotal, n int, value func(float64) string) {
	t := c.Theme()
	apps = apps[:min(len(apps), n)]
	ui.Grid(c).Columns(2).GapX(6*g()).GapY(g()+4).Margin(g(), 0, 0, 0).Children(func() {
		for i, a := range apps {
			ui.Row(c).Key(fmt.Sprintf("app-%d", i)).Gap(g() + 4).AlignItems(ui.Center).Children(func() {
				userIcon(c, a.name, a.bundle, a.user)
				ui.Text(c, a.name).FontSize(14).SingleLine().Shrink(1)
				if a.count > 1 {
					ui.Text(c, fmt.Sprint(a.count)).FontSize(13).TextColor(t.TextMuted).SingleLine()
				}
				ui.Spacer(c)
				ui.Text(c, value(a.value)).FontSize(14).FontWeight(650).SingleLine()
			})
		}
	})
}

// sumPtr adds the known values of xs; ok is false when none is known.
func sumPtr[T any](xs []T, f func(T) *float64) (float64, bool) {
	var s float64
	ok := false
	for _, x := range xs {
		if v := f(x); v != nil {
			s += *v
			ok = true
		}
	}
	return s, ok
}
