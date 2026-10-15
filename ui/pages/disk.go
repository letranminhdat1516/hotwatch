package pages

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/ui/components"
	"hotwatch/ui/format"
	"hotwatch/ui/icons"
)

// readHex is the colour of disk reads; writes take the disk orange.
var readHex = "#64d2ff"

// Disk builds the Disk page (the owner's reference): reads and writes with
// their totals since startup over a stacked dot chart, a card per volume,
// the mounted disk images and the apps reading and writing the most.
func Disk(c *ui.Context, v hub.HostView, now time.Time) {
	if waiting(c, v) {
		return
	}
	s := v.Snap
	pageColumn(c, func() {
		diskActivityCard(c, v, now)
		if partial(s, "disks") || len(s.Disks) == 0 {
			section(c, "Volumes", func() { note(c, format.Unknown) })
			topDiskCard(c, v)
			return // the volumes are carried over or unreadable
		}
		var images []model.Volume
		for _, d := range model.DistinctVolumes(s.Disks) {
			if d.Image {
				images = append(images, d)
				continue
			}
			volumeCard(c, d)
		}
		if len(images) > 0 {
			imagesCard(c, images)
		}
		topDiskCard(c, v)
	})
}

// diskActivityCard is the combined rate, reads and writes now and since
// startup, and the chart: writes under reads.
func diskActivityCard(c *ui.Context, v hub.HostView, now time.Time) {
	s := v.Snap
	write, read := metricTint(c, "disk"), ui.Hex(readHex)
	disks := model.DistinctVolumes(s.Disks)
	for i := 0; i < len(disks); i++ {
		if disks[i].Image { // a disk image's traffic is the host disk's too
			disks = append(disks[:i], disks[i+1:]...)
			i--
		}
	}
	right := func() {
		legend(c, write, "Write")
		legend(c, read, "Read")
	}
	pageCard(c, icons.Disk, "Disk Activity", "disk", right, func() {
		r, okR := sumPtr(disks, func(d model.Volume) *float64 { return d.ReadBps })
		w, okW := sumPtr(disks, func(d model.Volume) *float64 { return d.WriteBps })
		val, unit := format.Unknown, ""
		if okR && okW {
			val, unit = splitUnit(format.Rate(ptr(r + w)))
		}
		figs := []fig{{"Reading", rateOr(r, okR), ""}, {"Writing", rateOr(w, okW), ""}}
		if t, ok := sumBytes(disks, func(d model.Volume) *uint64 { return d.ReadBytes }); ok {
			figs = append(figs, fig{"Read", format.Bytes(t), "since startup"})
		}
		if t, ok := sumBytes(disks, func(d model.Volume) *uint64 { return d.WriteBytes }); ok {
			figs = append(figs, fig{"Written", format.Bytes(t), "since startup"})
		}
		headline(c, "Reading and writing", val, unit, figs)
		from := now.Add(-components.ChartWindow())
		layers := []components.DotLayer{{Pts: points(v, "disk.read"), Color: read}}
		if rw := points(v, "disk.rw"); len(rw) > 0 {
			layers = append(layers, components.DotLayer{Pts: rw, Color: write})
		}
		components.StackedDotChart(c, layers, from, now, 0, write, 200).Margin(g(), 0, 0, 0)
	})
}

// volumeCard is one volume's free space with a used bar and its facts.
func volumeCard(c *ui.Context, d model.Volume) {
	tint := metricTint(c, "disk")
	name := d.Name
	if name == "" {
		name = d.Mount
	}
	right := func() {
		if name != d.Mount {
			ui.Text(c, d.Mount).FontSize(13).TextColor(c.Theme().TextMuted).SingleLine()
		}
	}
	pageCard(c, icons.Disk, name, "disk", right, func() {
		free := d.Total - min(d.Used, d.Total)
		val, unit := splitUnit(format.Bytes(free))
		headline(c, "Free of "+format.Bytes(d.Total), val, unit, nil)
		frac := 0.0
		if d.Total > 0 {
			frac = float64(d.Used) / float64(d.Total)
		}
		col := tint
		if frac >= 0.9 {
			col = c.Theme().Danger
		}
		levelBar(c, frac, col, 8).Margin(g(), 0, g(), 0)
		ui.Grid(c).Columns(4).GapX(2 * g()).Children(func() {
			cell := func(label, value string) {
				ui.Column(c).Gap(2).Children(func() {
					ui.Text(c, label).FontSize(13).TextColor(c.Theme().TextMuted).SingleLine()
					ui.Text(c, value).FontSize(16).FontWeight(650).SingleLine()
				})
			}
			cell("Used", format.Bytes(d.Used)+" · "+fmt.Sprintf("%.0f%%", frac*100))
			cell("Format", strings.ToUpper(d.FS))
			if d.Device != "" {
				cell("Device", strings.TrimPrefix(d.Device, "/dev/"))
			}
			cell("Activity", format.Rate(d.ReadBps)+" ↓  "+format.Rate(d.WriteBps)+" ↑")
		})
	})
}

// imagesCard lists the mounted disk images.
func imagesCard(c *ui.Context, imgs []model.Volume) {
	t := c.Theme()
	right := func() {
		ui.Text(c, fmt.Sprintf("%d mounted", len(imgs))).FontSize(13).TextColor(t.TextMuted).SingleLine()
	}
	pageCard(c, icons.Layers, "Disk Images", "", right, func() {
		for _, d := range imgs {
			name := d.Name
			if name == "" {
				name = d.Mount
			}
			kvRow(c, name, format.Bytes(d.Total)+" · "+d.FS)
		}
	})
}

// topDiskCard lists the apps reading and writing the most.
func topDiskCard(c *ui.Context, v hub.HostView) {
	if len(v.Procs) == 0 {
		note(c, noProcs)
		return
	}
	apps := groupByApp(v.Procs, func(p model.Process) (float64, bool) {
		var s float64
		ok := false
		for _, r := range []*float64{p.DiskReadBps, p.DiskWriteBps} {
			if r != nil {
				s += *r
				ok = true
			}
		}
		return s, ok
	})
	tint := metricTint(c, "disk")
	pageCard(c, icons.Disk, "Reading and Writing the Most", "disk", func() { showAll(c, "disk-show-all", "processes", tint) }, func() {
		if len(apps) == 0 {
			note(c, "No disk activity right now.")
			return
		}
		topApps(c, apps, 10, func(b float64) string { return format.Rate(&b) })
	})
}

// rateOr is the rate, or a dash when unknown.
func rateOr(v float64, ok bool) string {
	if !ok {
		return format.Unknown
	}
	return format.Rate(&v)
}

// sumBytes adds the known byte counts of vols; ok is false when none is known.
func sumBytes(vols []model.Volume, f func(model.Volume) *uint64) (uint64, bool) {
	var s uint64
	ok := false
	for _, v := range vols {
		if b := f(v); b != nil {
			s += *b
			ok = true
		}
	}
	return s, ok
}
