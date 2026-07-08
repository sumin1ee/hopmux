r"""The remote inventory probe.

`REMOTE_SH` is piped to `sh -` on each host over SSH. It locates a Python
interpreter and runs `PY_PAYLOAD`, which lists tmux sessions and scans for
Claude Code and Codex CLI sessions, emitting compact TSV on stdout.

Design notes:
- No install on the remote. Everything travels over the pipe.
- If the remote has no python at all, we still emit tmux sessions via a shell
  fallback, so at least live terminals show up.
- Every field is tab-sanitized so the TSV stays parseable.

TSV line formats (tab-separated):
    T <name> <windows> <attached 0/1> <created_epoch>         a tmux session
    A <agent> <sid> <cwd> <mtime_epoch> <title>               claude/codex session
    M <key> <value>                                           meta / errors
"""

PY_PAYLOAD = r'''
import os, sys, json, glob, subprocess, time

def out(*f):
    s = ["" if x is None else str(x).replace("\t", " ").replace("\n", " ").replace("\r", " ") for x in f]
    sys.stdout.write("\t".join(s) + "\n")

# ---------- tmux ----------
try:
    fmt = "#{session_name}\t#{session_windows}\t#{session_attached}\t#{session_created}"
    p = subprocess.run(["tmux", "list-sessions", "-F", fmt],
                       stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    if p.returncode == 0:
        for ln in p.stdout.decode("utf-8", "replace").splitlines():
            a = ln.split("\t")
            if len(a) == 4:
                out("T", a[0], a[1], "1" if a[2] not in ("0", "") else "0", a[3])
    else:
        out("M", "tmux", "none")
except FileNotFoundError:
    out("M", "tmux", "absent")
except Exception as e:
    out("M", "tmux_err", repr(e))

home = os.path.expanduser("~")

def first_text(content):
    # Claude uses {"type":"text"}; Codex uses {"type":"input_text"/"output_text"}.
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        for c in content:
            if isinstance(c, dict) and c.get("type") in ("text", "input_text", "output_text"):
                return c.get("text", "")
    return ""

def is_noise(t):
    return (t.startswith("<ide_") or t.startswith("<command-")
            or t.startswith("<local-command") or t.startswith("<task-")
            or t.startswith("Caveat:") or t.startswith("<system-reminder")
            or t.startswith("<environment_context") or t.startswith("<user_instructions"))

# ---------- Claude Code: ~/.claude/projects/<slug>/<uuid>.jsonl ----------
def scan_claude():
    files = glob.glob(os.path.join(home, ".claude", "projects", "*", "*.jsonl"))
    try:
        files.sort(key=lambda f: os.path.getmtime(f), reverse=True)
    except Exception:
        pass
    n = 0
    for f in files[:80]:
        try:
            mt = int(os.path.getmtime(f))
        except OSError:
            continue
        sid = os.path.splitext(os.path.basename(f))[0]
        cwd, title = "", ""
        try:
            with open(f, "r", encoding="utf-8", errors="replace") as fh:
                for ln in fh:
                    if cwd and title:
                        break
                    ln = ln.strip()
                    if not ln or ln[0] != "{":
                        continue
                    try:
                        o = json.loads(ln)
                    except Exception:
                        continue
                    if not cwd and isinstance(o.get("cwd"), str):
                        cwd = o["cwd"]
                    if not title and o.get("type") == "user":
                        t = first_text((o.get("message") or {}).get("content")).strip()
                        if t and not is_noise(t):
                            title = t[:100]
        except Exception:
            pass
        if not cwd and not title:
            continue
        out("A", "claude", sid, cwd, mt, title)
        n += 1
    return n

# ---------- Codex CLI: ~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl ----------
def scan_codex():
    files = glob.glob(os.path.join(home, ".codex", "sessions", "*", "*", "*", "rollout-*.jsonl"))
    files += glob.glob(os.path.join(home, ".codex", "sessions", "rollout-*.jsonl"))
    try:
        files.sort(key=lambda f: os.path.getmtime(f), reverse=True)
    except Exception:
        pass
    n = 0
    for f in files[:80]:
        try:
            mt = int(os.path.getmtime(f))
        except OSError:
            continue
        sid, cwd, title = "", "", ""
        try:
            with open(f, "r", encoding="utf-8", errors="replace") as fh:
                for ln in fh:
                    if sid and cwd and title:
                        break
                    ln = ln.strip()
                    if not ln or ln[0] != "{":
                        continue
                    try:
                        o = json.loads(ln)
                    except Exception:
                        continue
                    if o.get("type") == "session_meta":
                        pl = o.get("payload") or {}
                        sid = pl.get("id") or pl.get("session_id") or ""
                        if isinstance(pl.get("cwd"), str):
                            cwd = pl["cwd"]
                    # user turns in codex rollouts show up as response_item/message
                    if not title:
                        pl = o.get("payload") or o
                        role = pl.get("role") or o.get("role")
                        if role == "user":
                            t = first_text(pl.get("content")).strip()
                            if t and not is_noise(t):
                                title = t[:100]
        except Exception:
            pass
        if not sid:
            # fall back to the uuid embedded in the filename
            base = os.path.basename(f)
            sid = base.replace("rollout-", "").replace(".jsonl", "")[-36:]
        out("A", "codex", sid, cwd, mt, title)
        n += 1
    return n

nc = scan_claude()
nx = scan_codex()
try:
    out("M", "host", os.uname().nodename)
except Exception:
    out("M", "host", "?")
out("M", "claude", nc)
out("M", "codex", nx)
out("M", "now", int(time.time()))
'''

# Piped to `sh -`. Find a python, run the payload; else emit tmux via shell.
REMOTE_SH = r'''
PY=""
for c in python3 python; do
  if command -v "$c" >/dev/null 2>&1; then PY="$c"; break; fi
done
if [ -n "$PY" ]; then
  "$PY" - <<'HOPMUX_PY'
''' + PY_PAYLOAD + r'''
HOPMUX_PY
else
  printf 'M\tpython\tabsent\n'
  if command -v tmux >/dev/null 2>&1; then
    tmux list-sessions -F 'T	#{session_name}	#{session_windows}	#{session_attached}	#{session_created}' 2>/dev/null
  fi
fi
'''
