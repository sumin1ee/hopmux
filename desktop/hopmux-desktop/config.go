package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/isumin/hopmux/core/sshconfig"
)

func sshConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "config")
}

func sshDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh")
}

// LocalPublicKey returns the user's SSH public key text, creating an ed25519 key
// pair first if none exists. This is the key the app installs on a host so
// future connections need no password. Returns "" only if key generation fails.
func (a *App) LocalPublicKey() string {
	dir := sshDir()
	// Prefer ed25519, fall back to rsa — whatever already exists.
	for _, name := range []string{"id_ed25519.pub", "id_rsa.pub"} {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			if s := strings.TrimSpace(string(b)); s != "" {
				return s
			}
		}
	}
	// None yet: generate an ed25519 key with no passphrase.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	key := filepath.Join(dir, "id_ed25519")
	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-f", key)
	hideWindowCmd(cmd)
	if err := cmd.Run(); err != nil {
		return ""
	}
	b, err := os.ReadFile(key + ".pub")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// EnsureIdentityFile makes sure the given host's block in ~/.ssh/config points
// at the local private key, so once the public key is installed on the host the
// app connects with it automatically. No-op if an IdentityFile is already set.
// Returns "" on success (or already-present), else an error message.
func (a *App) EnsureIdentityFile(alias string) string {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return "host is required"
	}
	// Pick the private key that matches the public key we hand out.
	idFile := "~/.ssh/id_ed25519"
	if _, err := os.Stat(filepath.Join(sshDir(), "id_ed25519")); err != nil {
		if _, err2 := os.Stat(filepath.Join(sshDir(), "id_rsa")); err2 == nil {
			idFile = "~/.ssh/id_rsa"
		}
	}
	path := sshConfigPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		return err.Error()
	}
	lines := strings.Split(string(raw), "\n")
	out := make([]string, 0, len(lines)+1)
	inBlock := false
	hasIdentity := false
	injected := false
	flush := func() {
		// Called when leaving the target block: add IdentityFile if it lacked one.
		if inBlock && !hasIdentity && !injected {
			out = append(out, "    IdentityFile "+idFile)
			injected = true
		}
	}
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		fields := strings.Fields(trimmed)
		if len(fields) >= 2 && strings.EqualFold(fields[0], "Host") {
			// Leaving any previous block; entering a new one.
			flush()
			inBlock = false
			hasIdentity = false
			for _, name := range fields[1:] {
				if name == alias {
					inBlock = true
					break
				}
			}
		} else if inBlock && len(fields) >= 1 && strings.EqualFold(fields[0], "IdentityFile") {
			hasIdentity = true
		}
		out = append(out, ln)
	}
	flush() // in case the target block is the last one in the file
	if hasIdentity && !injected {
		return "" // already had an IdentityFile — nothing to do
	}
	if !injected {
		return "host " + alias + " not found in ssh config"
	}
	if prev := raw; len(prev) > 0 {
		_ = os.WriteFile(path+".hopmux.bak", prev, 0o600)
	}
	if err := os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o600); err != nil {
		return err.Error()
	}
	a.reload()
	return ""
}

// ReadSSHConfig returns the raw ~/.ssh/config text (for the editor in Settings).
func (a *App) ReadSSHConfig() string {
	b, err := os.ReadFile(sshConfigPath())
	if err != nil {
		return ""
	}
	return string(b)
}

// WriteSSHConfig overwrites ~/.ssh/config, backs up the previous version, then
// reloads the host list and rescans. Returns "" on success or an error message.
func (a *App) WriteSSHConfig(text string) string {
	path := sshConfigPath()
	if prev, err := os.ReadFile(path); err == nil {
		_ = os.WriteFile(path+".hopmux.bak", prev, 0o600)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err.Error()
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return err.Error()
	}
	a.reload()
	return ""
}

// AddServer appends a Host block to ~/.ssh/config and reloads.
func (a *App) AddServer(alias, hostname, port, user string) string {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return "alias (Host) is required"
	}
	for _, e := range a.entries {
		if e.Alias == alias {
			return "a host named " + alias + " already exists"
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\nHost %s\n", alias)
	if h := strings.TrimSpace(hostname); h != "" {
		fmt.Fprintf(&b, "    HostName %s\n", h)
	}
	if p := strings.TrimSpace(port); p != "" {
		fmt.Fprintf(&b, "    Port %s\n", p)
	}
	if u := strings.TrimSpace(user); u != "" {
		fmt.Fprintf(&b, "    User %s\n", u)
	}
	path := sshConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err.Error()
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err.Error()
	}
	if _, err := f.WriteString(b.String()); err != nil {
		f.Close()
		return err.Error()
	}
	f.Close()
	a.reload()
	return ""
}

