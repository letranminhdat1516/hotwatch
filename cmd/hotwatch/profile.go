package main

import (
	"log/slog"
	"os"
	"runtime/pprof"
	"time"
)

// profileFor is how long HOTWATCH_CPUPROFILE records.
const profileFor = 60 * time.Second

// startProfile writes a CPU profile of the first profileFor to the file
// named by HOTWATCH_CPUPROFILE, for measuring the app's idle cost (R17).
// Without the variable it does nothing.
func startProfile(log *slog.Logger) {
	path := os.Getenv("HOTWATCH_CPUPROFILE")
	if path == "" {
		return
	}
	f, err := os.Create(path)
	if err != nil {
		log.Warn("cpuprofile", "err", err)
		return
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		log.Warn("cpuprofile", "err", err)
		f.Close()
		return
	}
	time.AfterFunc(profileFor, func() {
		pprof.StopCPUProfile()
		f.Close()
		log.Info("cpuprofile.written", "path", path)
	})
}
