//go:build darwin

// Package cf is a minimal CoreFoundation and IOKit binding via purego (no
// cgo), shared by gpu/apple and power. It converts CF property-list values to
// plain Go values so callers parse them with ordinary, testable code.
//
// Ownership: every function named Create/Copy in the CF sense returns a
// reference the caller must Release; ToGo never takes ownership.
package cf

import (
	"fmt"
	"strconv"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	encodingUTF8  = 0x08000100 // kCFStringEncodingUTF8
	numberSInt64  = 4          // kCFNumberSInt64Type
	numberFloat64 = 6          // kCFNumberFloat64Type

	// MainPort is kIOMainPortDefault.
	MainPort = 0
)

// Ref is a CFTypeRef; 0 is NULL.
type Ref = uintptr

var lib struct {
	cf, iokit uintptr

	typeString, typeNumber, typeBool, typeDict, typeArray, typeData uintptr

	release           func(Ref)
	getTypeID         func(Ref) uintptr
	stringCreate      func(alloc uintptr, s string, enc uint32) Ref
	stringLength      func(Ref) int
	stringMaxSize     func(length int, enc uint32) int
	stringGetCString  func(s Ref, buf *byte, size int, enc uint32) bool
	numberIsFloat     func(Ref) bool
	numberGetValue    func(n Ref, typ int, out unsafe.Pointer) bool
	boolValue         func(Ref) bool
	dictCount         func(Ref) int
	dictKeysAndValues func(d Ref, keys, values *uintptr)
	dictGetValue      func(d Ref, key Ref) Ref
	arrayCount        func(Ref) int
	arrayValueAt      func(a Ref, i int) Ref
	dataLength        func(Ref) int
	dataBytes         func(Ref) unsafe.Pointer

	serviceMatching       func(name string) Ref
	getMatchingServices   func(port uint32, matching Ref, iter *uint32) int32
	iteratorNext          func(iter uint32) uint32
	objectRelease         func(obj uint32) int32
	entryCreateCFProperty func(entry uint32, key Ref, alloc uintptr, options uint32) Ref
}

var (
	loadOnce sync.Once
	loadErr  error
)

// Load opens CoreFoundation and IOKit once; later calls return the first result.
func Load() error {
	loadOnce.Do(func() { loadErr = load() })
	return loadErr
}

func load() error {
	var err error
	lib.cf, err = purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("cf: open CoreFoundation: %w", err)
	}
	lib.iokit, err = purego.Dlopen("/System/Library/Frameworks/IOKit.framework/IOKit", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("cf: open IOKit: %w", err)
	}
	c, io := lib.cf, lib.iokit
	purego.RegisterLibFunc(&lib.release, c, "CFRelease")
	purego.RegisterLibFunc(&lib.getTypeID, c, "CFGetTypeID")
	purego.RegisterLibFunc(&lib.stringCreate, c, "CFStringCreateWithCString")
	purego.RegisterLibFunc(&lib.stringLength, c, "CFStringGetLength")
	purego.RegisterLibFunc(&lib.stringMaxSize, c, "CFStringGetMaximumSizeForEncoding")
	purego.RegisterLibFunc(&lib.stringGetCString, c, "CFStringGetCString")
	purego.RegisterLibFunc(&lib.numberIsFloat, c, "CFNumberIsFloatType")
	purego.RegisterLibFunc(&lib.numberGetValue, c, "CFNumberGetValue")
	purego.RegisterLibFunc(&lib.boolValue, c, "CFBooleanGetValue")
	purego.RegisterLibFunc(&lib.dictCount, c, "CFDictionaryGetCount")
	purego.RegisterLibFunc(&lib.dictKeysAndValues, c, "CFDictionaryGetKeysAndValues")
	purego.RegisterLibFunc(&lib.dictGetValue, c, "CFDictionaryGetValue")
	purego.RegisterLibFunc(&lib.arrayCount, c, "CFArrayGetCount")
	purego.RegisterLibFunc(&lib.arrayValueAt, c, "CFArrayGetValueAtIndex")
	purego.RegisterLibFunc(&lib.dataLength, c, "CFDataGetLength")
	purego.RegisterLibFunc(&lib.dataBytes, c, "CFDataGetBytePtr")

	var typeID func() uintptr
	for _, t := range []struct {
		dst  *uintptr
		name string
	}{
		{&lib.typeString, "CFStringGetTypeID"},
		{&lib.typeNumber, "CFNumberGetTypeID"},
		{&lib.typeBool, "CFBooleanGetTypeID"},
		{&lib.typeDict, "CFDictionaryGetTypeID"},
		{&lib.typeArray, "CFArrayGetTypeID"},
		{&lib.typeData, "CFDataGetTypeID"},
	} {
		purego.RegisterLibFunc(&typeID, c, t.name)
		*t.dst = typeID()
	}

	purego.RegisterLibFunc(&lib.serviceMatching, io, "IOServiceMatching")
	purego.RegisterLibFunc(&lib.getMatchingServices, io, "IOServiceGetMatchingServices")
	purego.RegisterLibFunc(&lib.iteratorNext, io, "IOIteratorNext")
	purego.RegisterLibFunc(&lib.objectRelease, io, "IOObjectRelease")
	purego.RegisterLibFunc(&lib.entryCreateCFProperty, io, "IORegistryEntryCreateCFProperty")
	return nil
}

