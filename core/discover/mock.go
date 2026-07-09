package discover

import (
	"math/rand"
	"sync"
	"time"

	"github.com/isumin/hopmux/core/model"
)

// MockBackend serves synthetic hosts with simulated per-host latency, so the TUI
// and its streaming I/O can be exercised with no servers at all (`hopmux --demo`).
type MockBackend struct {
	// Fast skips the artificial latency (useful in automated tests).
	Fast bool
	seed int64
}

// NewMock returns a mock backend. seed varies the simulated latencies.
func NewMock(seed int64) *MockBackend { return &MockBackend{seed: seed} }

func (m *MockBackend) Discover(hosts []string, onUpdate Update) []model.Host {
	sample := sampleHosts()
	byName := map[string]model.Host{}
	for _, h := range sample {
		byName[h.Name] = h
	}

	rng := rand.New(rand.NewSource(m.seed + 1))
	results := make([]model.Host, len(hosts))
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i, name := range hosts {
		h, ok := byName[name]
		if !ok {
			// unknown host in config → simulate an unreachable one
			h = model.Host{Name: name, Scanned: true, Reachable: false, Err: "no route to host (mock)"}
		}
		delay := time.Duration(0)
		if !m.Fast {
			// stagger completions between ~150ms and ~1.6s to feel like a real scan
			delay = time.Duration(150+rng.Intn(1500)) * time.Millisecond
		}
		wg.Add(1)
		go func(idx int, host model.Host, d time.Duration) {
			defer wg.Done()
			if d > 0 {
				time.Sleep(d)
			}
			mu.Lock()
			results[idx] = host
			mu.Unlock()
			if onUpdate != nil {
				onUpdate(host)
			}
		}(i, h, delay)
	}
	wg.Wait()
	return results
}

// SampleHosts exposes the demo fixture for screenshot/preview rendering.
func SampleHosts() []model.Host { return sampleHosts() }

// sampleHosts builds a believable fixture of generic servers for demo/screenshots.
func sampleHosts() []model.Host {
	now := NowEpoch()
	reach := func(name, hn string) model.Host {
		return model.Host{Name: name, Reachable: true, Scanned: true, Hostname: hn, Now: now}
	}
	ml := reach("ml-train-01", "us-west-2")
	ml.Tmux = []model.TmuxSession{
		{Name: "train", Windows: "1", Attached: true, Host: ml.Name},
		{Name: "eval", Windows: "1", Attached: false, Host: ml.Name},
		{Name: "tensorboard", Windows: "2", Attached: true, Host: ml.Name},
	}
	ml.GPUs = []model.GPU{
		{Index: 0, Util: 94, MemUsed: 22140, MemTotal: 24564, Name: "RTX 4090"},
		{Index: 1, Util: 3, MemUsed: 512, MemTotal: 24564, Name: "RTX 4090"},
	}
	ml.Agents = []model.AgentSession{
		{Agent: model.Claude, SID: "a1", CWD: "/home/dev/ai-agent", MTime: now - 30, Title: "explain what the codebook is doing here, intuitively", Host: ml.Name},
		{Agent: model.Codex, SID: "b2", CWD: "/home/dev/data-pipeline", MTime: now - 300, Title: "add a retry with backoff to the fetch step", Host: ml.Name},
		{Agent: model.Claude, SID: "c3", CWD: "/home/dev/tracker", MTime: now - 1080, Title: "pull the feature-distillation discussion", Host: ml.Name},
		{Agent: model.Claude, SID: "d4", CWD: "/home/dev/dataset", MTime: now - 7200, Title: "downloading dataset; labels are missing for the val split", Host: ml.Name},
	}

	prod := reach("prod-api", "us-east-1")
	prod.Agents = []model.AgentSession{
		{Agent: model.Claude, SID: "e5", CWD: "/srv/api", MTime: now - 3600, Title: "summarize the load-test results", Host: prod.Name},
		{Agent: model.Codex, SID: "f6", CWD: "/srv/tmp", MTime: now - 5400, Title: "review the deploy script", Host: prod.Name},
	}

	research := reach("research-box", "eu-central-1")
	research.GPUs = []model.GPU{
		{Index: 0, Util: 61, MemUsed: 15800, MemTotal: 40960, Name: "A100"},
	}
	research.Tmux = []model.TmuxSession{{Name: "notebook", Windows: "1", Attached: true, Host: research.Name}}
	research.Agents = []model.AgentSession{
		{Agent: model.Codex, SID: "g7", CWD: "/data/diffusion", MTime: now - 90000, Title: "speed up the sampler", Host: research.Name},
	}

	gpu2 := reach("gpu-node-2", "us-west-2")
	gpu2.Agents = []model.AgentSession{
		{Agent: model.Claude, SID: "h8", CWD: "/home/dev/renderer", MTime: now - 260000, Title: "refactor the render pipeline", Host: gpu2.Name},
	}

	return []model.Host{
		ml,
		prod,
		research,
		gpu2,
		{Name: "staging", Reachable: true, Scanned: true, Hostname: "us-east-1", Now: now},
		{Name: "db-primary", Reachable: false, Scanned: true, AuthRequired: true, Err: "needs interactive login (password/key)"},
		{Name: "edge-01", Reachable: false, Scanned: true, Err: "Connection timed out"},
		{Name: "old-box", Reachable: false, Scanned: true, Err: "Connection timed out during banner exchange"},
	}
}
