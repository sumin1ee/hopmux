//go:build !windows

package main

import (
	"os/exec"

	pty "github.com/aymanbagabas/go-pty"
)

// hideConsole is a no-op off Windows: only Windows spawns a stray console window
// for a console-subsystem child. On macOS/Linux the pty child has no window.
func hideConsole(*pty.Cmd) {}

// hideWindowCmd is a no-op off Windows (used for one-shot exec.Command helpers
// like ssh-keygen). On Windows it suppresses the console flash.
func hideWindowCmd(*exec.Cmd) {}