// SetupMCP registers THIS executable as an MCP server for Claude Code in user
// scope (works in every project), so the local Claude Code becomes the "main
// agent" that can see and drive every server hopmux knows. One click, no
// terminal needed. Returns "" on success or a human-readable error.
func (a *App) SetupMCP() string {
	exe, err := os.Executable()
	if err != nil {
		return "cannot locate the hopmux executable: " + err.Error()
	}
	// Preferred path: the claude CLI owns its config format. Remove-then-add is
	// idempotent and heals a stale path from a previous install location.
	if cli, err := lookupClaude(); err == nil {
		_ = runQuiet(cli, "mcp", "remove", "-s", "user", "hopmux")
		if _, err := runQuietOut(cli, "mcp", "add", "-s", "user", "hopmux", "--", exe, "mcp"); err == nil {
			return ""
		}
		// CLI failed — fall through to editing the config directly.
	}
	// Fallback: write ~/.claude.json ourselves (user-scope mcpServers).
	home, _ := os.UserHomeDir()
	cfgPath := filepath.Join(home, ".claude.json")
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		return "Claude Code doesn't seem to be installed (no `claude` CLI on PATH and no ~/.claude.json). " +
			"Install it from https://claude.com/claude-code and click this again."
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return "~/.claude.json exists but isn't valid JSON: " + err.Error()
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers["hopmux"] = map[string]any{
		"type": "stdio", "command": exe, "args": []string{"mcp"}, "env": map[string]any{},
	}
	cfg["mcpServers"] = servers
	_ = os.WriteFile(cfgPath+".hopmux.bak", raw, 0o600)
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err.Error()
	}
	if err := os.WriteFile(cfgPath, b, 0o600); err != nil {
		return err.Error()
	}
	return ""
}

// lookupClaude finds the claude CLI: PATH first, then the usual install spots
// (native installer, npm global). A GUI app's PATH often misses shell-profile
// additions, so PATH alone isn't enough.
func lookupClaude() (string, error) {
	if p, err := exec.LookPath("claude"); err == nil {
		return p, nil
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".local", "bin", "claude.exe"),
		filepath.Join(home, ".local", "bin", "claude"),
	}
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		candidates = append(candidates, filepath.Join(appdata, "npm", "claude.cmd"))
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}

// runQuiet runs a CLI without a console window, ignoring output.
func runQuiet(cli string, args ...string) error {
	cmd := claudeCmd(cli, args...)
	hideWindowCmd(cmd)
	return cmd.Run()
}

func runQuietOut(cli string, args ...string) (string, error) {
	cmd := claudeCmd(cli, args...)
	hideWindowCmd(cmd)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// claudeCmd builds the exec.Cmd, routing .cmd/.bat shims (npm installs) through
// cmd.exe — CreateProcess cannot start those directly.
func claudeCmd(cli string, args ...string) *exec.Cmd {
	low := strings.ToLower(cli)
	if strings.HasSuffix(low, ".cmd") || strings.HasSuffix(low, ".bat") {
		return exec.Command("cmd", append([]string{"/C", cli}, args...)...)
	}
	return exec.Command(cli, args...)
}

// reload re-parses the ssh config, refreshes the host list, and rescans.
func (a *App) reload() {
	a.entries, _ = sshconfig.Parse("~/.ssh/config")
	a.hostList = a.hostList[:0]
	for _, e := range a.entries {
		a.hostList = append(a.hostList, e.Alias)
	}
	runtime.EventsEmit(a.ctx, "hosts:reloaded", a.hostList)
	go a.Scan()
}

// ---- settings (persisted to ~/.config/hopmux/settings.json) ----

type Settings struct {
	Theme          string `json:"theme"`          // "dark" | "light"
	AutoRefreshSec int    `json:"autoRefreshSec"` // 0 = off
	ScanTimeoutSec int    `json:"scanTimeoutSec"`
	FontSize       int    `json:"fontSize"`
}

func settingsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "hopmux", "settings.json")
}

func (a *App) GetSettings() Settings {
	s := Settings{Theme: "dark", AutoRefreshSec: 20, ScanTimeoutSec: 6, FontSize: 13}
	if b, err := os.ReadFile(settingsPath()); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func (a *App) SaveSettings(s Settings) string {
	if err := os.MkdirAll(filepath.Dir(settingsPath()), 0o700); err != nil {
		return err.Error()
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	if err := os.WriteFile(settingsPath(), b, 0o600); err != nil {
		return err.Error()
	}
	return ""
}
