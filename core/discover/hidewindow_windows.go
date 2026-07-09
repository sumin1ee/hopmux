//go:build windows

package discover

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// hideWindow keeps the ssh probe from popping a console window on each scan.
// The probe is a console-subsystem process spawned from a GUI app (hopmux
// desktop), so without this Windows opens a visible console for it every time.
func hideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
}
