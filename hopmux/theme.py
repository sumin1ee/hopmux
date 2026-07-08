"""Color themes for hopmux.

Two goals from the brief: (1) simple + modern, (2) it's a *terminal* app, so it
must not be monochrome — Claude Code shows colored output and we echo that.

We register a matched dark and light theme. Accent roles are consistent across
both so the UI reads the same in either mode; only the surface/foreground flips.

Agent colors:
    claude  -> warm coral/orange  (Claude's own accent family)
    codex   -> cyan/teal
    tmux    -> green (live terminals)
    unreachable -> dim red
"""
from __future__ import annotations

from textual.theme import Theme

# Anthropic-ish coral for Claude, cyan for Codex, green for live tmux.
CLAUDE = "#d97757"
CODEX = "#2bb6c4"
TMUX = "#3fb950"
DANGER = "#f85149"

hopmux_dark = Theme(
    name="hopmux-dark",
    primary=CLAUDE,
    secondary=CODEX,
    accent=CLAUDE,
    success=TMUX,
    warning="#d29922",
    error=DANGER,
    foreground="#e6e6e6",
    background="#0d1017",
    surface="#12161f",
    panel="#171c26",
    dark=True,
    variables={
        "block-cursor-foreground": "#0d1017",
        "block-cursor-background": CLAUDE,
        "border": "#2a313c",
        "scrollbar": "#2a313c",
        "footer-key-foreground": CODEX,
        "footer-description-foreground": "#9aa4b2",
    },
)

hopmux_light = Theme(
    name="hopmux-light",
    primary="#c25b3a",
    secondary="#0e8fa0",
    accent="#c25b3a",
    success="#1a7f37",
    warning="#9a6700",
    error="#cf222e",
    foreground="#1c2128",
    background="#fbfbfa",
    surface="#f3f3f1",
    panel="#ecebe8",
    dark=False,
    variables={
        "block-cursor-foreground": "#fbfbfa",
        "block-cursor-background": "#c25b3a",
        "border": "#d7d5d0",
        "scrollbar": "#cfcdc8",
        "footer-key-foreground": "#0e8fa0",
        "footer-description-foreground": "#57606a",
    },
)


def agent_color(agent: str) -> str:
    return {"claude": CLAUDE, "codex": CODEX}.get(agent, TMUX)
