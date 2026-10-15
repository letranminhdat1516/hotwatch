//go:build darwin

package darwin

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// netBytes is a process's network byte counters as nettop reports them.
type netBytes struct{ in, out uint64 }

// runNettop returns one nettop sample of every process's network bytes
// (argv, no shell, 2 s): CSV lines "name.pid,bytes_in,bytes_out,".
func runNettop(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "nettop", "-P", "-L", "1", "-n", "-x", "-J", "bytes_in,bytes_out").Output()
}

// parseNettop maps PIDs to their byte counters. The process name before the
// last dot is ignored (nettop truncates it); malformed lines are skipped.
func parseNettop(data []byte) map[int32]netBytes {
	out := map[int32]netBytes{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		f := strings.Split(sc.Text(), ",")
		if len(f) < 3 {
			continue
		}
		dot := strings.LastIndexByte(f[0], '.')
		if dot < 0 {
			continue
		}
		pid, err1 := strconv.ParseInt(f[0][dot+1:], 10, 32)
		in, err2 := strconv.ParseUint(f[1], 10, 64)
		o, err3 := strconv.ParseUint(f[2], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil || pid <= 0 {
			continue
		}
		out[int32(pid)] = netBytes{in: in, out: o}
	}
	return out
}
