// Package discover turns a list of ssh hosts into populated model.Host values.
//
// A Backend produces host inventories; the UI does not care whether they came
// from real SSH or a mock. Results stream in via the Update callback so the UI
// can render servers as each one finishes.
package discover

import (
	"sort"
	"time"

	"github.com/isumin/hopmux/core/model"
)

// Update is called (possibly from another goroutine) as each host completes.
type Update func(model.Host)

// Backend discovers sessions on a set of hosts.
type Backend interface {
	// Discover probes every host, calling onUpdate for each as it finishes, and
	// returns the full slice in the original order when done.
	Discover(hosts []string, onUpdate Update) []model.Host
}

// AllRecentAgents flattens every host's agent sessions, newest first — the view
// shown when no server is selected.
func AllRecentAgents(hosts []model.Host, limit int) []model.AgentSession {
	var flat []model.AgentSession
	for _, h := range hosts {
		flat = append(flat, h.Agents...)
	}
	sort.SliceStable(flat, func(i, j int) bool { return flat[i].MTime > flat[j].MTime })
	if limit > 0 && len(flat) > limit {
		flat = flat[:limit]
	}
	return flat
}

// NowEpoch is the current unix time (seconds).
func NowEpoch() int64 { return time.Now().Unix() }
