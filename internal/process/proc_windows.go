//go:build windows

package process

import "os/exec"

func setDetachedProcessGroup(cmd *exec.Cmd) {}
