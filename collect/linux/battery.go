package linux

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"hotwatch/model"
)

// Battery reads the first battery under root/sys/class/power_supply (a
// laptop running the app); nil, nil when there is none. Energy is read in
// µWh and power in µW, or charge in µAh and current in µA.
func Battery(root string) (*model.Battery, error) {
	dir := filepath.Join(root, "sys", "class", "power_supply")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var bat string
	onAC := false
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		switch readStr(p, "type") {
		case "Battery":
			if bat == "" && readStr(p, "scope") != "Device" { // not a mouse's battery
				bat = p
			}
		case "Mains", "USB":
			if readStr(p, "online") == "1" {
				onAC = true
			}
		}
	}
	if bat == "" {
		return nil, nil
	}
	pct, ok := readNum(bat, "capacity")
	if !ok {
		return nil, errors.New("battery capacity unreadable")
	}
	status := readStr(bat, "status")
	b := &model.Battery{ChargePct: pct, OnAC: onAC || status == "Charging" || status == "Full",
		Charging: status == "Charging"}
	now, full, design := "energy_now", "energy_full", "energy_full_design"
	rate, rateOK := readNum(bat, "power_now")
	if _, ok := readNum(bat, now); !ok {
		now, full, design = "charge_now", "charge_full", "charge_full_design"
		rate, rateOK = readNum(bat, "current_now")
	}
	if rateOK && rate > 0 && now == "energy_now" {
		w := rate / 1e6
		b.PowerW = &w
	}
	if left, ok := readNum(bat, now); ok && rateOK && rate > 0 && status == "Discharging" {
		d := time.Duration(left / rate * float64(time.Hour))
		b.TimeRemaining = &d
	}
	if f, ok := readNum(bat, full); ok {
		if d, ok := readNum(bat, design); ok && d > 0 {
			h := f / d * 100
			b.HealthPct = &h
		}
	}
	if c, ok := readNum(bat, "cycle_count"); ok && c > 0 {
		n := int(c)
		b.Cycles = &n
	}
	return b, nil
}

func readStr(dir, name string) string {
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func readNum(dir, name string) (float64, bool) {
	v, err := strconv.ParseFloat(readStr(dir, name), 64)
	return v, err == nil
}
