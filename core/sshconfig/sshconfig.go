// Package sshconfig parses ~/.ssh/config into a list of connectable host
// aliases, and — crucially for hopmux — groups aliases that resolve to the same
// endpoint so we never fan out multiple connections at one server.
package sshconfig

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Entry is one resolved host alias from the config.
type Entry struct {
	Alias    string
	HostName string // resolved HostName (falls back to Alias)
	Port     string // resolved Port (default "22")
	User     string
}

// Endpoint is the dedup key: two aliases with the same endpoint hit the same
// server, so we probe only one of them.
func (e Entry) Endpoint() string {
	hn := e.HostName
	if hn == "" {
		hn = e.Alias
	}
	port := e.Port
	if port == "" {
		port = "22"
	}
	return hn + ":" + port
}

// Parse reads an ssh config (following one level of Include) and returns entries
// in file order, skipping wildcard patterns (Host *, ?, negations) that can't be
// connected to directly.
func Parse(configPath string) ([]Entry, error) {
	path := expand(configPath)
	entries, err := parseFile(path, 0)
	if err != nil {
		return nil, err
	}
	// de-duplicate by alias, keep first occurrence
	seen := map[string]bool{}
	out := entries[:0]
	for _, e := range entries {
		if seen[e.Alias] {
			continue
		}
		seen[e.Alias] = true
		out = append(out, e)
	}
	return out, nil
}

func parseFile(path string, depth int) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var entries []Entry
	// current block: aliases share the settings until the next Host line
	var curAliases []string
	cur := map[string]string{} // key -> value for the current Host block

	flush := func() {
		if len(curAliases) == 0 {
			return
		}
		for _, a := range curAliases {
			if isPattern(a) {
				continue
			}
			entries = append(entries, Entry{
				Alias:    a,
				HostName: cur["hostname"],
				Port:     cur["port"],
				User:     cur["user"],
			})
		}
		curAliases = nil
		cur = map[string]string{}
	}

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val := splitKV(line)
		switch strings.ToLower(key) {
		case "host":
			flush()
			curAliases = strings.Fields(val)
		case "hostname", "port", "user":
			cur[strings.ToLower(key)] = strings.TrimSpace(val)
		case "include":
			// includes apply globally; resolve them in place
			if depth < 3 {
				flush()
				for _, inc := range resolveIncludes(val, filepath.Dir(path)) {
					sub, _ := parseFile(inc, depth+1)
					entries = append(entries, sub...)
				}
			}
		}
	}
	flush()
	return entries, sc.Err()
}

func resolveIncludes(val, baseDir string) []string {
	var out []string
	for _, tok := range strings.Fields(val) {
		tok = expand(tok)
		if !filepath.IsAbs(tok) {
			tok = filepath.Join(baseDir, tok)
		}
		matches, _ := filepath.Glob(tok)
		sort.Strings(matches)
		out = append(out, matches...)
	}
	return out
}

func splitKV(line string) (string, string) {
	// ssh config allows "Key Value" and "Key=Value"
	if i := strings.IndexAny(line, " \t="); i >= 0 {
		key := line[:i]
		val := strings.TrimLeft(line[i:], " \t=")
		return key, val
	}
	return line, ""
}

func isPattern(alias string) bool {
	return strings.ContainsAny(alias, "*?!")
}

func expand(p string) string {
	if p == "~" {
		if h, err := os.UserHomeDir(); err == nil {
			return h
		}
	}
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[2:])
		}
	}
	return p
}

// GroupByEndpoint returns, in original order, the "representative" entries — one
// per unique endpoint — plus a map from each representative alias to all aliases
// that share its endpoint. hopmux probes only the representatives.
func GroupByEndpoint(entries []Entry) (reps []Entry, members map[string][]string) {
	members = map[string][]string{}
	seenEndpoint := map[string]string{} // endpoint -> representative alias
	for _, e := range entries {
		ep := e.Endpoint()
		if rep, ok := seenEndpoint[ep]; ok {
			members[rep] = append(members[rep], e.Alias)
			continue
		}
		seenEndpoint[ep] = e.Alias
		members[e.Alias] = []string{e.Alias}
		reps = append(reps, e)
	}
	return reps, members
}
