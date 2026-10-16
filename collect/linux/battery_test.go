package linux

import (
	"testing"
	"time"
)

func TestBatteryFromSysfs(t *testing.T) {
	b, err := Battery("testdata/battery")
	if err != nil || b == nil {
		t.Fatalf("battery %+v err %v", b, err)
	}
	if b.ChargePct != 57 || b.Charging || b.OnAC {
		t.Fatalf("state %+v", b)
	}
	if b.PowerW == nil || *b.PowerW != 12.5 {
		t.Fatalf("power %v", b.PowerW)
	}
	if b.TimeRemaining == nil || *b.TimeRemaining != 144*time.Minute { // 30 Wh at 12.5 W
		t.Fatalf("time remaining %v", b.TimeRemaining)
	}
	if b.HealthPct == nil || *b.HealthPct != 80 || b.Cycles == nil || *b.Cycles != 212 {
		t.Fatalf("health %v cycles %v", b.HealthPct, b.Cycles)
	}
}

func TestBatteryAbsent(t *testing.T) {
	b, err := Battery(t.TempDir())
	if b != nil || err != nil {
		t.Fatalf("no power_supply: %+v %v (want nil, nil)", b, err)
	}
}
