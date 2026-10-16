package portable

import (
	"time"

	"hotwatch/model"
)

// powerStatus is Windows' SYSTEM_POWER_STATUS.
type powerStatus struct {
	ACLineStatus        uint8 // 0 offline, 1 online, 255 unknown
	BatteryFlag         uint8 // 8 charging, 128 no battery, 255 unknown
	BatteryLifePercent  uint8 // 255 unknown
	SystemStatusFlag    uint8
	BatteryLifeTime     uint32 // seconds; 0xFFFFFFFF unknown
	BatteryFullLifeTime uint32
}

const (
	flagCharging  = 8
	flagNoBattery = 128
	flagUnknown   = 255
)

// batteryFrom maps the power status; nil when the machine has no battery
// or the status is unknown.
func batteryFrom(st powerStatus) *model.Battery {
	if st.BatteryFlag == flagUnknown || st.BatteryFlag&flagNoBattery != 0 || st.BatteryLifePercent > 100 {
		return nil
	}
	b := &model.Battery{ChargePct: float64(st.BatteryLifePercent), OnAC: st.ACLineStatus == 1,
		Charging: st.BatteryFlag&flagCharging != 0}
	if st.BatteryLifeTime != 0xFFFFFFFF && !b.OnAC {
		d := time.Duration(st.BatteryLifeTime) * time.Second
		b.TimeRemaining = &d
	}
	return b
}
