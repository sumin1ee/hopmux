// Package probe holds the remote inventory script piped to `sh -` over ssh.
// It locates a python interpreter on the remote and runs an embedded payload
// that lists tmux sessions plus Claude Code and Codex CLI sessions as TSV.
//
// TSV line formats (tab-separated):
//
//	T <name> <windows> <attached 0/1> <created_epoch>       a tmux session
//	A <agent> <sid> <cwd> <mtime_epoch> <title>             claude/codex session
//	M <key> <value>                                         meta / errors
package probe

// RemoteSH is fed to `sh -`. It finds python3/python and runs PyPayload; if no
// python exists it still emits tmux sessions via a shell fallback.
const RemoteSH = `
PY=""
for c in python3 python; do
  if command -v "$c" >/dev/null 2>&1; then PY="$c"; break; fi
done
if [ -n "$PY" ]; then
  "$PY" - <<'HOPMUX_PY'
` + pyPayload + `
HOPMUX_PY
else
  printf 'M\tpython\tabsent\n'
  if command -v tmux >/dev/null 2>&1; then
    tmux list-sessions -F 'T	#{session_name}	#{session_windows}	#{session_attached}	#{session_created}' 2>/dev/null
  fi
fi
`

const pyPayload = `
import os, sys, json, glob, subprocess, time
def out(*f):
    s = ["" if x is None else str(x).replace("\t"," ").replace("\n"," ").replace("\r"," ") for x in f]
    sys.stdout.write("\t".join(s) + "\n")
try:
    fmt = "#{session_name}\t#{session_windows}\t#{session_attached}\t#{session_created}"
    p = subprocess.run(["tmux","list-sessions","-F",fmt], stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    if p.returncode == 0:
        for ln in p.stdout.decode("utf-8","replace").splitlines():
            a = ln.split("\t")
            if len(a) == 4:
                out("T", a[0], a[1], "1" if a[2] not in ("0","") else "0", a[3])
    else:
        out("M","tmux","none")
except FileNotFoundError:
    out("M","tmux","absent")
except Exception as e:
    out("M","tmux_err",repr(e))
try:
    q = "--query-gpu=index,utilization.gpu,memory.used,memory.total,name"
    g = subprocess.run(["nvidia-smi", q, "--format=csv,noheader,nounits"],
                       stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    if g.returncode == 0:
        for ln in g.stdout.decode("utf-8","replace").splitlines():
            a = [x.strip() for x in ln.split(",")]
            if len(a) >= 5:
                out("G", a[0], a[1], a[2], a[3], ",".join(a[4:]))
except FileNotFoundError:
    pass
except Exception:
    pass
home = os.path.expanduser("~")
def first_text(content):
    if isinstance(content, str): return content
    if isinstance(content, list):
        for c in content:
            if isinstance(c, dict) and c.get("type") in ("text","input_text","output_text"):
                return c.get("text","")
    return ""
def is_noise(t):
    return (t.startswith("<ide_") or t.startswith("<command-") or t.startswith("<local-command")
            or t.startswith("<task-") or t.startswith("Caveat:") or t.startswith("<system-reminder")
            or t.startswith("<environment_context") or t.startswith("<user_instructions"))
def scan_claude():
    files = glob.glob(os.path.join(home,".claude","projects","*","*.jsonl"))
    try: files.sort(key=lambda f: os.path.getmtime(f), reverse=True)
    except Exception: pass
    n = 0
    for f in files[:80]:
        try: mt = int(os.path.getmtime(f))
        except OSError: continue
        sid = os.path.splitext(os.path.basename(f))[0]
        cwd, title = "", ""
        try:
            with open(f,"r",encoding="utf-8",errors="replace") as fh:
                for ln in fh:
                    if cwd and title: break
                    ln = ln.strip()
                    if not ln or ln[0] != "{": continue
                    try: o = json.loads(ln)
                    except Exception: continue
                    if not cwd and isinstance(o.get("cwd"), str): cwd = o["cwd"]
                    if not title and o.get("type") == "user":
                        t = first_text((o.get("message") or {}).get("content")).strip()
                        if t and not is_noise(t): title = t[:100]
        except Exception: pass
        if not cwd and not title: continue
        out("A","claude",sid,cwd,mt,title); n += 1
    return n
def scan_codex():
    files = glob.glob(os.path.join(home,".codex","sessions","*","*","*","rollout-*.jsonl"))
    files += glob.glob(os.path.join(home,".codex","sessions","rollout-*.jsonl"))
    try: files.sort(key=lambda f: os.path.getmtime(f), reverse=True)
    except Exception: pass
    n = 0
    for f in files[:80]:
        try: mt = int(os.path.getmtime(f))
        except OSError: continue
        sid, cwd, title = "", "", ""
        try:
            with open(f,"r",encoding="utf-8",errors="replace") as fh:
                for ln in fh:
                    if sid and cwd and title: break
                    ln = ln.strip()
                    if not ln or ln[0] != "{": continue
                    try: o = json.loads(ln)
                    except Exception: continue
                    if o.get("type") == "session_meta":
                        pl = o.get("payload") or {}
                        sid = pl.get("id") or pl.get("session_id") or ""
                        if isinstance(pl.get("cwd"), str): cwd = pl["cwd"]
                    if not title:
                        pl = o.get("payload") or o
                        role = pl.get("role") or o.get("role")
                        if role == "user":
                            t = first_text(pl.get("content")).strip()
                            if t and not is_noise(t): title = t[:100]
        except Exception: pass
        if not sid:
            base = os.path.basename(f)
            sid = base.replace("rollout-","").replace(".jsonl","")[-36:]
        out("A","codex",sid,cwd,mt,title); n += 1
    return n
nc = scan_claude()
nx = scan_codex()
try: out("M","host",os.uname().nodename)
except Exception: out("M","host","?")
out("M","claude",nc); out("M","codex",nx); out("M","now",int(time.time()))
`
