package pages

import (
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/ui/components"
	"hotwatch/ui/format"
)

// Disk builds the Disk page: the volumes, read and write throughput over the
// last five minutes and the processes doing the most I/O.
func Disk(c *ui.Context, v hub.HostView, now time.Time) {
	if waiting(c, v) {
		return
	}
	s := v.Snap
	pageColumn(c, func() {
		section(c, "Volumes", func() {
			if partial(s, "disks") || len(s.Disks) == 0 {
				note(c, format.Unknown)
				return
			}
			table(c, []column{{title: "Mount"}, {title: "File system", width: 10 * g()}, {title: "Used", width: 10 * g(), end: true},
				{title: "Total", width: 10 * g(), end: true}, {title: "Used %", width: 9 * g(), end: true},
				{title: "Read", width: 11 * g(), end: true}, {title: "Write", width: 11 * g(), end: true}},
				len(s.Disks), func(r, col int) {
					d := s.Disks[r]
					switch col {
					case 0:
						text(c, d.Mount, false)
					case 1:
						text(c, d.FS, false)
					case 2:
						text(c, format.Bytes(d.Used), true)
					case 3:
						text(c, format.Bytes(d.Total), true)
					case 4:
						text(c, format.Percent(ratio(d.Used, d.Total)), true)
					case 5:
						text(c, format.Rate(d.ReadBps), true)
					case 6:
						text(c, format.Rate(d.WriteBps), true)
					}
				})
		})
		components.DotTimeChart(c, "Disk throughput", "B/s", []components.Series{
			{Name: "Read", Pts: points(v, "disk.read")}, {Name: "Write", Pts: points(v, "disk.write")}}, now, metricTint(c, "disk"))
		procSection(c, v, "Top processes by disk I/O", func() {
			io := func(p model.Process) float64 {
				var sum float64
				for _, r := range []*float64{p.DiskReadBps, p.DiskWriteBps} {
					if r != nil {
						sum += *r
					}
				}
				return sum
			}
			top := topProcs(v.Procs, func(p model.Process) bool { return p.DiskReadBps != nil || p.DiskWriteBps != nil }, io)
			table(c, []column{{title: "Name"}, {title: "PID", width: 8 * g(), end: true},
				{title: "Read", width: 11 * g(), end: true}, {title: "Write", width: 11 * g(), end: true}},
				len(top), func(r, col int) {
					switch col {
					case 0:
						text(c, top[r].Name, false)
					case 1:
						text(c, pid(top[r]), true)
					case 2:
						text(c, format.Rate(top[r].DiskReadBps), true)
					case 3:
						text(c, format.Rate(top[r].DiskWriteBps), true)
					}
				})
		})
	})
}
