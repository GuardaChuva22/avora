//go:build windows

package runner

import (
	"errors"
	"syscall"
	"unsafe"
)

// The one Windows credential agy reads its login from (go-keyring's
// "service:user" target), read and written with the Credential Manager API.

var (
	advapi32        = syscall.NewLazyDLL("advapi32.dll")
	procCredReadW   = advapi32.NewProc("CredReadW")
	procCredWriteW  = advapi32.NewProc("CredWriteW")
	procCredDeleteW = advapi32.NewProc("CredDeleteW")
	procCredFree    = advapi32.NewProc("CredFree")
)

// winCredential is CREDENTIALW.
type winCredential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        syscall.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
	errorNotFound           = syscall.Errno(1168)
)

const agyKeyringSupported = true

// readAgySlot returns agy's saved login, or nil when there is none.
func readAgySlot() ([]byte, error) {
	target, _ := syscall.UTF16PtrFromString(agySlotTarget)
	var cred *winCredential
	r, _, err := procCredReadW.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&cred)))
	if r == 0 {
		if errors.Is(err, errorNotFound) {
			return nil, nil
		}
		return nil, err
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(cred)))
	if cred.CredentialBlobSize == 0 || cred.CredentialBlob == nil {
		return nil, nil
	}
	return append([]byte(nil), unsafe.Slice(cred.CredentialBlob, cred.CredentialBlobSize)...), nil
}

// writeAgySlot saves blob as agy's login, the way go-keyring does.
func writeAgySlot(blob []byte) error {
	if len(blob) == 0 {
		return deleteAgySlot()
	}
	target, _ := syscall.UTF16PtrFromString(agySlotTarget)
	user, _ := syscall.UTF16PtrFromString(agySlotUser)
	cred := winCredential{Type: credTypeGeneric, TargetName: target, UserName: user, Persist: credPersistLocalMachine,
		CredentialBlobSize: uint32(len(blob)), CredentialBlob: &blob[0]}
	if r, _, err := procCredWriteW.Call(uintptr(unsafe.Pointer(&cred)), 0); r == 0 {
		return err
	}
	return nil
}

// deleteAgySlot signs agy out on this Windows user (no login is fine).
func deleteAgySlot() error {
	target, _ := syscall.UTF16PtrFromString(agySlotTarget)
	if r, _, err := procCredDeleteW.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0); r == 0 && !errors.Is(err, errorNotFound) {
		return err
	}
	return nil
}
