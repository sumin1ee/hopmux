//go:build !windows

package discover

import "os/exec"

// hideWindow is a no-op off Windows: only Windows opens a stray console window
// for a console-subsystem child spawned from a GUI process.
func hideWindow(*exec.Cmd) {}
