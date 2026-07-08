"""Shared data structures for hopmux.

Everything the discovery layer produces and the UI consumes is defined here so
the two never disagree on shape.
"""
from __future__ import annotations

from dataclasses import dataclass, field
from typing import List, Optional


@dataclass
class AgentSession:
    """A resumable Claude Code or Codex CLI session found on a host."""

    agent: str          # "claude" | "codex"
    sid: str            # session id used for `--resume <sid>` / `resume <sid>`
    cwd: str            # working directory the session lives in (the resume path)
    mtime: int          # last-modified epoch seconds
    title: str          # first human prompt (best-effort), for at-a-glance meaning
    host: str = ""      # filled in by discovery

    @property
    def project(self) -> str:
        p = (self.cwd or "").rstrip("/")
        if not p:
            return "?"
        return p.rsplit("/", 1)[-1] or p


@dataclass
class TmuxSession:
    """A live tmux session on a host (real terminals we can attach to)."""

    name: str
    windows: str
    attached: bool
    created: str
    host: str = ""


@dataclass
class Host:
    """One entry from ~/.ssh/config plus whatever we discovered on it."""

    name: str
    reachable: bool = False
    error: Optional[str] = None
    tmux: List[TmuxSession] = field(default_factory=list)
    agents: List[AgentSession] = field(default_factory=list)
    hostname: str = ""              # remote uname nodename, if probed
    now: int = 0                    # remote clock at probe time (for relative times)

    @property
    def session_count(self) -> int:
        return len(self.tmux) + len(self.agents)

    def recent_agents(self) -> List[AgentSession]:
        return sorted(self.agents, key=lambda a: a.mtime, reverse=True)
