//go:build darwin

package darwin

import (
	"context"
	"time"

	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
)

func readLoad(ctx context.Context) ([3]float64, error) {
	a, err := load.AvgWithContext(ctx)
	if err != nil {
		return [3]float64{}, err
	}
	return [3]float64{a.Load1, a.Load5, a.Load15}, nil
}

func readUptime(ctx context.Context) (time.Duration, error) {
	u, err := host.UptimeWithContext(ctx)
	if err != nil {
		return 0, err
	}
	return time.Duration(u) * time.Second, nil
}
