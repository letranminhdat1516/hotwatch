package linux

import (
	"bufio"
	"errors"
	"os"
	"strconv"
	"strings"

	"hotwatch/model"
)

func readMemory(meminfo, pressure string) (model.Memory, error) {
	f, err := os.Open(meminfo)
	if err != nil {
		return model.Memory{}, err
	}
	defer f.Close()
	kb := map[string]uint64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		v, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return model.Memory{}, errors.New("malformed meminfo value for " + key)
		}
		kb[key] = v * 1024
	}
	if err := sc.Err(); err != nil {
		return model.Memory{}, err
	}
	total, okT := kb["MemTotal"]
	avail, okA := kb["MemAvailable"]
	if !okT || !okA || avail > total || kb["SwapFree"] > kb["SwapTotal"] {
		return model.Memory{}, errors.New("meminfo missing or inconsistent MemTotal/MemAvailable")
	}
	return model.Memory{
		Total:     total,
		Used:      total - avail,
		Cached:    kb["Cached"],
		Buffers:   kb["Buffers"],
		SwapTotal: kb["SwapTotal"],
		SwapUsed:  kb["SwapTotal"] - kb["SwapFree"],
		Pressure:  readPressure(pressure),
	}, nil
}

// readPressure maps PSI "some avg10" to a level; "" when PSI is unavailable.
func readPressure(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || f[0] != "some" {
			continue
		}
		for _, kv := range f[1:] {
			if v, ok := strings.CutPrefix(kv, "avg10="); ok {
				p, err := strconv.ParseFloat(v, 64)
				if err != nil {
					return ""
				}
				switch {
				case p < 10:
					return "normal"
				case p < 30:
					return "warn"
				}
				return "critical"
			}
		}
	}
	return ""
}
