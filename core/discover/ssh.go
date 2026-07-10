package discover

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/isumin/hopmux/core/model"
	"github.com/isumin/hopmux/core/probe"
	"github.com/isumin/hopmux/core/sshconfig"
)

// SSHBackend probes hosts over real SSH, connection-safely.
//
// Safety rules that exist because an earlier naive fan-out looked like an SSH
// scan to a shared gateway and risked fail2ban:
//   - At most ONE concurrent connection per destination host (IP): connections
//     to the same box are serialized via a per-host mutex.
//   - If a host at a given IP fails to connect or auth, that IP is put in a
//     session cooldown and its remaining aliases are marked skipped, not retried.
//   - BatchMode=yes so a password-needing host never hangs; short ConnectTimeout.
//   - One reused ControlMaster connection per endpoint.
type SSHBackend struct {
	Timeout        time.Duration // per-host connect timeout
	entriesByAlias map[string]sshconfig.Entry

	ipMu   sync.Mutex
	ipLock map[string]*sync.Mutex // per-IP serialization
	cold   map[string]string      // IP -> reason it's in cooldown
}

// NewSSH builds an SSH backend, using the ssh config to know each host's IP so
// it can serialize per-IP and share cooldowns across aliases.
func NewSSH(timeout time.Duration, entries []sshconfig.Entry) *SSHBackend {
	byAlias := map[string]sshconfig.Entry{}
	for _, e := range entries {
		byAlias[e.Alias] = e
	}
	return &SSHBackend{
		Timeout:        timeout,
		entriesByAlias: byAlias,
		ipLock:         map[string]*sync.Mutex{},
		cold:           map[string]string{},
	}
}

func (s *SSHBackend) ipOf(alias string) string {
	if e, ok := s.entriesByAlias[alias]; ok && e.HostName != "" {
		return e.HostName
	}
	return alias
}

// endpointOf is IP:port — the real server identity. Cooldown keys on this (not
// bare IP) so a failure on one port never skips a *different* server that merely
// shares the gateway IP.
func (s *SSHBackend) endpointOf(alias string) string {
	if e, ok := s.entriesByAlias[alias]; ok {
		return e.Endpoint()
	}
	return alias
}

func (s *SSHBackend) lockFor(ip string) *sync.Mutex {
	s.ipMu.Lock()
	defer s.ipMu.Unlock()
	if m, ok := s.ipLock[ip]; ok {
		return m
	}
	m := &sync.Mutex{}
	s.ipLock[ip] = m
	return m
}

func (s *SSHBackend) coldReason(ip string) string {
	s.ipMu.Lock()
	defer s.ipMu.Unlock()
	return s.cold[ip]
}

func (s *SSHBackend) markCold(ip, reason string) {
	s.ipMu.Lock()
	defer s.ipMu.Unlock()
	if _, ok := s.cold[ip]; !ok {
		s.cold[ip] = reason
	}
}

func (s *SSHBackend) Discover(hosts []string, onUpdate Update) []model.Host {
	results := make([]model.Host, len(hosts))
	var wg sync.WaitGroup
	// Cap total concurrency too (across different IPs).
	sem := make(chan struct{}, 8)

	for i, name := range hosts {
		wg.Add(1)
		go func(idx int, alias string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			ip := s.ipOf(alias)
			endpoint := s.endpointOf(alias)
			// Skip only if THIS exact endpoint already failed this run (retry
			// guard). Different ports on the same IP are different servers, so we
			// key on endpoint — a failure on one never skips another.
			if reason := s.coldReason(endpoint); reason != "" {
				h := model.Host{Name: alias, Scanned: true, Reachable: false, Err: reason}
				results[idx] = h
				if onUpdate != nil {
					onUpdate(h)
				}
				return
			}
			// Serialize per IP: at most one connection in flight to a given
			// gateway at a time (fail2ban-friendly), even across its ports.
			l := s.lockFor(ip)
			l.Lock()
			defer l.Unlock()

			h := s.probeOne(alias)
			// Cool down only THIS endpoint, and only on real network failure.
			if !h.Reachable && !h.AuthRequired && isConnErr(h.Err) {
				s.markCold(endpoint, h.Err)
			}
			results[idx] = h
			if onUpdate != nil {
				onUpdate(h)
			}
		}(i, name)
	}
	wg.Wait()
	return results
}

