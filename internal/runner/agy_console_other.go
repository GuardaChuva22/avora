//go:build !windows

package runner

import (
	"errors"
	"os/exec"
)

func agyLoginConsole(cmd *exec.Cmd) { hideWindow(cmd) }

// agyTypeLine is Windows only; elsewhere the code goes on stdin.
func agyTypeLine(int, string) error { return errors.New("no console to type into") }

func agyNoBrowserDir() string { return "" }
