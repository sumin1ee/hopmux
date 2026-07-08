"""SSH plumbing: one multiplexed, reused connection per host.

The first connection to a host authenticates; ControlMaster + ControlPersist
keep it warm so the inventory probe and the subsequent attach don't re-auth.

The control-socket path MUST stay short — a Unix socket's sun_path caps around
104 chars, and the default macOS tempdir (/var/folders/.../T/) already eats most
of it. We anchor under a short stable dir and use %C (a fixed-length hash of the
connection tuple) as the socket token.
"""
from __future__ import annotations

import os
import tempfile
from typing import List

_CTRL_DIR = None


def _control_dir() -> str:
    global _CTRL_DIR
    if _CTRL_DIR is None:
        try:
            uid = os.getuid()  # POSIX
            base = "/tmp/hopmux-%d" % uid
        except AttributeError:
            base = os.path.join(tempfile.gettempdir(), "hopmux")
        try:
            os.makedirs(base, mode=0o700, exist_ok=True)
            _CTRL_DIR = base
        except Exception:
            _CTRL_DIR = tempfile.mkdtemp(prefix="hopmux-")
    return _CTRL_DIR


def mux_opts(persist: str = "120s") -> List[str]:
    """ControlMaster options shared by every ssh invocation in a run."""
    if os.name == "nt":
        # Windows OpenSSH does not support ControlMaster; skip multiplexing.
        return []
    return [
        "-o", "ControlMaster=auto",
        "-o", "ControlPath=" + os.path.join(_control_dir(), "%C"),
        "-o", "ControlPersist=" + persist,
    ]


def probe_argv(host: str, timeout: int) -> List[str]:
    """Non-interactive probe: never hang on a prompt, fail fast."""
    return (["ssh"] + mux_opts() + [
        "-o", "BatchMode=yes",
        "-o", "ConnectTimeout=" + str(timeout),
        "-o", "StrictHostKeyChecking=accept-new",
        host, "sh", "-",
    ])


def run_argv(host: str, remote_cmd: str, tty: bool = True) -> List[str]:
    """Interactive command on a host (attach / tmux control).

    TTY allocated so full-screen apps (tmux, claude, codex) work; BatchMode is
    NOT set so key/password/2FA prompts still function when going in for real.
    """
    argv = ["ssh"] + mux_opts()
    if tty:
        argv.append("-t")
    argv += ["-o", "ConnectTimeout=10", host, remote_cmd]
    return argv