func (s *SSHBackend) probeOne(alias string) model.Host {
	h := model.Host{Name: alias, Scanned: true}
	to := int(s.Timeout.Seconds())
	if to <= 0 {
		to = 6
	}
	args := append(muxOpts(),
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout="+strconv.Itoa(to),
		"-o", "StrictHostKeyChecking=accept-new",
		alias, "sh", "-",
	)
	ctx, cancel := context.WithTimeout(context.Background(), s.Timeout+25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", args...)
	// On Windows this ssh probe would flash its own console window on every scan
	// (and scans repeat on a timer). Suppress it; no-op on other platforms.
	hideWindow(cmd)
	cmd.Stdin = strings.NewReader(probe.RemoteSH)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			h.Err = "timeout"
			return h
		}
		if out.Len() == 0 {
			h.Err = cleanErr(errb.String())
			// "Permission denied" = reachable, just needs interactive auth.
			low := strings.ToLower(errb.String())
			if strings.Contains(low, "permission denied") ||
				strings.Contains(low, "authentication failed") {
				h.AuthRequired = true
				h.Err = "needs interactive login (password/key)"
			}
			return h
		}
	}
	parseTSV(out.String(), &h)
	h.Reachable = true
	return h
}

func parseTSV(s string, h *model.Host) {
	for _, ln := range strings.Split(s, "\n") {
		p := strings.Split(ln, "\t")
		if len(p) == 0 {
			continue
		}
		switch p[0] {
		case "T":
			if len(p) == 5 {
				h.Tmux = append(h.Tmux, model.TmuxSession{
					Name: p[1], Windows: p[2], Attached: p[3] == "1", Created: p[4], Host: h.Name})
			}
		case "A":
			if len(p) == 6 {
				mt, _ := strconv.ParseInt(p[4], 10, 64)
				h.Agents = append(h.Agents, model.AgentSession{
					Agent: model.Agent(p[1]), SID: p[2], CWD: p[3], MTime: mt, Title: p[5], Host: h.Name})
			}
		case "G":
			if len(p) == 6 {
				idx, _ := strconv.Atoi(p[1])
				util, _ := strconv.Atoi(p[2])
				mu, _ := strconv.Atoi(p[3])
				mt, _ := strconv.Atoi(p[4])
				h.GPUs = append(h.GPUs, model.GPU{
					Index: idx, Util: util, MemUsed: mu, MemTotal: mt, Name: p[5]})
			}
		case "M":
			if len(p) == 3 {
				switch p[1] {
				case "host":
					h.Hostname = p[2]
				case "now":
					h.Now, _ = strconv.ParseInt(p[2], 10, 64)
				}
			}
		}
	}
}

func cleanErr(stderr string) string {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	msg := ""
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			continue
		}
		low := strings.ToLower(l)
		if strings.Contains(low, "pseudo-terminal") || strings.HasPrefix(low, "warning: permanently added") {
			continue
		}
		msg = l
		break
	}
	low := strings.ToLower(msg)
	if strings.Contains(low, "is not recognized") || strings.Contains(low, "command not found") || msg == "Python" {
		msg = "no shell/python on remote"
	}
	if len(msg) > 110 {
		msg = msg[:110]
	}
	// Some remotes (e.g. Windows) emit non-UTF8 error text → sanitize to avoid
	// mojibake in the UI.
	if !utf8.ValidString(msg) {
		msg = "connection failed"
	}
	if msg == "" {
		msg = "ssh failed"
	}
	return msg
}

