package store

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// CredentialManager stores secrets as generic credentials in the user's
// Windows Credential Manager (DPAPI-encrypted, per user), one per key,
// named "<service>:<key>".
type CredentialManager struct{ service string }

func NewCredentialManager(service string) *CredentialManager {
	return &CredentialManager{service: service}
}

var (
	advapi32       = windows.NewLazySystemDLL("advapi32.dll")
	procCredReadW  = advapi32.NewProc("CredReadW")
	procCredWriteW = advapi32.NewProc("CredWriteW")
	procCredDelete = advapi32.NewProc("CredDeleteW")
	procCredFree   = advapi32.NewProc("CredFree")
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
)

// credential is CREDENTIALW.
type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

func (s *CredentialManager) target(key string) (*uint16, error) {
	return windows.UTF16PtrFromString(s.service + ":" + key)
}

func (s *CredentialManager) Get(key string) (string, error) {
	name, err := s.target(key)
	if err != nil {
		return "", err
	}
	var c *credential
	if r, _, err := procCredReadW.Call(uintptr(unsafe.Pointer(name)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&c))); r == 0 {
		if errors.Is(err, windows.ERROR_NOT_FOUND) {
			return "", ErrSecretNotFound
		}
		return "", err
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(c)))
	return string(unsafe.Slice(c.CredentialBlob, c.CredentialBlobSize)), nil
}

func (s *CredentialManager) Set(key, value string) error {
	name, err := s.target(key)
	if err != nil {
		return err
	}
	user, _ := windows.UTF16PtrFromString("hotwatch")
	blob := []byte(value)
	c := credential{Type: credTypeGeneric, TargetName: name, CredentialBlobSize: uint32(len(blob)),
		Persist: credPersistLocalMachine, UserName: user}
	if len(blob) > 0 {
		c.CredentialBlob = &blob[0]
	}
	if r, _, err := procCredWriteW.Call(uintptr(unsafe.Pointer(&c)), 0); r == 0 {
		return err
	}
	return nil
}

func (s *CredentialManager) Delete(key string) error {
	name, err := s.target(key)
	if err != nil {
		return err
	}
	if r, _, err := procCredDelete.Call(uintptr(unsafe.Pointer(name)), credTypeGeneric, 0); r == 0 && !errors.Is(err, windows.ERROR_NOT_FOUND) {
		return err
	}
	return nil
}
