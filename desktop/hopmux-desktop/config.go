package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/isumin/hopmux/core/sshconfig"
)

func sshConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "config")
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
