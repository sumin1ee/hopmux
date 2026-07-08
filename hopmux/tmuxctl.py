"""tmux orchestration.

hopmux does not embed a terminal emulator. Instead it drives tmux *on the remote
host* — the battle-tested multiplexer already handles panes, splits, resizing and
persistence — and hands your terminal to `tmux attach` when you go in.

Everything here builds shell command strings meant to run on the remote via
`ssh.run_argv(host, cmd)`. Keeping them as strings (not argv) lets us compose
`cd ... && exec agent` cleanly through the remote shell.

The convention: hopmux parks all the sessions it opens inside one tmux session
per host, named `hopmux`. Splitting/closing act on that session's current window.
"""
from __future__ import annotations

import shlex

from .models import AgentSession

SESSION = "hopmux"


def resume_command(a: AgentSession) -> str:
    """The command that re-enters a Claude/Codex session in its own directory."""
    cwd = a.cwd or "~"
    cd = "cd %s 2>/dev/null; " % shlex.quote(cwd)
    if a.agent == "claude":
        return cd + "claude --resume %s" % shlex.quote(a.sid)
    if a.agent == "codex":
        # codex resumes by id; fall back to plain `codex` if the id is unknown.
        if a.sid:
            return cd + "codex resume %s" % shlex.quote(a.sid)
        return cd + "codex"
    return cd + "$SHELL"


def attach_session(name: str = SESSION) -> str:
    """Attach to a tmux session, creating it if absent (-A). Full-screen takeover."""
    return "tmux new-session -A -s %s" % shlex.quote(name)


def attach_existing(name: str) -> str:
    """Attach to a specific already-running tmux session by name."""
    return "tmux attach -t %s" % shlex.quote(name)


def _has_session() -> str:
    return "tmux has-session -t %s 2>/dev/null" % shlex.quote(SESSION)


def open_agent(a: AgentSession, window_name: str = "") -> str:
    """Land in the agent session inside a dedicated hopmux window, then attach.

    If the hopmux session doesn't exist yet, create it with this agent as its
    first window. If it already exists, add a new window for this agent. Either
    way we finish attached to it — one clean window per open, no duplicates.
    """
    wname = window_name or (a.project[:18] or a.agent)
    inner = resume_command(a)
    create = "tmux new-session -d -s %s -n %s %s" % (
        shlex.quote(SESSION), shlex.quote(wname), shlex.quote(inner))
    add_win = "tmux new-window -t %s -n %s %s" % (
        shlex.quote(SESSION), shlex.quote(wname), shlex.quote(inner))
    # if <session exists> then add a window else create the session
    body = "if %s; then %s; else %s; fi" % (_has_session(), add_win, create)
    return "%s; %s" % (body, attach_session(SESSION))


def split(a: AgentSession, vertical: bool = False) -> str:
    """Split the current window of the hopmux session and run this agent session
    in the new pane, then attach. vertical=False → side-by-side; True → stacked.

    Splitting assumes hopmux already exists (you split *from* something). If it
    somehow doesn't, fall back to opening the agent in a fresh session.
    """
    flag = "-v" if vertical else "-h"
    inner = resume_command(a)
    do_split = "tmux split-window %s -t %s %s" % (
        flag, shlex.quote(SESSION), shlex.quote(inner))
    create = "tmux new-session -d -s %s %s" % (shlex.quote(SESSION), shlex.quote(inner))
    body = "if %s; then %s; else %s; fi" % (_has_session(), do_split, create)
    return "%s; %s" % (body, attach_session(SESSION))


def new_shell(window_name: str = "shell") -> str:
    """Open a plain shell window in the hopmux session and attach."""
    add_win = "tmux new-window -t %s -n %s" % (shlex.quote(SESSION), shlex.quote(window_name))
    create = "tmux new-session -d -s %s -n %s" % (shlex.quote(SESSION), shlex.quote(window_name))
    body = "if %s; then %s; else %s; fi" % (_has_session(), add_win, create)
    return "%s; %s" % (body, attach_session(SESSION))


def kill_session(name: str = SESSION) -> str:
    return "tmux kill-session -t %s 2>/dev/null" % shlex.quote(name)
