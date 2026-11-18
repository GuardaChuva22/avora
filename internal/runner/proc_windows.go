package runner

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps agent processes from opening a console window: the
// runtime itself is built without one (-H=windowsgui).
func hideWindow(cmd *exec.Cmd) {
	const createNoWindow = 0x08000000
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
