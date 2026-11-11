//go:build windows

package app

import (
	"os"
	"os/exec"
)

func detachDaemonProcess(cmd *exec.Cmd) {}

func signalProcessTerm(pid int) {
	if pid <= 0 {
		return
	}
	if p, err := os.FindProcess(pid); err == nil && p != nil {
		_ = p.Kill()
	}
}
