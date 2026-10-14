//go:build darwin

// Package power reads the Mac battery (charge, charging state, time
// remaining, power draw, temperature), the thermal state and the sleep
// assertions. Everything goes through IOKit/IOPM and the Objective-C runtime
// via purego: no cgo, no root, no subprocess, so a Read fits the 2 s refresh.
package power

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"hotwatch/internal/cf"
	"hotwatch/model"
)

var ps struct {
	once sync.Once
	err  error

	copyInfo    func() cf.Ref
	copyList    func(blob cf.Ref) cf.Ref
	description func(blob, source cf.Ref) cf.Ref // not owned by the caller
	byProcess   func(out *cf.Ref) int32
}

func load() error {
	ps.once.Do(func() {
		if ps.err = cf.Load(); ps.err != nil {
			return
		}
		io := cf.Lib("IOKit")
		purego.RegisterLibFunc(&ps.copyInfo, io, "IOPSCopyPowerSourcesInfo")
		purego.RegisterLibFunc(&ps.copyList, io, "IOPSCopyPowerSourcesList")
		purego.RegisterLibFunc(&ps.description, io, "IOPSGetPowerSourceDescription")
		purego.RegisterLibFunc(&ps.byProcess, io, "IOPMCopyAssertionsByProcess")
	})
	return ps.err
}

// Read returns the internal battery, or nil when the Mac has none. Unknown
// values (time remaining while the OS is still estimating, power draw or
// temperature the gauge does not report) are nil. If only the assertion list
// fails, Read returns the battery with Assertions nil together with the error.
func Read(ctx context.Context) (*model.Battery, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := load(); err != nil {
		return nil, err
	}
	b, ok := internalBattery()
	if !ok {
		return nil, nil
	}
	b.PowerW, b.TempC = smartBattery(smartBatteryProps())
	b.ThermalState = ThermalState()
	list, err := Assertions(ctx)
	b.Assertions = list
	return &b, err
}

// internalBattery reads IOPSCopyPowerSourcesInfo and returns the first
// present internal battery.
func internalBattery() (model.Battery, bool) {
	blob := ps.copyInfo()
	if blob == 0 {
		return model.Battery{}, false
	}
	defer cf.Release(blob)
	list := ps.copyList(blob)
	defer cf.Release(list)
	for _, src := range cf.ArrayValues(list) {
		desc, _ := cf.ToGo(ps.description(blob, src)).(map[string]any)
		if b, ok := fromPowerSource(desc); ok {
			return b, true
		}
	}
	return model.Battery{}, false
}

// fromPowerSource maps an IOPS power-source description (keys from
// <IOKit/ps/IOPSKeys.h>) to a Battery; ok is false for anything but a present
// internal battery with a usable capacity.
func fromPowerSource(d map[string]any) (model.Battery, bool) {
	if cf.Str(d, "Type") != "InternalBattery" {
		return model.Battery{}, false
	}
	if present, ok := cf.Bool(d, "Is Present"); ok && !present {
		return model.Battery{}, false
	}
	cur, ok1 := cf.Float(d, "Current Capacity")
	maxCap, ok2 := cf.Float(d, "Max Capacity")
	if !ok1 || !ok2 || maxCap <= 0 {
		return model.Battery{}, false
	}
	charging, _ := cf.Bool(d, "Is Charging")
	b := model.Battery{
		ChargePct: cur / maxCap * 100,
		Charging:  charging,
		OnAC:      cf.Str(d, "Power Source State") == "AC Power",
	}
	key := ""
	switch {
	case b.Charging:
		key = "Time to Full Charge"
	case !b.OnAC:
		key = "Time to Empty"
	}
	if mins, ok := cf.Int(d, key); ok && mins >= 0 {
		tr := time.Duration(mins) * time.Minute
		b.TimeRemaining = &tr
	}
	return b, true
}

var smartBatteryKeys = []string{"InstantAmperage", "Voltage", "Temperature"}

// smartBatteryProps reads only the few AppleSmartBattery properties needed;
// the full property table is large (BatteryData, LifetimeData) and not read.
func smartBatteryProps() map[string]any {
	entries, err := cf.Services("AppleSmartBattery")
	if err != nil || len(entries) == 0 {
		return nil
	}
	defer func() {
		for _, e := range entries {
			cf.ReleaseObject(e)
		}
	}()
	props := make(map[string]any, len(smartBatteryKeys))
	for _, k := range smartBatteryKeys {
		props[k] = cf.Property(entries[0], k)
	}
	return props
}

// smartBattery derives power draw (|InstantAmperage mA × Voltage mV| / 1e6,
// in W: the battery's charge or discharge power) and the battery temperature
// (centi-°C → °C). Missing values are nil.
func smartBattery(p map[string]any) (powerW, tempC *float64) {
	amp, ok1 := cf.Int(p, "InstantAmperage")
	mv, ok2 := cf.Int(p, "Voltage")
	if ok1 && ok2 && mv > 0 {
		w := math.Abs(float64(amp) * float64(mv) / 1e6)
		powerW = &w
	}
	if t, ok := cf.Int(p, "Temperature"); ok {
		c := float64(t) / 100
		tempC = &c
	}
	return powerW, tempC
}

var thermal struct {
	once        sync.Once
	processInfo objc.ID
	sel         objc.SEL
}

// ThermalState returns NSProcessInfo.processInfo.thermalState as "nominal",
// "fair", "serious" or "critical"; "" when unavailable.
func ThermalState() string {
	thermal.once.Do(func() {
		if _, err := purego.Dlopen("/System/Library/Frameworks/Foundation.framework/Foundation", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
			return
		}
		cls := objc.GetClass("NSProcessInfo")
		if cls == 0 {
			return
		}
		// processInfo is a process-wide singleton: no ownership to release.
		thermal.processInfo = objc.ID(cls).Send(objc.RegisterName("processInfo"))
		thermal.sel = objc.RegisterName("thermalState")
	})
	if thermal.processInfo == 0 {
		return ""
	}
	return thermalName(objc.Send[int64](thermal.processInfo, thermal.sel))
}

// thermalName maps NSProcessInfoThermalState values.
func thermalName(v int64) string {
	switch v {
	case 0:
		return "nominal"
	case 1:
		return "fair"
	case 2:
		return "serious"
	case 3:
		return "critical"
	}
	return ""
}
