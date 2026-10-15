//go:build darwin

package cf

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Test-only constructors, so ToGo is exercised on real CoreFoundation values.
var (
	numberCreate func(alloc uintptr, typ int, value unsafe.Pointer) uintptr
	dictCreate   func(alloc uintptr, keys, values *uintptr, n int, keyCB, valueCB uintptr) uintptr
	arrayCreate  func(alloc uintptr, values *uintptr, n int, cb uintptr) uintptr

	arrayCallbacks, dictKeyCallbacks, dictValueCallbacks uintptr
)

func mustLoad(t *testing.T) {
	t.Helper()
	if err := Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if numberCreate == nil {
		purego.RegisterLibFunc(&numberCreate, lib.cf, "CFNumberCreate")
		purego.RegisterLibFunc(&dictCreate, lib.cf, "CFDictionaryCreate")
		purego.RegisterLibFunc(&arrayCreate, lib.cf, "CFArrayCreate")
		arrayCallbacks = mustSym(t, "kCFTypeArrayCallBacks")
		dictKeyCallbacks = mustSym(t, "kCFTypeDictionaryKeyCallBacks")
		dictValueCallbacks = mustSym(t, "kCFTypeDictionaryValueCallBacks")
	}
}

func mustSym(t *testing.T, name string) uintptr {
	t.Helper()
	p, err := purego.Dlsym(lib.cf, name)
	if err != nil {
		t.Fatalf("dlsym %s: %v", name, err)
	}
	return p
}

func newInt(t *testing.T, v int64) uintptr {
	return numberCreate(0, numberSInt64, unsafe.Pointer(&v))
}

func newFloat(t *testing.T, v float64) uintptr {
	return numberCreate(0, numberFloat64, unsafe.Pointer(&v))
}

func TestToGoScalars(t *testing.T) {
	mustLoad(t)
	s := String("Device Utilization %")
	defer Release(s)
	if got := ToGo(s); got != "Device Utilization %" {
		t.Fatalf("string: got %#v", got)
	}
	n := newInt(t, -6130)
	defer Release(n)
	if got := ToGo(n); got != int64(-6130) {
		t.Fatalf("int: got %#v", got)
	}
	f := newFloat(t, 1.5)
	defer Release(f)
	if got := ToGo(f); got != 1.5 {
		t.Fatalf("float: got %#v", got)
	}
	if got := ToGo(0); got != nil {
		t.Fatalf("NULL: got %#v", got)
	}
}

func TestToGoContainers(t *testing.T) {
	mustLoad(t)
	k1, k2 := String("name"), String("n")
	v1, v2 := String("x"), newInt(t, 7)
	arr := arrayCreate(0, &[]uintptr{v1, v2}[0], 2, arrayCallbacks)
	k3 := newInt(t, 42) // numeric keys become decimal strings
	keys := []uintptr{k1, k2, k3}
	vals := []uintptr{v1, v2, arr}
	d := dictCreate(0, &keys[0], &vals[0], 3, dictKeyCallbacks, dictValueCallbacks)
	for _, r := range []uintptr{k1, k2, k3, v1, v2, arr} {
		Release(r)
	}
	defer Release(d)
	want := map[string]any{"name": "x", "n": int64(7), "42": []any{"x", int64(7)}}
	if got := ToGo(d); !reflect.DeepEqual(got, want) {
		t.Fatalf("dict: got %#v want %#v", got, want)
	}
}

func TestNumberHelpers(t *testing.T) {
	m := map[string]any{"i": int64(3), "f": 2.5, "s": "x", "b": true}
	if v, ok := Int(m, "i"); !ok || v != 3 {
		t.Fatalf("Int i = %v %v", v, ok)
	}
	if v, ok := Int(m, "f"); !ok || v != 2 {
		t.Fatalf("Int f = %v %v", v, ok)
	}
	if _, ok := Int(m, "s"); ok {
		t.Fatal("Int s should fail")
	}
	if v, ok := Float(m, "i"); !ok || v != 3 {
		t.Fatalf("Float i = %v %v", v, ok)
	}
	if v, ok := Bool(m, "b"); !ok || !v {
		t.Fatalf("Bool b = %v %v", v, ok)
	}
	if v := Str(m, "s"); v != "x" {
		t.Fatalf("Str s = %q", v)
	}
	if v := Str(m, "missing"); v != "" {
		t.Fatalf("Str missing = %q", v)
	}
}
