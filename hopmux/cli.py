"""hopmux command-line entry point."""
from __future__ import annotations

import argparse
import sys

from . import config, discovery


def _print_list(hosts, now):
    for h in hosts:
        if not h.reachable:
            print("  \033[90m✗ %-18s (%s)\033[0m" % (h.name, h.error or "unreachable"))
            continue
        nc = sum(1 for a in h.agents if a.agent == "claude")
        nx = sum(1 for a in h.agents if a.agent == "codex")
        print("\033[1m● %-18s\033[0m tmux:%d claude:%d codex:%d"
              % (h.name, len(h.tmux), nc, nx))
        for t in h.tmux:
            fl = "\033[32mattached\033[0m" if t.attached else "detached"
            print("    \033[32m▣ tmux\033[0m  %-22s %sw %s" % (t.name, t.windows, fl))
        for a in h.recent_agents():
            col = "\033[38;5;209m" if a.agent == "claude" else "\033[36m"
            print("    %s◉ %-6s\033[0m %-20s %s" % (col, a.agent, a.project, a.title[:46]))


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(
        prog="hopmux",
        description="Hop across your SSH servers and resume any Claude Code / "
                    "Codex session — a cmux-style TUI over tmux.")
    ap.add_argument("--list", action="store_true",
                    help="print the inventory as text and exit (no TUI)")
    ap.add_argument("--only", default="",
                    help="comma-separated subset of hosts to include")
    ap.add_argument("--timeout", type=int, default=6,
                    help="per-host SSH connect timeout in seconds (default 6)")
    ap.add_argument("--local", action="store_true",
                    help="also include this machine as a host named 'local'")
    ap.add_argument("--config", default="~/.ssh/config",
                    help="path to ssh config (default ~/.ssh/config)")
    ap.add_argument("--version", action="store_true", help="print version and exit")
    args = ap.parse_args(argv)

    if args.version:
        print(_version())
        return 0

    hosts = config.parse_hosts(args.config)
    local_name = None
    if args.local:
        local_name = "local"
        hosts = ["local"] + hosts
    if args.only:
        want = {x.strip() for x in args.only.split(",") if x.strip()}
        hosts = [h for h in hosts if h in want]
    if not hosts:
        sys.stderr.write("hopmux: no hosts found in %s\n" % args.config)
        return 1

    if args.list or not sys.stdout.isatty():
        sys.stderr.write("hopmux: probing %d host(s)…\n" % len(hosts))
        result = discovery.discover(hosts, args.timeout, local_name)
        _print_list(result, discovery.now_epoch())
        return 0

    from .app import HopmuxApp
    HopmuxApp(hosts, timeout=args.timeout, local_name=local_name).run()
    return 0


def _version() -> str:
    return "hopmux 0.1.0"


if __name__ == "__main__":
    raise SystemExit(main())
