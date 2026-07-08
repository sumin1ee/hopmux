"""Concurrent host inventory: fan out the probe over SSH, parse TSV into models."""
from __future__ import annotations

import subprocess
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from typing import Callable, List, Optional

from . import ssh
from .models import AgentSession, Host, TmuxSession
from .probe import REMOTE_SH


def _clean_error(stderr: bytes, returncode: int) -> str:
    lines = stderr.decode("utf-8", "replace").strip().splitlines()
    msg = ""
    for ln in reversed(lines):
        s = ln.strip()
        if not s:
            continue
        low = s.lower()
        if "pseudo-terminal" in low or low.startswith("warning: permanently added"):
            continue
        msg = s
        break
    low = msg.lower()
    if "is not recognized" in low or "command not found" in low or msg == "Python":
        msg = "no shell/python on remote"
    return (msg or "ssh exit %d" % returncode)[:110]


def probe_host(name: str, timeout: int, local: bool = False) -> Host:
    h = Host(name)
    try:
        if local:
            argv = ["sh", "-"]
            extra = 20
        else:
            argv = ssh.probe_argv(name, timeout)
            extra = 25
        proc = subprocess.run(argv, input=REMOTE_SH.encode(),
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                              timeout=timeout + extra)
    except subprocess.TimeoutExpired:
        h.error = "timeout"
        return h
    except Exception as e:  # noqa: BLE001
        h.error = repr(e)[:110]
        return h

    if proc.returncode != 0 and not proc.stdout.strip():
        h.error = _clean_error(proc.stderr, proc.returncode)
        return h

    for ln in proc.stdout.decode("utf-8", "replace").splitlines():
        p = ln.split("\t")
        if not p:
            continue
        tag = p[0]
        if tag == "T" and len(p) == 5:
            h.tmux.append(TmuxSession(name=p[1], windows=p[2],
                                      attached=(p[3] == "1"), created=p[4], host=name))
        elif tag == "A" and len(p) == 6:
            try:
                mt = int(p[4] or 0)
            except ValueError:
                mt = 0
            h.agents.append(AgentSession(agent=p[1], sid=p[2], cwd=p[3],
                                         mtime=mt, title=p[5], host=name))
        elif tag == "M" and len(p) == 3:
            if p[1] == "host":
                h.hostname = p[2]
            elif p[1] == "now":
                try:
                    h.now = int(p[2])
                except ValueError:
                    pass
    h.reachable = True
    return h


def discover(hosts: List[str], timeout: int = 6, local_name: Optional[str] = None,
             workers: int = 12,
             on_done: Optional[Callable[[Host], None]] = None) -> List[Host]:
    """Probe every host concurrently. `on_done` (if given) is called from worker
    threads as each host finishes — handy for streaming results into a UI."""
    results = {}
    with ThreadPoolExecutor(max_workers=workers) as ex:
        futs = {ex.submit(probe_host, n, timeout, n == local_name): n for n in hosts}
        for fut in as_completed(futs):
            h = fut.result()
            results[h.name] = h
            if on_done:
                try:
                    on_done(h)
                except Exception:
                    pass
    return [results[n] for n in hosts if n in results]


def all_recent_agents(hosts: List[Host], limit: int = 40) -> List[AgentSession]:
    """Flatten every host's agent sessions, newest first — the default view when
    no server is selected."""
    flat: List[AgentSession] = []
    for h in hosts:
        flat.extend(h.agents)
    flat.sort(key=lambda a: a.mtime, reverse=True)
    return flat[:limit]


def now_epoch() -> int:
    return int(time.time())
