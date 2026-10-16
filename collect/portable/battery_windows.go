package portable

import (
	"context"
	"unsafe"

	"golang.org/x/sys/windows"

	"hotwatch/model"
)

var procGetSystemPowerStatus = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetSystemPowerStatus")

// Battery reads the battery through GetSystemPowerStatus; nil, nil on a
// machine without one.
func Battery(context.Context) (*model.Battery, error) {
	var st powerStatus
	if r, _, err := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&st))); r == 0 {
		return nil, err
	}
	return batteryFrom(st), nil
}