// isConnErr reports whether an error means the *connection* (not just this host's
// contents) failed — those are the ones that should cool down the whole IP.
func isConnErr(msg string) bool {
	// Only genuine network-down signals — NOT auth failures (those mean the host
	// is reachable and just needs interactive login).
	low := strings.ToLower(msg)
	for _, s := range []string{"timed out", "timeout",
		"connection refused", "no route", "banner exchange", "connection closed",
		"could not resolve"} {
		if strings.Contains(low, s) {
			return true
		}
	}
	return false
}

// --- ControlMaster options (short socket path; skipped on Windows) ---

var ctrlDirOnce sync.Once
var ctrlDir string

func controlDir() string {
	ctrlDirOnce.Do(func() {
		// The socket path MUST stay short: sun_path caps ~104 chars and macOS's
		// os.TempDir() (/var/folders/.../T/) already eats most of it, blowing the
		// limit once %C (a 40-char hash) is appended. Anchor under /tmp instead.
		suffix := "u"
		if runtime.GOOS != "windows" {
			suffix = strconv.Itoa(os.Getuid())
		}
		base := "/tmp/hopmux-" + suffix
		if runtime.GOOS == "windows" {
			base = filepath.Join(os.TempDir(), "hopmux-"+suffix)
		}
		if err := os.MkdirAll(base, 0o700); err == nil {
			ctrlDir = base
		} else {
			ctrlDir, _ = os.MkdirTemp("", "hm-")
		}
	})
	return ctrlDir
}

func muxOpts() []string {
	if runtime.GOOS == "windows" { // no ControlMaster on Windows OpenSSH
		return nil
	}
	return []string{
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=" + filepath.Join(controlDir(), "%C"),
		"-o", "ControlPersist=120s",
	}
}

// ListDir returns the sub-directory names under dir on the host (for path
// tab-completion when starting a new session). dir may contain a leading ~ and a
// partial trailing segment — the remote shell expands ~ and we list the parent,
// letting the caller filter by the partial. Non-interactive and BatchMode, so it
// returns quickly or fails fast on a host that still needs a password.
func ListDir(alias, dir string) []string {
	if dir == "" {
		dir = "~/"
	}
	// Only complete directories; append '/' so the UI can tell dirs apart and
	// keep completing into them. `2>/dev/null` swallows "no such file" noise.
	remote := "cd " + shellQuote(dir) + " 2>/dev/null && ls -1ap 2>/dev/null | grep '/$'"
	args := append(muxOpts(),
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=6",
		"-o", "StrictHostKeyChecking=accept-new",
		alias, remote,
	)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", args...)
	hideWindow(cmd)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil
	}
	var names []string
	for _, ln := range strings.Split(out.String(), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || ln == "./" || ln == "../" {
			continue
		}
		names = append(names, ln) // keeps the trailing '/'
	}
	return names
}

// shellQuote single-quote-wraps a path for the remote shell, but leaves a
// leading ~ (and ~/) unquoted so the shell still expands it.
func shellQuote(p string) string {
	esc := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	if p == "~" {
		return "~"
	}
	if strings.HasPrefix(p, "~/") {
		return "~/" + esc(p[2:])
	}
	return esc(p)
}

// RunArgs builds the argv for an interactive command on a host: TTY allocated so
// full-screen apps (tmux/claude/codex) work, no BatchMode so auth can prompt.
// This is what the UI hands to the terminal when attaching. It reuses the same
// ControlMaster connection the probe warmed up.
func RunArgs(alias, remoteCmd string) []string {
	args := muxOpts()
	// SetEnv asks sshd to apply a UTF-8 locale (so remote TUIs render CJK, not
	// underscores). GUI-app ssh doesn't forward the local LANG, and boxes often
	// default to POSIX.
	args = append(args, "-t", "-o", "ConnectTimeout=10",
		"-o", "SetEnv=LC_ALL=C.utf8", alias)
	if remoteCmd != "" {
		args = append(args, remoteCmd)
	} else {
		// interactive login shell, but force a UTF-8 locale first
		args = append(args, "export LC_ALL=C.utf8 LANG=C.utf8 2>/dev/null; exec ${SHELL:-sh} -l")
	}
	return args
}
