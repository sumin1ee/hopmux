#!/usr/bin/env python3
"""Generate demo screenshots (SVG) for the README using Textual's snapshot.

Runs the app headless against synthetic data so the demo is stable and shows off
both themes and both views. SVGs are written to docs/images/.

    python3 scripts/make_demo.py

Convert to PNG afterwards with any SVG rasteriser, e.g.:
    qlmanage -t -s 1600 -o docs/images docs/images/*.svg   # macOS
    rsvg-convert -o out.png in.svg                          # linux
"""
import asyncio
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
sys.path.insert(0, ROOT)

from hopmux.app import HopmuxApp                       # noqa: E402
from hopmux.models import Host, AgentSession, TmuxSession  # noqa: E402
from hopmux import discovery                            # noqa: E402
from textual.widgets import OptionList                  # noqa: E402

OUT = os.path.join(ROOT, "docs", "images")
NOW = 1783520000  # fixed clock so relative times are stable


def sample_hosts(on_done=None):
    h1 = Host("DCTN-0702103117", reachable=True, hostname="dctn", now=NOW)
    h1.tmux = [TmuxSession("download", "1", True, "0", host=h1.name),
               TmuxSession("rogs_overlap_v2i", "1", False, "0", host=h1.name),
               TmuxSession("shchoi", "2", True, "0", host=h1.name)]
    h1.agents = [
        AgentSession("claude", "a", "/home/x/camosplat_refinement", NOW - 30,
                     "codebook 이 뭐하는건지 직관적으로 설명해줘", h1.name),
        AgentSession("codex", "b", "/home/x/PseudoMapTrainer", NOW - 300,
                     "act as final ULW gate reviewer", h1.name),
        AgentSession("claude", "c", "/home/x/satmaptracker", NOW - 1080,
                     "bevfeature distillation 대화 내용 가져와라", h1.name),
        AgentSession("claude", "d", "/home/x/sung", NOW - 7200,
                     "nvidia 데이터셋 다운받고 있음. hd map gt가 없는 상태", h1.name),
    ]
    h2 = Host("Hinton", reachable=True, hostname="hinton", now=NOW)
    h2.agents = [AgentSession("claude", "e", "/home/sumin/projects/XGS", NOW - 3600,
                              "XGS eval 돌린 결과 정리", h2.name)]
    h3 = Host("Lecun", reachable=True, hostname="lecun", now=NOW)
    h3.tmux = [TmuxSession("train", "1", True, "0", host=h3.name)]
    h3.agents = [AgentSession("codex", "f", "/data/diffusion", NOW - 90000,
                              "diffusion sampler 속도 개선", h3.name)]
    h4 = Host("Poseidon", reachable=False, error="Permission denied (publickey)")
    h5 = Host("server22", reachable=False, error="Connection timed out")
    hosts = [h1, h2, h3, h4, h5]
    if on_done:
        for h in hosts:
            on_done(h)
    return hosts


async def shoot(name, theme, focus_host=None):
    def fake(names, timeout, local_name=None, workers=12, on_done=None):
        return sample_hosts(on_done)
    discovery.discover = fake
    app = HopmuxApp(["DCTN-0702103117", "Hinton", "Lecun", "Poseidon", "server22"])
    async with app.run_test(size=(118, 34)) as pilot:
        await pilot.pause(); await asyncio.sleep(0.4); await pilot.pause()
        app.theme = theme
        if focus_host is not None:
            app.set_focus(app.query_one("#server-list", OptionList))
            for _ in range(focus_host):
                await pilot.press("down")
        await pilot.pause(); await asyncio.sleep(0.2); await pilot.pause()
        svg = app.export_screenshot(title="hopmux")
        path = os.path.join(OUT, name)
        with open(path, "w") as fh:
            fh.write(svg)
        print("wrote", path)


async def main():
    os.makedirs(OUT, exist_ok=True)
    await shoot("demo-dark.svg", "hopmux-dark", focus_host=1)     # DCTN, dark
    await shoot("demo-light.svg", "hopmux-light", focus_host=1)   # DCTN, light
    await shoot("demo-recent.svg", "hopmux-dark", focus_host=0)   # recent view


if __name__ == "__main__":
    asyncio.run(main())
