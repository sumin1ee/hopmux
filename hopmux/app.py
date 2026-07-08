"""The hopmux Textual application.

Left panel  = SSH servers (from ~/.ssh/config).
Right panel = the selected server's sessions (live tmux + resumable Claude/Codex),
              or, when nothing is selected, the most recent agent sessions across
              every host so you can jump straight back into what you last touched.

hopmux never embeds a terminal. Opening / splitting a session suspends the TUI,
hands your real terminal to `ssh -t <host> tmux ...`, and restores the TUI when
you detach. tmux owns panes, splits and persistence; hopmux owns discovery,
navigation and layout intent.

Both panels use OptionList (not ListView): its clear/rebuild is synchronous, so
rapid navigation and streaming discovery updates can't race a deferred clear.
Each option carries a Rich renderable (colored, two lines) plus a stable id we
map back to the underlying host / session.
"""
from __future__ import annotations

import subprocess
from typing import Dict, List, Optional

from rich.console import Group
from rich.text import Text
from textual import work
from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Vertical
from textual.widgets import Footer, Input, Label, OptionList, Static
from textual.widgets.option_list import Option

from . import discovery, ssh, tmuxctl
from .models import AgentSession, Host, TmuxSession
from .theme import CLAUDE, CODEX, DANGER, TMUX, agent_color, hopmux_dark, hopmux_light


