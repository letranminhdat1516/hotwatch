package nvidia

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

var wantArgs = []string{"--query-gpu=name,utilization.gpu,memory.used,memory.total,temperature.gpu", "--format=csv,noheader,nounits"}

func fakeRunner(out []byte, err error) func(context.Context, string, ...string) ([]byte, error) {
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "nvidia-smi" || !reflect.DeepEqual(args, wantArgs) {
			return nil, errors.New("unexpected command: " + name + " " + strings.Join(args, " "))
		}
		return out, err
	}
}

func TestReadCannedLine(t *testing.T) {
	gpus, err := Read(context.Background(), fakeRunner([]byte("NVIDIA GeForce RTX 3080, 34, 1595, 10240, 52\n"), nil))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(gpus) != 1 {
		t.Fatalf("got %d GPUs, want 1", len(gpus))
	}
	g := gpus[0]
	if g.Name != "NVIDIA GeForce RTX 3080" {
		t.Errorf("Name = %q", g.Name)
	}
	if g.UtilPct == nil || *g.UtilPct != 34 {
		t.Errorf("UtilPct = %v, want 34", g.UtilPct)
	}
	if g.MemUsed == nil || *g.MemUsed != 1595*mib {
		t.Errorf("MemUsed = %v, want %d", g.MemUsed, 1595*mib)
	}
	if g.MemTotal == nil || *g.MemTotal != 10240*mib {
		t.Errorf("MemTotal = %v, want %d", g.MemTotal, 10240*mib)
	}
	if g.TempC == nil || *g.TempC != 52 {
		t.Errorf("TempC = %v, want 52", g.TempC)
	}
}

func TestReadNotSupportedFieldsAreNil(t *testing.T) {
	gpus, err := Read(context.Background(), fakeRunner([]byte("Tesla T4, [N/A], [Not Supported], , [N/A]\n"), nil))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(gpus) != 1 {
		t.Fatalf("got %d GPUs, want 1", len(gpus))
	}
	g := gpus[0]
	if g.Name != "Tesla T4" {
		t.Errorf("Name = %q", g.Name)
	}
	if g.UtilPct != nil || g.MemUsed != nil || g.MemTotal != nil || g.TempC != nil {
		t.Errorf("unknown fields must be nil, got %+v", g)
	}
}

func TestReadTwoGPUs(t *testing.T) {
	out := []byte("NVIDIA A, 1, 2, 3, 4\nNVIDIA B, [N/A], 5, 6, [N/A]\n")
	gpus, err := Read(context.Background(), fakeRunner(out, nil))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(gpus) != 2 {
		t.Fatalf("got %d GPUs, want 2", len(gpus))
	}
	if gpus[0].Name != "NVIDIA A" || gpus[1].Name != "NVIDIA B" {
		t.Errorf("names = %q, %q", gpus[0].Name, gpus[1].Name)
	}
	if gpus[1].UtilPct != nil || gpus[1].TempC != nil || gpus[1].MemUsed == nil || *gpus[1].MemUsed != 5*mib {
		t.Errorf("second GPU = %+v", gpus[1])
	}
}

func TestReadNotFoundMeansNoGPU(t *testing.T) {
	notFound := func(context.Context, string, ...string) ([]byte, error) {
		return nil, &exec.Error{Name: "nvidia-smi", Err: exec.ErrNotFound}
	}
	gpus, err := Read(context.Background(), notFound)
	if gpus != nil || err != nil {
		t.Fatalf("Read = %v, %v; want nil, nil", gpus, err)
	}
}

func TestReadTimeoutIsError(t *testing.T) {
	timeout := func(context.Context, string, ...string) ([]byte, error) {
		return nil, context.DeadlineExceeded
	}
	if _, err := Read(context.Background(), timeout); err == nil {
		t.Fatal("Read returned nil error on timeout")
	}
}

func TestReadNonZeroExitIncludesStderrLine(t *testing.T) {
	exitErr := &exec.ExitError{Stderr: []byte("NVIDIA-SMI has failed\nsecond line\n")}
	_, err := Read(context.Background(), fakeRunner(nil, exitErr))
	if err == nil || !strings.Contains(err.Error(), "NVIDIA-SMI has failed") || strings.Contains(err.Error(), "second line") {
		t.Fatalf("error = %v; want first stderr line only", err)
	}
}

func TestRunTimesOutAndReportsIt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := Run(ctx, "sleep", "5")
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run error = %v; want deadline exceeded", err)
	}
}
