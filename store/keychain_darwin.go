//go:build darwin

package store

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"

	"hotwatch/internal/cf"
)

// OSStatus values from Security/SecBase.h.
const (
	errSecItemNotFound          = -25300
	errSecInteractionNotAllowed = -25308
	errSecUserCanceled          = -128
	errSecAuthFailed            = -25293
)

// errKeychainInteraction: the Keychain is locked, or the user refused access.
var errKeychainInteraction = errors.New("keychain access was not allowed")

var sec struct {
	once sync.Once
	err  error

	itemAdd           func(attrs cf.Ref, result uintptr) int32
	itemCopyMatching  func(query cf.Ref, result *cf.Ref) int32
	itemUpdate        func(query, attrs cf.Ref) int32
	itemDelete        func(query cf.Ref) int32
	dictCreate        func(alloc uintptr, keys, values *cf.Ref, n int, keyCB, valueCB uintptr) cf.Ref
	dataCreate        func(alloc uintptr, b *byte, n int) cf.Ref
	keyCB, valueCB    uintptr // &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks
	boolTrue          cf.Ref
	class, genericPwd cf.Ref
	service, account  cf.Ref
	valueData         cf.Ref
	returnData        cf.Ref
	matchLimit, one   cf.Ref
}

func loadSecurity() error {
	sec.once.Do(func() {
		if err := cf.Load(); err != nil {
			sec.err = err
			return
		}
		lib, err := purego.Dlopen("/System/Library/Frameworks/Security.framework/Security", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			sec.err = fmt.Errorf("keychain: open Security: %w", err)
			return
		}
		core := cf.Lib("CoreFoundation")
		purego.RegisterLibFunc(&sec.itemAdd, lib, "SecItemAdd")
		purego.RegisterLibFunc(&sec.itemCopyMatching, lib, "SecItemCopyMatching")
		purego.RegisterLibFunc(&sec.itemUpdate, lib, "SecItemUpdate")
		purego.RegisterLibFunc(&sec.itemDelete, lib, "SecItemDelete")
		purego.RegisterLibFunc(&sec.dictCreate, core, "CFDictionaryCreate")
		purego.RegisterLibFunc(&sec.dataCreate, core, "CFDataCreate")
		addr := func(h uintptr, name string) uintptr {
			p, e := purego.Dlsym(h, name)
			if e != nil && err == nil {
				err = fmt.Errorf("keychain: %s: %w", name, e)
			}
			return p
		}
		// A CF constant is a global variable holding the CFTypeRef.
		value := func(h uintptr, name string) cf.Ref {
			p := addr(h, name)
			if p == 0 {
				return 0
			}
			return **(**cf.Ref)(unsafe.Pointer(&p))
		}
		sec.keyCB = addr(core, "kCFTypeDictionaryKeyCallBacks")
		sec.valueCB = addr(core, "kCFTypeDictionaryValueCallBacks")
		sec.boolTrue = value(core, "kCFBooleanTrue")
		sec.class = value(lib, "kSecClass")
		sec.genericPwd = value(lib, "kSecClassGenericPassword")
		sec.service = value(lib, "kSecAttrService")
		sec.account = value(lib, "kSecAttrAccount")
		sec.valueData = value(lib, "kSecValueData")
		sec.returnData = value(lib, "kSecReturnData")
		sec.matchLimit = value(lib, "kSecMatchLimit")
		sec.one = value(lib, "kSecMatchLimitOne")
		sec.err = err
	})
	return sec.err
}

// Keychain is the Secrets of the app: generic passwords in the user's login
// Keychain, one per key, under one service name. Values never appear in
// errors.
type Keychain struct{ service string }

var _ Secrets = (*Keychain)(nil)

// NewKeychain returns the Keychain store for service ("Hotwatch" in the app).
func NewKeychain(service string) (*Keychain, error) {
	if err := loadSecurity(); err != nil {
		return nil, err
	}
	return &Keychain{service: service}, nil
}

// dict builds a CFDictionary from pairs, releasing the CFStrings and CFData
// it creates for them once the dictionary retains them.
type pairs struct {
	keys, values []cf.Ref
	owned        []cf.Ref
}

func (p *pairs) add(k, v cf.Ref) { p.keys, p.values = append(p.keys, k), append(p.values, v) }

func (p *pairs) str(k cf.Ref, s string) {
	r := cf.String(s)
	p.owned = append(p.owned, r)
	p.add(k, r)
}

func (p *pairs) build() cf.Ref {
	d := sec.dictCreate(0, &p.keys[0], &p.values[0], len(p.keys), sec.keyCB, sec.valueCB)
	for _, r := range p.owned {
		cf.Release(r)
	}
	return d
}

func (k *Keychain) query(key string) *pairs {
	p := &pairs{}
	p.add(sec.class, sec.genericPwd)
	p.str(sec.service, k.service)
	p.str(sec.account, key)
	return p
}

func statusErr(op string, st int32) error {
	switch st {
	case errSecItemNotFound:
		return ErrSecretNotFound
	case errSecInteractionNotAllowed, errSecUserCanceled, errSecAuthFailed:
		return fmt.Errorf("keychain %s: %w (OSStatus %d)", op, errKeychainInteraction, st)
	}
	return fmt.Errorf("keychain %s: OSStatus %d", op, st)
}

func (k *Keychain) Get(key string) (string, error) {
	p := k.query(key)
	p.add(sec.returnData, sec.boolTrue)
	p.add(sec.matchLimit, sec.one)
	q := p.build()
	defer cf.Release(q)
	var out cf.Ref
	if st := sec.itemCopyMatching(q, &out); st != 0 {
		return "", statusErr("read", st)
	}
	defer cf.Release(out)
	b, ok := cf.ToGo(out).([]byte)
	if !ok {
		return "", errors.New("keychain read: unexpected item type")
	}
	return string(b), nil
}

func (k *Keychain) Set(key, value string) error {
	var data cf.Ref
	if value == "" {
		data = sec.dataCreate(0, nil, 0)
	} else {
		b := []byte(value)
		data = sec.dataCreate(0, &b[0], len(b))
		clear(b)
	}
	defer cf.Release(data)

	q := k.query(key).build()
	defer cf.Release(q)
	up := &pairs{}
	up.add(sec.valueData, data)
	attrs := up.build()
	defer cf.Release(attrs)
	st := sec.itemUpdate(q, attrs)
	if st != errSecItemNotFound {
		if st != 0 {
			return statusErr("update", st)
		}
		return nil
	}
	p := k.query(key)
	p.add(sec.valueData, data)
	add := p.build()
	defer cf.Release(add)
	if st := sec.itemAdd(add, 0); st != 0 {
		return statusErr("add", st)
	}
	return nil
}

func (k *Keychain) Delete(key string) error {
	q := k.query(key).build()
	defer cf.Release(q)
	if st := sec.itemDelete(q); st != 0 && st != errSecItemNotFound {
		return statusErr("delete", st)
	}
	return nil
}
