//go:build darwin

package darwin

import (
	"strings"
	"testing"
)

func TestMachineDescribesThisMac(t *testing.T) {
	m := Machine()
	if m.Model == "" || m.ModelID == "" {
		t.Fatalf("model %q / %q empty", m.Model, m.ModelID)
	}
	if !strings.Contains(m.Chip, "Apple") && !strings.Contains(m.Chip, "Intel") {
		t.Fatalf("chip %q", m.Chip)
	}
	if m.OSVersion == "" || m.Build == "" {
		t.Fatalf("os %q build %q", m.OSVersion, m.Build)
	}
	if m.PerfCores+m.EffCores == 0 {
		t.Fatalf("no cores: %+v", m)
	}
	if m.Serial == "" {
		t.Fatalf("serial empty: %+v", m)
	}
	if m.DisplayW == 0 || m.DisplayH == 0 {
		t.Fatalf("display %dx%d", m.DisplayW, m.DisplayH)
	}
	t.Logf("%+v", m)
}
