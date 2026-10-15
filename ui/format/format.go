// Package format renders metric values as text. Unknown values are "—".
package format

import (
	"fmt"
	"math"
	"time"
)

// Unknown is shown for a value that is not known; never 0.
const Unknown = "—"

// Byte sizes step by 1024; bit rates follow networking's decimal units.
var (
	byteUnits = [...]string{"B", "KB", "MB", "GB", "TB", "PB"}
	bitUnits  = [...]string{"b", "kb", "Mb", "Gb", "Tb", "Pb"}
)

// bitRates makes Rate print bits per second ("1.2 Mb/s") instead of bytes.
// The UI sets it from Settings on the main thread, where values are
// formatted.
var bitRates bool

// SetBitRates chooses bits per second for Rate.
func SetBitRates(on bool) { bitRates = on }

// BitRates reports whether Rate prints bits per second.
func BitRates() bool { return bitRates }

// valid reports whether v is a usable measurement: finite and not negative.
func valid(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 }

// humanize picks the unit first and moves to the next one when the rounded
// value would print as a whole base. Values beyond the largest unit are
// unknown.
func humanize(v, base float64, units [6]string) string {
	i := 0
	for v >= base && i < len(units)-1 {
		v /= base
		i++
	}
	if v >= base {
		return Unknown // beyond the largest unit
	}
	if i == 0 {
		if math.Round(v) < base {
			return fmt.Sprintf("%.0f %s", v, units[0])
		}
		v /= base
		i++
	}
	if math.Round(v) >= base && i < len(units)-1 {
		v /= base
		i++
	}
	return sig3(v) + " " + units[i]
}

// sig3 prints v with three significant digits: "5.38", "11.2", "126"
// (whole numbers from 100, so "1023" stays whole).
func sig3(v float64) string {
	switch {
	case math.Round(v*100)/100 < 10:
		return fmt.Sprintf("%.2f", v)
	case math.Round(v*10)/10 < 100:
		return fmt.Sprintf("%.1f", v)
	}
	return fmt.Sprintf("%.0f", v)
}

// Bytes formats n with a 1024 base: "12.3 GB".
func Bytes(n uint64) string { return humanize(float64(n), 1024, byteUnits) }

// Rate formats a bytes-per-second rate: "1.2 MB/s", or "9.6 Mb/s" when
// SetBitRates is on. nil, negative and non-finite rates are unknown.
func Rate(bps *float64) string {
	if bps == nil || !valid(*bps) {
		return Unknown
	}
	h := humanize(*bps, 1024, byteUnits)
	if bitRates {
		h = humanize(*bps*8, 1000, bitUnits)
	}
	if h != Unknown {
		return h + "/s"
	}
	return Unknown
}

// Number formats a value with one decimal: "37.5". nil and non-finite are
// unknown.
func Number(p *float64) string {
	if p == nil || math.IsNaN(*p) || math.IsInf(*p, 0) {
		return Unknown
	}
	return fmt.Sprintf("%.1f", *p)
}

// Percent formats a percentage: "37.5 %". nil and non-finite are unknown.
func Percent(p *float64) string {
	if n := Number(p); n != Unknown {
		return n + " %"
	}
	return Unknown
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

// Ago formats how long ago something happened: "12s ago", "2m ago",
// "3h 2m ago". A negative age (clock skew) is "0s ago".
func Ago(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", max(int(d/time.Second), 0))
	}
	return Duration(d) + " ago"
}
