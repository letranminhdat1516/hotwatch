// Package format renders metric values as text. Unknown values are "—".
package format

import (
	"fmt"
	"time"
)

// Unknown is shown for a value that is not known; never 0.
const Unknown = "—"

var byteUnits = [...]string{"B", "KB", "MB", "GB", "TB", "PB"}

// Bytes formats n with a 1024 base: "12.3 GB".
func Bytes(n uint64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(byteUnits)-1 {
		v /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", v, byteUnits[i])
}

// Rate formats a bytes-per-second rate: "1.2 MB/s". nil (and a negative,
// impossible rate) is unknown.
func Rate(bps *float64) string {
	if bps == nil || *bps < 0 {
		return Unknown
	}
	if *bps < 1024 {
		return fmt.Sprintf("%.0f B/s", *bps)
	}
	return Bytes(uint64(*bps)) + "/s"
}

// Percent formats a percentage: "37.5 %". nil is unknown.
func Percent(p *float64) string {
	if p == nil {
		return Unknown
	}
	return fmt.Sprintf("%.1f %%", *p)
}

// Duration formats an uptime with its two largest units: "2d 2h", "3h 2m".
func Duration(d time.Duration) string {
	m := int(d / time.Minute)
	switch {
	case m >= 24*60:
		return fmt.Sprintf("%dd %dh", m/(24*60), m/60%24)
	case m >= 60:
		return fmt.Sprintf("%dh %dm", m/60, m%60)
	}
	return fmt.Sprintf("%dm", m)
}
