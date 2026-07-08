"""Parse ~/.ssh/config into a list of concrete host aliases.

Wildcard patterns (Host *, ?, negations) are skipped — you can't attach to a
pattern. `Include` directives are followed one level deep, which is how a lot
of people split their config across files.
"""
from __future__ import annotations

import glob
import os
import re
from typing import List

_HOST_RE = re.compile(r"^\s*host\s+(.+?)\s*$", re.IGNORECASE)
_INCLUDE_RE = re.compile(r"^\s*include\s+(.+?)\s*$", re.IGNORECASE)


def _expand_include(arg: str, base_dir: str) -> List[str]:
    paths: List[str] = []
    for token in arg.split():
        token = os.path.expanduser(token)
        if not os.path.isabs(token):
            token = os.path.join(base_dir, token)
        paths.extend(sorted(glob.glob(token)))
    return paths


def parse_hosts(config_path: str = "~/.ssh/config", _depth: int = 0) -> List[str]:
    path = os.path.expanduser(config_path)
    hosts: List[str] = []
    seen = set()
    base_dir = os.path.dirname(path)
    try:
        with open(path, "r", errors="replace") as fh:
            lines = fh.readlines()
    except FileNotFoundError:
        return hosts

    for raw in lines:
        line = raw.strip()
        if not line or line.startswith("#"):
            continue

        inc = _INCLUDE_RE.match(line)
        if inc and _depth < 3:
            for sub in _expand_include(inc.group(1), base_dir):
                for h in parse_hosts(sub, _depth + 1):
                    if h not in seen:
                        seen.add(h)
                        hosts.append(h)
            continue

        m = _HOST_RE.match(line)
        if not m:
            continue
        for alias in m.group(1).split():
            if any(ch in alias for ch in "*?!"):
                continue
            if alias not in seen:
                seen.add(alias)
                hosts.append(alias)
    return hosts
