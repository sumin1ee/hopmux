# Third-party licenses

hopmux itself is MIT-licensed (see [LICENSE](LICENSE)). It bundles and depends on
the following third-party software, each under its own license.

## Bundled in the application

### D2Coding (font)
- Bundled: `desktop/hopmux-desktop/frontend/src/assets/fonts/D2Coding.woff2`
  (converted to WOFF2 from the original TTF; no glyphs were modified).
- Copyright © 2010–2018 NAVER Corporation, with Reserved Font Name **D2Coding**.
- License: **SIL Open Font License, Version 1.1** — full text in
  [`.../fonts/D2Coding-LICENSE.txt`](desktop/hopmux-desktop/frontend/src/assets/fonts/D2Coding-LICENSE.txt).
- Source: https://github.com/naver/d2codingfont

## Key dependencies

| Project | License | Use |
|---|---|---|
| [Wails v2](https://github.com/wailsapp/wails) | MIT | desktop app framework |
| [xterm.js](https://github.com/xtermjs/xterm.js) (`@xterm/*`) | MIT | terminal emulator in the desktop app |
| [Bubble Tea](https://github.com/charmbracelet/bubbletea) | MIT | terminal UI (TUI) |
| [Lip Gloss](https://github.com/charmbracelet/lipgloss) | MIT | TUI styling |
| [creack/pty](https://github.com/creack/pty) | MIT | PTY handling |

Go module and npm dependency licenses are recorded in `go.sum` /
`package-lock.json` and each dependency's own repository.
