//go:build darwin && arm64

package sensors

import (
	"errors"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// HID usage of the temperature sensors and the event that carries a reading
// (IOHIDEventTypes.h): kIOHIDEventTypeTemperature, its level field.
const (
	pageAppleVendor  = 0xff00
	usageTemperature = 5
	eventTemperature = 15
	fieldTemperature = eventTemperature << 16
	cfNumberSInt32   = 3
	cfUTF8           = 0x08000100
)

type fns struct {
	cfRelease         func(uintptr)
	cfStringCreate    func(alloc uintptr, s *byte, enc uint32) uintptr
	cfStringGet       func(s uintptr, buf *byte, size int, enc uint32) bool
	cfNumberCreate    func(alloc uintptr, typ int, v unsafe.Pointer) uintptr
	cfDictCreate      func(alloc uintptr, keys, vals *uintptr, n int, kcb, vcb uintptr) uintptr
	cfArrayCount      func(a uintptr) int
	cfArrayAt         func(a uintptr, i int) uintptr
	clientCreate      func(alloc uintptr) uintptr
	clientSetMatching func(client, match uintptr) int32
	clientServices    func(client uintptr) uintptr
	serviceProperty   func(service, key uintptr) uintptr
	serviceEvent      func(service uintptr, typ int64, matching uintptr, opts int64) uintptr
	eventFloat        func(event uintptr, field int32) float64
	keyCallbacks      uintptr
	valueCallbacks    uintptr
}

var load = sync.OnceValues(func() (*fns, error) {
	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, err
	}
	io, err := purego.Dlopen("/System/Library/Frameworks/IOKit.framework/IOKit", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, err
	}
	f := &fns{}
	purego.RegisterLibFunc(&f.cfRelease, cf, "CFRelease")
	purego.RegisterLibFunc(&f.cfStringCreate, cf, "CFStringCreateWithCString")
	purego.RegisterLibFunc(&f.cfStringGet, cf, "CFStringGetCString")
	purego.RegisterLibFunc(&f.cfNumberCreate, cf, "CFNumberCreate")
	purego.RegisterLibFunc(&f.cfDictCreate, cf, "CFDictionaryCreate")
	purego.RegisterLibFunc(&f.cfArrayCount, cf, "CFArrayGetCount")
	purego.RegisterLibFunc(&f.cfArrayAt, cf, "CFArrayGetValueAtIndex")
	purego.RegisterLibFunc(&f.clientCreate, io, "IOHIDEventSystemClientCreate")
	purego.RegisterLibFunc(&f.clientSetMatching, io, "IOHIDEventSystemClientSetMatching")
	purego.RegisterLibFunc(&f.clientServices, io, "IOHIDEventSystemClientCopyServices")
	purego.RegisterLibFunc(&f.serviceProperty, io, "IOHIDServiceClientCopyProperty")
	purego.RegisterLibFunc(&f.serviceEvent, io, "IOHIDServiceClientCopyEvent")
	purego.RegisterLibFunc(&f.eventFloat, io, "IOHIDEventGetFloatValue")
	kcb, err := purego.Dlsym(cf, "kCFTypeDictionaryKeyCallBacks")
	if err != nil {
		return nil, err
	}
	vcb, err := purego.Dlsym(cf, "kCFTypeDictionaryValueCallBacks")
	if err != nil {
		return nil, err
	}
	f.keyCallbacks, f.valueCallbacks = kcb, vcb
	return f, nil
})

func (f *fns) str(s string) uintptr {
	b := append([]byte(s), 0)
	return f.cfStringCreate(0, &b[0], cfUTF8)
}

func (f *fns) goStr(s uintptr) string {
	if s == 0 {
		return ""
	}
	var buf [128]byte
	if !f.cfStringGet(s, &buf[0], len(buf), cfUTF8) {
		return ""
	}
	for i, c := range buf {
		if c == 0 {
			return string(buf[:i])
		}
	}
	return string(buf[:])
}

func (f *fns) num(v int32) uintptr { return f.cfNumberCreate(0, cfNumberSInt32, unsafe.Pointer(&v)) }

var (
	clientOnce sync.Once
	client     uintptr // kept for the process: creating one per read leaks Mach ports
	clientMu   sync.Mutex
)

// Read returns every temperature sensor's current reading.
func Read() ([]Reading, error) {
	f, err := load()
	if err != nil {
		return nil, err
	}
	clientOnce.Do(func() {
		c := f.clientCreate(0)
		if c == 0 {
			return
		}
		keys := []uintptr{f.str("PrimaryUsagePage"), f.str("PrimaryUsage")}
		vals := []uintptr{f.num(pageAppleVendor), f.num(usageTemperature)}
		match := f.cfDictCreate(0, &keys[0], &vals[0], 2, f.keyCallbacks, f.valueCallbacks)
		for _, x := range append(keys, vals...) {
			f.cfRelease(x)
		}
		f.clientSetMatching(c, match)
		f.cfRelease(match)
		client = c
	})
	if client == 0 {
		return nil, errors.New("sensors: IOHIDEventSystemClientCreate failed")
	}
	clientMu.Lock()
	defer clientMu.Unlock()
	services := f.clientServices(client)
	if services == 0 {
		return nil, nil
	}
	defer f.cfRelease(services)
	product := f.str("Product")
	defer f.cfRelease(product)
	n := f.cfArrayCount(services)
	out := make([]Reading, 0, n)
	for i := 0; i < n; i++ {
		s := f.cfArrayAt(services, i)
		ev := f.serviceEvent(s, eventTemperature, 0, 0)
		if ev == 0 {
			continue
		}
		c := f.eventFloat(ev, fieldTemperature)
		f.cfRelease(ev)
		name := f.serviceProperty(s, product)
		out = append(out, Reading{Name: f.goStr(name), C: c})
		if name != 0 {
			f.cfRelease(name)
		}
	}
	return out, nil
}
