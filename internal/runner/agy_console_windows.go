//go:build windows

package runner

import (
	"errors"
	"os"
	"path/filepath"
	"os/exec"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

// On Windows agy reads a pasted sign-in code from its console (CONIN$),
// never from stdin. So the login runs agy in a console of its own, hidden,
// and types the owner's code there: attach to that console for a moment
// and write the keystrokes into its input.

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procAttachConsole      = kernel32.NewProc("AttachConsole")
	procFreeConsole        = kernel32.NewProc("FreeConsole")
	procWriteConsoleInputW = kernel32.NewProc("WriteConsoleInputW")
	// consoleMu: a process is attached to one console at a time.
	consoleMu sync.Mutex
)

// agyLoginConsole gives the login's agy a hidden console of its own.
func agyLoginConsole(cmd *exec.Cmd) {
	const createNewConsole = 0x00000010
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNewConsole}
}

// keyRecord is INPUT_RECORD holding a KEY_EVENT_RECORD.
type keyRecord struct {
	EventType uint16
	_         uint16
	KeyDown   int32
	Repeat    uint16
	VK        uint16
	Scan      uint16
	Char      uint16
	Control   uint32
}

// agyTypeLine types line and Enter into the console of process pid.
func agyTypeLine(pid int, line string) error {
	consoleMu.Lock()
	defer consoleMu.Unlock()
	procFreeConsole.Call()
	if r, _, err := procAttachConsole.Call(uintptr(uint32(pid))); r == 0 {
		return errors.New("não consegui falar com o console do agy: " + err.Error())
	}
	defer procFreeConsole.Call()
	name, _ := syscall.UTF16PtrFromString("CONIN$")
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		return errors.New("não consegui abrir a entrada do agy: " + err.Error())
	}
	defer syscall.CloseHandle(h)
	const keyEvent, vkReturn = 0x0001, 0x0D
	var recs []keyRecord
	for _, c := range utf16.Encode([]rune(line)) {
		recs = append(recs, keyRecord{EventType: keyEvent, KeyDown: 1, Repeat: 1, Char: c},
			keyRecord{EventType: keyEvent, KeyDown: 0, Repeat: 1, Char: c})
	}
	recs = append(recs, keyRecord{EventType: keyEvent, KeyDown: 1, Repeat: 1, VK: vkReturn, Char: '\r'},
		keyRecord{EventType: keyEvent, KeyDown: 0, Repeat: 1, VK: vkReturn, Char: '\r'})
	var written uint32
	if r, _, err := procWriteConsoleInputW.Call(uintptr(h), uintptr(unsafe.Pointer(&recs[0])), uintptr(len(recs)), uintptr(unsafe.Pointer(&written))); r == 0 {
		return errors.New("não consegui digitar o código no agy: " + err.Error())
	}
	return nil
}

var (
	noBrowserOnce sync.Once
	noBrowserDir  string
)

// agyNoBrowserDir holds a rundll32.cmd that does nothing: agy opens a
// sign-in page with "rundll32 url.dll,FileProtocolHandler <url>".
func agyNoBrowserDir() string {
	noBrowserOnce.Do(func() {
		base, err := os.UserConfigDir()
		if err != nil {
			return
		}
		d := filepath.Join(base, "Avora", "agy", "nobrowser")
		if os.MkdirAll(d, 0o700) != nil || os.WriteFile(filepath.Join(d, "rundll32.cmd"), []byte("@exit /b 0\r\n"), 0o600) != nil {
			return
		}
		noBrowserDir = d
	})
	return noBrowserDir
}
