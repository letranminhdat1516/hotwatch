// Package format renders metric values as text. Unknown values are "—".
package format

import (
	"fmt"
	"math"
	"time"
)

// Unknown is shown for a value that is not known; never 0.
const Unknown = "—"

var byteUnits = [...]string{"B", "KB", "MB", "GB", "TB", "PB"}

// valid reports whether v is a usable measurement: finite and not negative.
func valid(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 }

// humanize picks the unit first and moves to the next one when the rounded
// value would print as 1024.
func humanize(v float64) string {
	i := 0
	for v >= 1024 && i < len(byteUnits)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		if math.Round(v) < 1024 {
			return fmt.Sprintf("%.0f B", v)
		}
		v /= 1024
		i++
	}
	if math.Round(v*10)/10 >= 1024 && i < len(byteUnits)-1 {
		v /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", v, byteUnits[i])
}

// Bytes formats n with a 1024 base: "12.3 GB".
func Bytes(n uint64) string { return humanize(float64(n)) }

// Rate formats a bytes-per-second rate: "1.2 MB/s". nil, negative and
// non-finite rates are unknown.
func Rate(bps *float64) string {
	if bps == nil || !valid(*bps) {
		return Unknown
	}
	return humanize(*bps) + "/s"
}

// Percent formats a percentage: "37.5 %". nil and non-finite are unknown.
func Percent(p *float64) string {
	if p == nil || math.IsNaN(*p) || math.IsInf(*p, 0) {
		return Unknown
	}
	return fmt.Sprintf("%.1f %%", *p)
}

// Load formats the three load averages; unknown if any is invalid.
func Load(l [3]float64) string {
	for _, v := range l {
		if !valid(v) {
			return Unknown
		}
	}
	return fmt.Sprintf("%.2f / %.2f / %.2f", l[0], l[1], l[2])
}

// Duration formats an uptime with its two largest units: "2d 2h", "3h 2m".
// A negative duration is unknown.
func Duration(d time.Duration) string {
	if d < 0 {
		return Unknown
	}
	m := int(d / time.Minute)
	switch {
	case m >= 24*60:
		return fmt.Sprintf("%dd %dh", m/(24*60), m/60%24)
	case m >= 60:
		return fmt.Sprintf("%dh %dm", m/60, m%60)
	}
	return fmt.Sprintf("%dm", m)
}
