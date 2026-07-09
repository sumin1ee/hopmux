<div align="center">

<img src="docs/icon.png" width="96" alt="hopmux"/>

# hopmux

**Hop across your SSH servers and resume any Claude Code or Codex session — instantly.**

*tmux for jumping across servers.*

<img src="docs/teaser.png" alt="hopmux" width="820"/>

</div>

---

## What is it

You run work across a handful of machines — a lab workstation, a couple of GPU boxes, whatever's in your `~/.ssh/config`. On each one you've got several **Claude Code** or **Codex** sessions going: one debugging a dataloader, one refactoring a model, one you started three days ago and half-forgot.

Getting back to any single one means: remember which server → `ssh` in → hunt through `tmux` or `claude --resume`'s list → squint at the titles → hope you picked the right one. Times five servers. Every day.

**hopmux collapses that to two clicks.** It reads your SSH config, connects to every host at once, and shows every session waiting on each — with the working directory it lives in, so an opaque title still tells you *which* thing. Pick one, and you're in — on the right host, in the right directory, in the right conversation.

## Who it's for

- You **SSH into more than one or two machines** and lose track of what's running where.
- You use **Claude Code and/or [Codex CLI](https://github.com/openai/codex)** and run **several sessions in parallel**.
- You live in `tmux` and want a **fast index over all of it**, across hosts.
- Researchers, ML/CV grad students, anyone juggling many remote experiments.

## Features

- **One window over every host in `~/.ssh/config`.** Reachable hosts light up; unreachable/needs-login ones are marked with the reason.
- **Finds your AI sessions automatically** — scans `~/.claude/projects` (Claude Code) and `~/.codex/sessions` (Codex) on each host, newest first, each with its **resume path** and first prompt.
- **Tabbed terminals** — open several sessions at once, switch between them, split with real `tmux` (`Ctrl-B %`).
- **Live tmux sessions** listed too, with attached state.
- **GPU at a glance** — toggle to see each host's GPU utilization (great for "which box is free?").
- **Native desktop app** (macOS, Windows via Wails) — its own window, its own terminal, not bound to your shell — *and* a **standalone terminal TUI**.
- **Dark & light**, keyboard-driven, colored (Claude coral, Codex cyan, tmux green).
- **Connection-safe** — probes over one reused SSH connection per host (`ControlMaster`); never re-hammers a host that failed auth.

## Install

### Desktop app (macOS)

Requires [Go](https://go.dev), [Node](https://nodejs.org), and the [Wails CLI](https://wails.io).

```bash
git clone https://github.com/sumin1ee/hopmux
cd hopmux
go install github.com/wailsapp/wails/v2/cmd/wails@latest
./desktop/build-mac.sh          # → desktop/hopmux-desktop/build/bin/hopmux.app
open desktop/hopmux-desktop/build/bin/hopmux.app
```

### CLI / TUI

```bash
go install github.com/sumin1ee/hopmux@latest
hopmux            # the terminal dashboard
hopmux --demo     # try it with built-in mock data (no servers needed)
```

**On the remote hosts** you want full functionality, you need `python3` (for session discovery) and `tmux`. `claude` / `codex` must be installed there for resume to launch.

## Usage

Pick a server on the left → its sessions appear → click one to open it as a tab.

| | |
|---|---|
| Open a session | click it (opens a tab) |
| Split the terminal | `Ctrl-B %` / `Ctrl-B "` (real tmux) |
| Close a tab | `⌘W` or the tab's ✕ |
| Toggle sidebar | `⌘B` |
| Toggle GPU view | `⌘G` |
| Dark / light | `⌘D` |
| Rescan | `⌘R` |
| Zoom in / out | `⌘+` / `⌘-` / `⌘0` |
| Add a server / edit config | sidebar → **Add server** / **Settings** |

## How it works

hopmux isn't a terminal emulator in the traditional sense — it's a controller. It reads `~/.ssh/config`, probes every host concurrently over a reused SSH connection, and inventories each host's `tmux` sessions plus Claude Code / Codex sessions (by scanning their session files). Opening a session hands a terminal to `ssh -t <host> tmux …`, so panes, splits, and persistence come from battle-tested `tmux`. The desktop app hosts that terminal in its own native window (via [Wails](https://wails.io) + [xterm.js](https://xtermjs.org)).

```
┌── ~/.ssh/config ──┐   concurrent, connection-safe    ┌── on each host ──┐
│ ml-train-01       │  ───────────────────────────▶   │ tmux ls          │
│ prod-api          │   (one reused ControlMaster/host)│ ~/.claude/projects│
│ research-box …    │  ◀───────────────────────────   │ ~/.codex/sessions │
└───────────────────┘         session inventory        └──────────────────┘
```

## Project layout

```
core/        reusable engine — ssh config, discovery/probe, tmux command builders, models
internal/ui/ the terminal TUI (Bubble Tea)
desktop/     the native desktop app (Wails: Go backend + xterm.js frontend)
main.go      the CLI / TUI entry point
```

## Status

Early but usable. macOS desktop app is the primary target; Windows builds compile but need real-hardware testing. Contributions welcome.

## License

MIT — see [LICENSE](LICENSE). Bundled font [D2Coding](https://github.com/naver/d2codingfont) is under the SIL Open Font License.
