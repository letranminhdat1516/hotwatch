//go:build darwin

package power

import (
	"context"
	"hotwatch/model"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"testing"
	"time"
)

var pmsetPct = regexp.MustCompile(`\t(\d+)%;`)

func pmsetCharge(t *testing.T) float64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "pmset", "-g", "batt").Output()
	if err != nil {
		t.Fatalf("pmset -g batt: %v", err)
	}
	m := pmsetPct.FindSubmatch(out)
	if m == nil {
		t.Fatalf("no percentage in pmset output: %q", out)
	}
	v, _ := strconv.ParseFloat(string(m[1]), 64)
	return v
}

func TestReadBatteryMatchesPmset(t *testing.T) {
	b, err := Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if b == nil {
		t.Fatal("battery is nil on this MacBook")
	}
	if want := pmsetCharge(t); math.Abs(b.ChargePct-want) > 1 {
		t.Errorf("ChargePct = %v, pmset says %v", b.ChargePct, want)
	}
	switch b.ThermalState {
	case "nominal", "fair", "serious", "critical":
	default:
		t.Errorf("ThermalState = %q", b.ThermalState)
	}
	if b.TempC == nil || *b.TempC < 0 || *b.TempC > 80 {
		t.Errorf("TempC = %v, want a plausible battery temperature", b.TempC)
	}
	if b.PowerW != nil && (*b.PowerW < 0 || *b.PowerW > 300) {
		t.Errorf("PowerW = %v", *b.PowerW)
	}
	if b.Charging && !b.OnAC {
		t.Error("charging while not on AC")
	}
}

func TestFromPowerSource(t *testing.T) {
	b, ok := fromPowerSource(map[string]any{
		"Type": "InternalBattery", "Is Present": true,
		"Current Capacity": int64(42), "Max Capacity": int64(100),
		"Is Charging": true, "Power Source State": "AC Power",
		"Time to Empty": int64(0), "Time to Full Charge": int64(81),
	})
	if !ok || b.ChargePct != 42 || !b.Charging || !b.OnAC {
		t.Fatalf("got %+v ok=%v", b, ok)
	}
	if b.TimeRemaining == nil || *b.TimeRemaining != 81*time.Minute {
		t.Fatalf("TimeRemaining = %v, want 81m (time to full)", b.TimeRemaining)
	}

	b, ok = fromPowerSource(map[string]any{
		"Type": "InternalBattery", "Is Present": true,
		"Current Capacity": int64(1500), "Max Capacity": int64(3000),
		"Is Charging": false, "Power Source State": "Battery Power",
		"Time to Empty": int64(-1), "Time to Full Charge": int64(-1),
	})
	if !ok || b.ChargePct != 50 || b.Charging || b.OnAC {
		t.Fatalf("got %+v ok=%v", b, ok)
	}
	if b.TimeRemaining != nil {
		t.Fatalf("TimeRemaining = %v, want nil while the OS is still calculating (-1)", *b.TimeRemaining)
	}

	b, _ = fromPowerSource(map[string]any{
		"Type": "InternalBattery", "Is Present": true,
		"Current Capacity": int64(80), "Max Capacity": int64(100),
		"Is Charging": false, "Power Source State": "Battery Power",
		"Time to Empty": int64(185),
	})
	if b.TimeRemaining == nil || *b.TimeRemaining != 185*time.Minute {
		t.Fatalf("TimeRemaining = %v, want 185m (time to empty)", b.TimeRemaining)
	}

	for _, desc := range []map[string]any{
		{"Type": "UPS", "Is Present": true, "Current Capacity": int64(1), "Max Capacity": int64(1)},
		{"Type": "InternalBattery", "Is Present": false, "Current Capacity": int64(1), "Max Capacity": int64(1)},
		{"Type": "InternalBattery", "Is Present": true, "Current Capacity": int64(1), "Max Capacity": int64(0)},
	} {
		if _, ok := fromPowerSource(desc); ok {
			t.Errorf("accepted %v", desc)
		}
	}
}

func TestSmartBattery(t *testing.T) {
	w, c := smartBattery(map[string]any{"InstantAmperage": int64(-1500), "Voltage": int64(12000), "Temperature": int64(3095)})
	if w == nil || math.Abs(*w-18) > 1e-9 {
		t.Fatalf("PowerW = %v, want 18", w)
	}
	if c == nil || math.Abs(*c-30.95) > 1e-9 {
		t.Fatalf("TempC = %v, want 30.95", c)
	}
	w, c = smartBattery(map[string]any{"Voltage": int64(12000)})
	if w != nil || c != nil {
		t.Fatalf("missing values must be nil: %v %v", w, c)
	}
}

func TestThermalName(t *testing.T) {
	for in, want := range map[int64]string{0: "nominal", 1: "fair", 2: "serious", 3: "critical", 9: ""} {
		if got := thermalName(in); got != want {
			t.Errorf("thermalName(%d) = %q, want %q", in, got, want)
		}
	}
	if s := ThermalState(); s == "" {
		t.Error("ThermalState() empty on this Mac")
	}
}

func BenchmarkRead(b *testing.B) {
	ctx := context.Background()
	for b.Loop() {
		if _, err := Read(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func TestBatteryHealthFromCapacities(t *testing.T) {
	var b model.Battery
	batteryHealth(&b, map[string]any{"AppleRawMaxCapacity": int64(6016), "DesignCapacity": int64(6249), "CycleCount": int64(92)})
	if b.HealthPct == nil || *b.HealthPct < 96.2 || *b.HealthPct > 96.4 {
		t.Fatalf("health %v", b.HealthPct)
	}
	if b.Cycles == nil || *b.Cycles != 92 || b.CapacityMAh == nil || *b.CapacityMAh != 6016 || b.DesignMAh == nil || *b.DesignMAh != 6249 {
		t.Fatalf("battery %+v", b)
	}
	var none model.Battery
	batteryHealth(&none, map[string]any{"CycleCount": int64(3)})
	if none.HealthPct != nil || none.Cycles == nil {
		t.Fatalf("without capacities: %+v", none)
	}
}
