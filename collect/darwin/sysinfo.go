//go:build darwin

package darwin

import (
	"strings"
	"sync"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/unix"

	"hotwatch/internal/cf"
	"hotwatch/model"
)

// Machine describes this Mac for the About page: sysctl for the model,
// chip, core split and OS build; IOKit for the marketing name, serial and
// GPU core count; CoreGraphics for the main display. It is read once: none
// of it changes while the app runs. A fact that cannot be read stays zero.
var Machine = sync.OnceValue(readMachine)

func readMachine() model.Machine {
	str := func(name string) string {
		v, _ := unix.Sysctl(name)
		return strings.TrimSpace(v)
	}
	num := func(name string) int {
		v, _ := unix.SysctlUint32(name)
		return int(v)
	}
	m := model.Machine{
		ModelID:   str("hw.model"),
		Chip:      str("machdep.cpu.brand_string"),
		PerfCores: num("hw.perflevel0.physicalcpu"),
		EffCores:  num("hw.perflevel1.physicalcpu"),
		OSVersion: str("kern.osproductversion"),
		Build:     str("kern.osversion"),
	}
	if m.PerfCores == 0 && m.EffCores == 0 {
		m.PerfCores = num("hw.physicalcpu") // Intel: no performance levels
	}
	if cf.Load() == nil {
		if entries, err := cf.Services("IOPlatformExpertDevice"); err == nil {
			for _, e := range entries {
				m.Serial = cfString(cf.Property(e, "IOPlatformSerialNumber"))
				cf.ReleaseObject(e)
			}
		}
		// The marketing name ("MacBook Pro") is the product node's
		// product-name on Apple silicon; Intel Macs have no such node.
		if entries, err := cf.Services("IOPlatformDevice"); err == nil {
			for _, e := range entries {
				if m.Model == "" {
					m.Model = cfString(cf.Property(e, "product-name"))
				}
				cf.ReleaseObject(e)
			}
		}
		if entries, err := cf.Services("AGXAccelerator"); err == nil {
			for _, e := range entries {
				if n, ok := cf.Property(e, "gpu-core-count").(int64); ok && m.GPUCores == 0 {
					m.GPUCores = int(n)
				}
				cf.ReleaseObject(e)
			}
		}
	}
	if m.Model == "" {
		m.Model = m.ModelID
	}
	m.DisplayW, m.DisplayH, m.DisplayScale = mainDisplay()
	return m
}

// cfString is the string of a CFString or CFData property (IOKit stores
// some names as NUL-terminated data).
func cfString(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimRight(x, "\x00")
	case []byte:
		return strings.TrimRight(string(x), "\x00")
	}
	return ""
}

// mainDisplay is the main display's size in pixels and its scale (2 on a
// Retina display) through CoreGraphics; zeros when it cannot be read (no
// window server, as over ssh).
func mainDisplay() (w, h, scale int) {
	cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return 0, 0, 0
	}
	var mainID func() uint32
	var pointsWide func(uint32) uintptr
	var copyMode func(uint32) uintptr
	var pixelWidth, pixelHeight func(uintptr) uintptr
	var releaseMode func(uintptr)
	purego.RegisterLibFunc(&mainID, cg, "CGMainDisplayID")
	purego.RegisterLibFunc(&pointsWide, cg, "CGDisplayPixelsWide")
	purego.RegisterLibFunc(&copyMode, cg, "CGDisplayCopyDisplayMode")
	purego.RegisterLibFunc(&pixelWidth, cg, "CGDisplayModeGetPixelWidth")
	purego.RegisterLibFunc(&pixelHeight, cg, "CGDisplayModeGetPixelHeight")
	purego.RegisterLibFunc(&releaseMode, cg, "CGDisplayModeRelease")
	id := mainID()
	if id == 0 {
		return 0, 0, 0
	}
	mode := copyMode(id)
	if mode == 0 {
		return 0, 0, 0
	}
	defer releaseMode(mode)
	w, h = int(pixelWidth(mode)), int(pixelHeight(mode))
	if pts := int(pointsWide(id)); pts > 0 && w > 0 {
		scale = w / pts
	}
	return w, h, scale
}
