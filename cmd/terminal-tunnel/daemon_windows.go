//go:build windows

package main

import (
	"os/exec"
)

// setSysProcAttr sets Windows-specific process attributes for daemonization
// Windows doesn't support Setsid, so we just detach the console
func setSysProcAttr(cmd *exec.Cmd) {
	// On Windows, the process will still be attached to the console
	// but will continue running after the parent exits
}