def _rel(epoch: int, now: int) -> str:
    if not epoch:
        return "?"
    d = max(0, now - epoch)
    if d < 60:
        return "%ds" % d
    if d < 3600:
        return "%dm" % (d // 60)
    if d < 86400:
        return "%dh" % (d // 3600)
    if d < 86400 * 30:
        return "%dd" % (d // 86400)
    return "%dmo" % (d // (86400 * 30))


class HopmuxApp(App):
    CSS_PATH = "app.tcss"
    TITLE = "hopmux"

    BINDINGS = [
        Binding("enter", "open", "open", show=True),
        Binding("s", "split", "split", show=True),
        Binding("v", "split_v", "split ↕", show=True),
        Binding("x", "close_pane", "close", show=True),
        Binding("n", "new_shell", "new shell", show=True),
        Binding("r", "rescan", "rescan", show=True),
        Binding("d", "toggle_theme", "light/dark", show=True),
        Binding("slash", "filter", "filter", show=True),
        Binding("tab", "focus_next_pane", "focus", show=False),
        Binding("left", "focus_servers", "", show=False),
        Binding("right", "focus_sessions", "", show=False),
        Binding("q", "quit", "quit", show=True),
        Binding("escape", "clear_filter", "", show=False),
    ]

    def __init__(self, host_names: List[str], timeout: int = 6,
                 local_name: Optional[str] = None):
        super().__init__()
        self.host_names = host_names
        self.timeout = timeout
        self.local_name = local_name
        self.now = discovery.now_epoch()
        self.hosts: List[Host] = []
        self._filter = ""
        self._current_host: Optional[Host] = None
        self._showing_recent = True
        # id -> payload maps for whatever is currently displayed
        self._server_by_id: Dict[str, Optional[Host]] = {}
        self._session_by_id: Dict[str, object] = {}

    # ---------------- layout ----------------
    def compose(self) -> ComposeResult:
        with Vertical(id="sidebar"):
            yield Label("  SERVERS", id="sidebar-title")
            yield OptionList(id="server-list")
        with Vertical(id="main"):
            yield Static("", id="main-title")
            yield Static("", id="hint")
            yield OptionList(id="session-list")
        yield Input(placeholder="filter sessions…", id="filter")
        yield Footer()

    def on_mount(self) -> None:
        self.register_theme(hopmux_dark)
        self.register_theme(hopmux_light)
        self.theme = "hopmux-dark"
        self._rebuild_sidebar()
        self.set_focus(self.query_one("#server-list", OptionList))
        self._render_right()
        self.call_after_refresh(self.run_discovery)

    # ---------------- discovery ----------------
    @work(thread=True, exclusive=True)
    def run_discovery(self) -> None:
        def done(h: Host):
            self.call_from_thread(self._host_updated, h)
        result = discovery.discover(self.host_names, self.timeout,
                                    self.local_name, on_done=done)
        self.call_from_thread(self._discovery_finished, result)

    def _host_updated(self, h: Host) -> None:
        replaced = False
        for i, existing in enumerate(self.hosts):
            if existing.name == h.name:
                self.hosts[i] = h
                replaced = True
                break
        if not replaced:
            self.hosts.append(h)
        self._rebuild_sidebar(preserve=True)
        if self._showing_recent:
            self._render_right()
        elif self._current_host and self._current_host.name == h.name:
            self._current_host = h
            self._render_right()

    def _discovery_finished(self, result: List[Host]) -> None:
        self.hosts = result
        self.now = discovery.now_epoch()
        self._rebuild_sidebar(preserve=True)
        self._render_right()

    # ---------------- sidebar ----------------
    def _server_label(self, h: Optional[Host], recent: bool) -> Text:
        if recent:
            t = Text()
            t.append("★ ", style=f"bold {CLAUDE}")
            t.append("Recent sessions")
            return t
        assert h is not None
        t = Text()
        if not h.reachable and h.error is None:
            t.append("◌ ", style="dim")
            t.append(h.name, style="dim")
            t.append("  ···", style="dim")
            return t
        if not h.reachable:
            t.append("✗ ", style=f"{DANGER}")
            t.append(h.name, style=f"{DANGER} dim")
            return t
        if h.session_count:
            t.append("● ", style=TMUX)
            t.append(h.name)
            t.append("  %d" % h.session_count, style="dim")
        else:
            t.append("○ ", style="dim")
            t.append(h.name, style="dim")
        return t

    def _rebuild_sidebar(self, preserve: bool = False) -> None:
        ol = self.query_one("#server-list", OptionList)
        idx = ol.highlighted if preserve else None
        ol.clear_options()
        self._server_by_id = {}
        rid = "server::__recent__"
        self._server_by_id[rid] = None
        ol.add_option(Option(self._server_label(None, recent=True), id=rid))
        existing = {h.name: h for h in self.hosts}
        for name in self.host_names:
            h = existing.get(name) or Host(name)
            oid = "server::" + name
            self._server_by_id[oid] = h
            ol.add_option(Option(self._server_label(h, recent=False), id=oid))
        if idx is not None and idx < ol.option_count:
            ol.highlighted = idx
        elif ol.option_count:
            ol.highlighted = 0

    # ---------------- right pane ----------------
    def _set_titles(self, title: str, hint: str) -> None:
        self.query_one("#main-title", Static).update(title)
        self.query_one("#hint", Static).update(hint)

    def _passes(self, text: str) -> bool:
        return self._filter.lower() in text.lower() if self._filter else True

    def _session_renderable(self, kind: str, obj, now: int, host_prefix: str = "") -> Text:
        # no_wrap + ellipsis keeps every row exactly two lines regardless of how
        # long a prompt or path is — otherwise long titles wrap and rows jump.
        t = Text(no_wrap=True, overflow="ellipsis")
        if kind == "tmux":
            tm: TmuxSession = obj
            t.append("▣ tmux ", style=f"bold {TMUX}")
            t.append(tm.name, style="bold")
            t.append("\n")
            t.append("   %s windows · " % tm.windows, style="dim")
            t.append("attached" if tm.attached else "detached",
                     style=TMUX if tm.attached else "dim")
            return t
        a: AgentSession = obj
        col = agent_color(a.agent)
        t.append("◉ %s " % ("claude" if a.agent == "claude" else "codex"),
                 style=f"bold {col}")
        if host_prefix:
            t.append("%s " % host_prefix, style="dim")
        title = " ".join((a.title or "").split()) or "(no prompt yet)"
        t.append(title, style="" if a.title.strip() else "dim italic")
        t.append("\n")
        t.append("   %s" % (a.cwd or "~"), style=f"{col}")
        t.append("  · %s" % _rel(a.mtime, now), style="dim")
        return t

    def _render_right(self) -> None:
        """Synchronous, atomic rebuild of the right pane — no async clear to race."""
        ol = self.query_one("#session-list", OptionList)
        ol.clear_options()
        self._session_by_id = {}

        if self._showing_recent or self._current_host is None:
            self._fill_recent(ol)
        else:
            self._fill_host(ol, self._current_host)

        if ol.option_count:
            ol.highlighted = 0

    def _fill_recent(self, ol: OptionList) -> None:
        recent = discovery.all_recent_agents(self.hosts, limit=60)
        scanned = sum(1 for h in self.hosts if h.reachable)
        self._set_titles(
            "[b]★ Recent sessions[/b]  [dim]across all servers[/dim]",
            "pick where to jump back in · %d/%d servers scanned"
            % (scanned, len(self.host_names)))
        n = 0
        for i, a in enumerate(recent):
            if not self._passes("%s %s %s %s" % (a.host, a.agent, a.project, a.title)):
                continue
            oid = "sess::recent::%d" % i
            self._session_by_id[oid] = ("agent", a, a.host)
            ol.add_option(Option(self._session_renderable("agent", a, self.now,
                                                          host_prefix="[%s]" % a.host),
                                  id=oid))
            n += 1
        if n == 0:
            ol.add_option(Option(Text("scanning servers…  sessions will appear here",
                                      style="dim")))
            ol.disable_option_at_index(0)

    def _fill_host(self, ol: OptionList, h: Host) -> None:
        if not h.reachable:
            self._set_titles("[b]%s[/b]" % h.name,
                             "[#f85149]unreachable[/#f85149]: %s" % (h.error or "?"))
            ol.add_option(Option(Text(
                "can't reach this host for a session list.\n"
                "you can still press Enter to try an interactive ssh.", style="dim")))
            self._session_by_id["sess::ssh"] = ("ssh", None, h.name)
            return
        self._set_titles(
            "[b]%s[/b]  [dim]%s[/dim]" % (h.name, h.hostname or ""),
            "%d tmux · %d claude · %d codex   —  Enter open · s split · x close"
            % (len(h.tmux),
               sum(1 for a in h.agents if a.agent == "claude"),
               sum(1 for a in h.agents if a.agent == "codex")))
        n = 0
        for i, tm in enumerate(sorted(h.tmux, key=lambda x: x.name)):
            if not self._passes("tmux %s" % tm.name):
                continue
            oid = "sess::tmux::%d" % i
            self._session_by_id[oid] = ("tmux", tm, h.name)
            ol.add_option(Option(self._session_renderable("tmux", tm, h.now or self.now),
                                  id=oid))
            n += 1
        for i, a in enumerate(h.recent_agents()):
            if not self._passes("%s %s %s" % (a.agent, a.project, a.title)):
                continue
            oid = "sess::agent::%d" % i
            self._session_by_id[oid] = ("agent", a, h.name)
            ol.add_option(Option(self._session_renderable("agent", a, h.now or self.now),
                                  id=oid))
            n += 1
        if n == 0:
            ol.add_option(Option(Text("no tmux or agent sessions here yet.\n"
                                      "press n to open a fresh shell.", style="dim")))
            ol.disable_option_at_index(0)

    # ---------------- events ----------------
    def on_option_list_option_highlighted(
            self, event: OptionList.OptionHighlighted) -> None:
        if event.option_list.id != "server-list":
            return
        oid = event.option_id
        if oid is None:
            return
        if oid == "server::__recent__":
            self._showing_recent = True
            self._current_host = None
        else:
            self._showing_recent = False
            self._current_host = self._server_by_id.get(oid)
        self._render_right()

    def on_option_list_option_selected(
            self, event: OptionList.OptionSelected) -> None:
        if event.option_list.id == "server-list":
            self.action_focus_sessions()
        elif event.option_list.id == "session-list":
            self.action_open()

    # ---------------- selection helpers ----------------
    def _selected_session(self):
        ol = self.query_one("#session-list", OptionList)
        idx = ol.highlighted
        if idx is None:
            return None
        try:
            opt = ol.get_option_at_index(idx)
        except Exception:
            return None
        return self._session_by_id.get(opt.id) if opt.id else None

    def _run_remote(self, host: str, remote_cmd: str) -> None:
        """Suspend the TUI and hand the terminal to ssh. Restore on return."""
        if not host:
            self.bell()
            return
        argv = ssh.run_argv(host, remote_cmd, tty=True)
        with self.suspend():
            try:
                subprocess.call(argv)
            except Exception:
                pass
        self.refresh(layout=True)

    # ---------------- actions ----------------
    def action_open(self) -> None:
        sel = self._selected_session()
        if sel is None:
            self.bell()
            return
        kind, obj, host = sel
        if kind == "tmux":
            self._run_remote(host, tmuxctl.attach_existing(obj.name))
        elif kind == "agent":
            self._run_remote(host, tmuxctl.open_agent(obj))
        elif kind == "ssh":
            self._run_remote(host, "$SHELL -l")

    def action_split(self) -> None:
        self._do_split(vertical=False)

    def action_split_v(self) -> None:
        self._do_split(vertical=True)

    def _do_split(self, vertical: bool) -> None:
        sel = self._selected_session()
        if sel is None or sel[0] != "agent":
            self.notify("select a Claude/Codex session to split into a new pane",
                        severity="warning", timeout=3)
            return
        _, a, host = sel
        self._run_remote(host, tmuxctl.split(a, vertical=vertical))

    def action_new_shell(self) -> None:
        host = self._current_host.name if self._current_host else ""
        if not host:
            self.notify("select a server first to open a shell on it",
                        severity="warning", timeout=3)
            return
        self._run_remote(host, tmuxctl.new_shell())

    def action_close_pane(self) -> None:
        host = self._current_host.name if self._current_host else ""
        sel = self._selected_session()
        if sel:
            host = sel[2] or host
        if not host:
            self.notify("select a server whose hopmux pane you want to close",
                        severity="warning", timeout=3)
            return
        self._run_remote(
            host,
            "tmux kill-pane -t %s 2>/dev/null || echo 'no hopmux pane to close'; "
            "sleep 0.4" % tmuxctl.SESSION)

    def action_rescan(self) -> None:
        self.now = discovery.now_epoch()
        self.notify("rescanning servers…", timeout=2)
        self.run_discovery()

    def action_toggle_theme(self) -> None:
        self.theme = "hopmux-light" if self.theme == "hopmux-dark" else "hopmux-dark"

    # ---------------- filter ----------------
    def action_filter(self) -> None:
        f = self.query_one("#filter", Input)
        f.add_class("visible")
        self.set_focus(f)

    def action_clear_filter(self) -> None:
        f = self.query_one("#filter", Input)
        if f.has_focus or self._filter:
            self._filter = ""
            f.value = ""
            f.remove_class("visible")
            self._render_right()
            self.set_focus(self.query_one("#session-list", OptionList))

    def on_input_changed(self, event: Input.Changed) -> None:
        if event.input.id == "filter":
            self._filter = event.value
            self._render_right()

    def on_input_submitted(self, event: Input.Submitted) -> None:
        if event.input.id == "filter":
            self.set_focus(self.query_one("#session-list", OptionList))

    # ---------------- focus ----------------
    def action_focus_servers(self) -> None:
        self.set_focus(self.query_one("#server-list", OptionList))

    def action_focus_sessions(self) -> None:
        ol = self.query_one("#session-list", OptionList)
        if ol.option_count:
            self.set_focus(ol)

    def action_focus_next_pane(self) -> None:
        sl = self.query_one("#server-list", OptionList)
        ss = self.query_one("#session-list", OptionList)
        self.set_focus(ss if self.focused is sl else sl)
