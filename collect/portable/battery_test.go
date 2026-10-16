package portable

import (
	"testing"
	"time"
)

func TestBatteryFromPowerStatus(t *testing.T) {
	b := batteryFrom(powerStatus{ACLineStatus: 0, BatteryFlag: 1, BatteryLifePercent: 64, BatteryLifeTime: 5400})
	if b == nil || b.ChargePct != 64 || b.OnAC || b.Charging || b.TimeRemaining == nil || *b.TimeRemaining != 90*time.Minute {
		t.Fatalf("on battery: %+v", b)
	}
	b = batteryFrom(powerStatus{ACLineStatus: 1, BatteryFlag: 8, BatteryLifePercent: 80, BatteryLifeTime: 0xFFFFFFFF})
	if b == nil || !b.OnAC || !b.Charging || b.TimeRemaining != nil {
		t.Fatalf("charging: %+v", b)
	}
	if b := batteryFrom(powerStatus{ACLineStatus: 1, BatteryFlag: 128, BatteryLifePercent: 255}); b != nil {
		t.Fatalf("a desktop without a battery: %+v", b)
	}
	if b := batteryFrom(powerStatus{ACLineStatus: 1, BatteryFlag: 255, BatteryLifePercent: 255}); b != nil {
		t.Fatalf("unknown status: %+v", b)
	}
}
