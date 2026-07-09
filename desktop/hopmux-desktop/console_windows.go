//go:build windows

package main

import (
	pty "github.com/aymanbagabas/go-pty"
)

// hideConsole is intentionally a no-op.
//
// The ssh child here is launched through a go-pty *ConPTY*, which binds the
// child's stdio to a pseudoconsole — so it never pops a visible console window
// in the first place, even from a GUI app. Setting CREATE_NO_WINDOW (or any
// DETACHED_PROCESS-style flag) on a ConPTY child does NOT just hide a window:
// it breaks the pseudoconsole attachment, so ssh's output never reaches the pty
// and Read() blocks forever at 0 bytes — the "blank terminal" bug.
//
// (The separate ssh *probe* in core/discover is a plain exec.Command, not a
// ConPTY child, so it still uses CREATE_NO_WINDOW to suppress its console — see
// core/discover/hidewindow_windows.go. That path is unaffected by this.)
func hideConsole(cmd *pty.Cmd) {}
