//go:build !linux && !windows

package agent

// openPin is nil off Linux: signalling uses kill(2) with a small check/kill
// race.
var openPin func(pid int) (pinned, error)
