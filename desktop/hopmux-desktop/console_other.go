//go:build !windows

package main

import pty "github.com/aymanbagabas/go-pty"

// hideConsole is a no-op off Windows: only Windows spawns a stray console window
// for a console-subsystem child. On macOS/Linux the pty child has no window.
func hideConsole(*pty.Cmd) {}
