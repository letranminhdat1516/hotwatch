package linux

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

func readLoad(path string) ([3]float64, error) {
	var out [3]float64
	b, err := os.ReadFile(path)
	if err != nil {
		return out, err
	}
	f := strings.Fields(string(b))
	if len(f) < 3 {
		return out, errors.New("short loadavg")
	}
	for i := range out {
		if out[i], err = strconv.ParseFloat(f[i], 64); err != nil {
			return [3]float64{}, err
		}
	}
	return out, nil
}

func readUptime(path string) (time.Duration, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	f := strings.Fields(string(b))
	if len(f) < 1 {
		return 0, errors.New("empty uptime")
	}
	sec, err := strconv.ParseFloat(f[0], 64)
	if err != nil || sec < 0 {
		return 0, errors.New("bad uptime")
	}
	return time.Duration(sec * float64(time.Second)), nil
}
