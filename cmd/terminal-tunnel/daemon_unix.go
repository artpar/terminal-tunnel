//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// setSysProcAttr sets Unix-specific process attributes for daemonization
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
}
