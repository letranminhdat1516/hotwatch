package model

import "time"

// Rate returns (cur-prev)/dt in units per second, or nil when the value is
// unknowable: non-positive dt, a counter reset (cur < prev) or a gap longer
// than 5 minutes (sleep).
func Rate(prev, cur uint64, dt time.Duration) *float64 {
	if dt <= 0 || dt > 5*time.Minute || cur < prev {
		return nil
	}
	r := float64(cur-prev) / dt.Seconds()
	return &r
}
