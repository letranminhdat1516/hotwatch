// Package testenv tells tests what the machine they run on can show:
// Unix permission bits, real Mac hardware, an unshared CPU.
package testenv

import (
	"os"
	"runtime"
	"testing"
	"time"
)

// UnixModes reports whether file modes carry permissions (not on Windows,
// where access lists do and Go reports 0666/0777).
func UnixModes() bool { return runtime.GOOS != "windows" }

// RequireHardware skips a test that reads sensors, the battery, the GPU or
// energy counters when the Mac is a virtual machine (a CI runner), which has
// none of them.
func RequireHardware(t *testing.T) {
	t.Helper()
	if virtualMac() {
		t.Skip("virtual machine: no sensors, battery, GPU statistics or energy counters")
	}
}

// Budget is a time limit measured on real hardware, four times looser on a
// shared CI runner (CI is set), where neighbours share the CPU.
func Budget(d time.Duration) time.Duration {
	if os.Getenv("CI") != "" {
		return 4 * d
	}
	return d
}