// Lib returns the dlopen handle of a framework already opened by Load, for
// packages that bind further functions ("IOKit" or "CoreFoundation").
func Lib(name string) uintptr {
	if name == "IOKit" {
		return lib.iokit
	}
	return lib.cf
}

// Release calls CFRelease; NULL is ignored.
func Release(r Ref) {
	if r != 0 {
		lib.release(r)
	}
}

// String creates a CFString the caller must Release.
func String(s string) Ref { return lib.stringCreate(0, s, encodingUTF8) }

// DictValue returns the value for a string key without retaining it (the
// dictionary still owns it). Missing keys return 0.
func DictValue(d Ref, key string) Ref {
	if d == 0 {
		return 0
	}
	k := String(key)
	defer Release(k)
	return lib.dictGetValue(d, k)
}

// ArrayValues returns the elements of a CFArray without retaining them.
func ArrayValues(a Ref) []Ref {
	if a == 0 || lib.getTypeID(a) != lib.typeArray {
		return nil
	}
	n := lib.arrayCount(a)
	out := make([]Ref, n)
	for i := range n {
		out[i] = lib.arrayValueAt(a, i)
	}
	return out
}

// ToGo converts a CF property-list value: CFString → string, integer CFNumber
// → int64, float CFNumber → float64, CFBoolean → bool, CFData → []byte,
// CFArray → []any, CFDictionary → map[string]any (string keys as is, number
// keys in decimal). Other types (CFDate, …) and NULL become nil.
func ToGo(r Ref) any {
	if r == 0 {
		return nil
	}
	switch lib.getTypeID(r) {
	case lib.typeString:
		return goString(r)
	case lib.typeNumber:
		if lib.numberIsFloat(r) {
			var f float64
			lib.numberGetValue(r, numberFloat64, unsafe.Pointer(&f))
			return f
		}
		var i int64
		lib.numberGetValue(r, numberSInt64, unsafe.Pointer(&i))
		return i
	case lib.typeBool:
		return lib.boolValue(r)
	case lib.typeData:
		n := lib.dataLength(r)
		if n == 0 {
			return []byte{}
		}
		return append([]byte(nil), unsafe.Slice((*byte)(lib.dataBytes(r)), n)...)
	case lib.typeArray:
		vals := ArrayValues(r)
		out := make([]any, len(vals))
		for i, v := range vals {
			out[i] = ToGo(v)
		}
		return out
	case lib.typeDict:
		n := lib.dictCount(r)
		out := make(map[string]any, n)
		if n == 0 {
			return out
		}
		keys, vals := make([]uintptr, n), make([]uintptr, n)
		lib.dictKeysAndValues(r, &keys[0], &vals[0])
		for i := range n {
			var k string
			switch kv := ToGo(keys[i]).(type) {
			case string:
				k = kv
			case int64:
				k = strconv.FormatInt(kv, 10)
			default:
				continue
			}
			out[k] = ToGo(vals[i])
		}
		return out
	}
	return nil
}

func goString(r Ref) string {
	size := lib.stringMaxSize(lib.stringLength(r), encodingUTF8) + 1
	buf := make([]byte, size)
	if !lib.stringGetCString(r, &buf[0], size, encodingUTF8) {
		return ""
	}
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}

// Services returns the registry entries matching an IOKit class name. The
// caller must ReleaseObject each entry.
func Services(class string) ([]uint32, error) {
	matching := lib.serviceMatching(class) // consumed by IOServiceGetMatchingServices
	if matching == 0 {
		return nil, fmt.Errorf("cf: IOServiceMatching(%s) failed", class)
	}
	var iter uint32
	if kr := lib.getMatchingServices(MainPort, matching, &iter); kr != 0 {
		return nil, fmt.Errorf("cf: IOServiceGetMatchingServices(%s): kern_return %#x", class, kr)
	}
	defer lib.objectRelease(iter)
	var out []uint32
	for s := lib.iteratorNext(iter); s != 0; s = lib.iteratorNext(iter) {
		out = append(out, s)
	}
	return out, nil
}

// ReleaseObject calls IOObjectRelease.
func ReleaseObject(obj uint32) {
	if obj != 0 {
		lib.objectRelease(obj)
	}
}

// Property reads one registry property of entry as a Go value (nil when absent).
func Property(entry uint32, key string) any {
	k := String(key)
	defer Release(k)
	v := lib.entryCreateCFProperty(entry, k, 0, 0)
	defer Release(v)
	return ToGo(v)
}

// Int reads an integer-valued key; floats are truncated.
func Int(m map[string]any, key string) (int64, bool) {
	switch v := m[key].(type) {
	case int64:
		return v, true
	case float64:
		return int64(v), true
	}
	return 0, false
}

// Float reads a numeric key as float64.
func Float(m map[string]any, key string) (float64, bool) {
	switch v := m[key].(type) {
	case int64:
		return float64(v), true
	case float64:
		return v, true
	}
	return 0, false
}

// Bool reads a CFBoolean key.
func Bool(m map[string]any, key string) (bool, bool) {
	v, ok := m[key].(bool)
	return v, ok
}

// Str reads a string key; "" when absent or not a string.
func Str(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}
