# hopmux — Usage guide

hopmux gives you one window over every server in your `~/.ssh/config`, lists the
Claude Code / Codex / tmux sessions waiting on each, and opens any of them in a
tabbed terminal. This guide covers the desktop app; the CLI TUI is at the end.

---

## 1. First launch

On start, hopmux reads `~/.ssh/config` and scans every host concurrently. Each
server appears in the left sidebar with a colored status dot:

| Dot | Meaning |
|:---:|---|
| 🟢 green | reachable — sessions listed |
| 🟡 amber ⚿ | reachable but **needs interactive login** (password/key) |
| 🔴 red ✗ | unreachable (timeout / refused) |
| ◌ grey | not scanned yet |

The bottom of the sidebar shows overall status (`5/11 connected`).

> Scanning is **connection-safe**: it uses one reused SSH connection per host
> and never re-hammers a host that failed auth.

## 2. Browse & open a session

1. **Click a server** in the sidebar → its sessions appear in the middle:
   - `▣ tmux` — live tmux sessions (with attached/detached state)
   - `◉ claude` / `◉ codex` — resumable agent sessions, newest first, each with
     its **working directory** and first prompt (so an opaque title still tells
     you *which* project).
2. **Click a session** → it opens in a **terminal tab** on the right.
   - Claude/Codex sessions resume in place (`claude --resume`, `codex resume`).
   - tmux sessions attach to the live session.

The **★ Recent sessions** entry (top of the sidebar) shows the most recently
touched sessions across *all* servers — handy for "get me back to what I was
doing."

## 3. Tabs

Open as many sessions as you want — each is a tab.

| Action | How |
|---|---|
| New session | click **＋** in the tab bar (opens the picker) |
| Switch tab | click the tab |
| Close tab | tab's **✕**, or `⌘W` |

## 4. Inside a session (real tmux)

Agent and tmux sessions run inside `tmux` on the remote, so tmux's own keys work:

| Action | Keys |
|---|---|
| Split left/right | `Ctrl-B` then `%` |
| Split top/bottom | `Ctrl-B` then `"` |
| Move between panes | `Ctrl-B` then arrow keys |
| Detach (leave it running) | `Ctrl-B` then `d`, or `⌘W` |

Because it's real tmux, closing the tab or quitting hopmux leaves the session
**running** — reopen it later and you're right back where you were.

## 5. Keyboard shortcuts (app)

App shortcuts use **⌘ (macOS)** / **Alt (Windows)** and work even while a
terminal is focused:

| Shortcut | Action |
|---|---|
| `⌘B` | show / hide the sidebar |
| `⌘G` | toggle GPU utilization (per host) |
| `⌘D` | dark / light theme |
| `⌘R` | rescan all servers |
| `⌘W` | close the current tab |
| `⌘ +` / `⌘ -` / `⌘ 0` | zoom the terminal font in / out / reset |
| `Esc` | close an open dialog (Settings / Add server) |

Arrow keys / `Enter` navigate the session picker when no terminal is focused.

## 6. GPU view

Press `⌘G` to reveal GPU load. Each server shows how many GPUs are busy and the
busiest one's utilization (`3/8 · 89%`), colored green→amber→red. Select a
server to see per-GPU detail in the header. Great for "which box is free?"

Requires `nvidia-smi` on the remote; hosts without it simply show nothing.

## 7. Add a server

Sidebar → **＋ Add server**. Fill in Alias / HostName / Port / User → it appends
a `Host` block to `~/.ssh/config` and rescans. (Your existing config is left
untouched apart from the appended block.)

## 8. Settings

Sidebar → **⚙ Settings**:

- **Theme** and **terminal font size**
- **Auto-refresh** interval — reachable servers are re-probed periodically so
  GPU % and session counts stay live (set to `0` to disable). Only already-
  connected hosts are refreshed, so it never risks locking you out.
- **Per-host scan timeout**
- **Edit `~/.ssh/config`** directly (a backup is written to
  `~/.ssh/config.hopmux.bak` before saving).

## 9. Window

- Drag the **title bar** to move the window.
- **Double-click** the title bar to maximize / restore.

## 10. A host that needs login

If a host shows ⚿ **needs login**, click it → **click to log in**. A terminal
opens for you to type your password/passphrase. Once authenticated, hopmux warms
the connection, re-probes the host, and its sessions appear automatically.

---

## CLI / TUI

The same engine ships as a terminal app:

```bash
hopmux            # dashboard over ~/.ssh/config
hopmux --demo     # built-in mock data, no servers needed
hopmux --list     # print the inventory as text and exit
hopmux --only a,b # limit to specific hosts
hopmux --timeout 8
```

In the TUI: `↑↓`/`jk` move, `→`/`Enter` open, `b` sidebar, `g` GPU, `d` theme,
`/` filter, `s`/`v` split, `n` new session, `r` rescan, `q` quit.
