// Package nvidia reads NVIDIA GPU utilisation, memory and temperature by
// running nvidia-smi. A host without nvidia-smi has no NVIDIA GPUs.
package nvidia

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"hotwatch/model"
)

const (
	command    = "nvidia-smi"
	mib        = 1024 * 1024
	timeout    = 2 * time.Second
	fieldCount = 5
)

var queryArgs = []string{
	"--query-gpu=name,utilization.gpu,memory.used,memory.total,temperature.gpu",
	"--format=csv,noheader,nounits",
}

// Run is the production runner: it executes name with args under a 2 s
// timeout derived from ctx and returns its stdout.
func Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("%s timed out after %s: %w", name, timeout, ctx.Err())
	}
	return out, err
}

// Read returns one model.GPU per GPU that nvidia-smi reports. A missing
// nvidia-smi means there are no NVIDIA GPUs and yields (nil, nil).
func Read(ctx context.Context, run func(ctx context.Context, name string, args ...string) ([]byte, error)) ([]model.GPU, error) {
	out, err := run(ctx, command, queryArgs...)
	if errors.Is(err, exec.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("%s: %w: %s", command, err, firstLine(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("%s: %w", command, err)
	}

	var gpus []model.GPU
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		g, err := parseLine(line)
		if err != nil {
			return nil, err
		}
		gpus = append(gpus, g)
	}
	return gpus, nil
}

func parseLine(line string) (model.GPU, error) {
	raw := strings.Split(line, ",")
	if len(raw) != fieldCount {
		return model.GPU{}, fmt.Errorf("%s: want %d fields, got %d in %q", command, fieldCount, len(raw), line)
	}
	fields := make([]string, len(raw))
	for i, f := range raw {
		fields[i] = strings.TrimSpace(f)
	}

	util, err := optFloat(fields[1])
	if err != nil {
		return model.GPU{}, fmt.Errorf("%s utilization.gpu: %w", command, err)
	}
	used, err := optFloat(fields[2])
	if err != nil {
		return model.GPU{}, fmt.Errorf("%s memory.used: %w", command, err)
	}
	total, err := optFloat(fields[3])
	if err != nil {
		return model.GPU{}, fmt.Errorf("%s memory.total: %w", command, err)
	}
	temp, err := optFloat(fields[4])
	if err != nil {
		return model.GPU{}, fmt.Errorf("%s temperature.gpu: %w", command, err)
	}

	return model.GPU{
		Name:     fields[0],
		UtilPct:  util,
		MemUsed:  mibToBytes(used),
		MemTotal: mibToBytes(total),
		TempC:    temp,
	}, nil
}

// optFloat returns nil for nvidia-smi's "unknown" markers and empty fields.
func optFloat(s string) (*float64, error) {
	if s == "" || s == "[N/A]" || s == "[Not Supported]" {
		return nil, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", s, err)
	}
	return &v, nil
}

func mibToBytes(v *float64) *uint64 {
	if v == nil {
		return nil
	}
	b := uint64(*v * mib)
	return &b
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}
