// Package mcp implements `hopmux mcp` — a Model Context Protocol server over
// stdio that exposes hopmux's engine (host/GPU discovery, session inventory,
// remote execution, host-to-host copy, remote session launch) as tools for an
// orchestrating AI agent such as Claude Code. Register it with:
//
//	claude mcp add hopmux -- hopmux mcp
//
// after which a local Claude Code session can answer things like "which server
// has a free GPU?" and act on "move that project to Hinton and rerun it".
//
// The transport is JSON-RPC 2.0, one message per line on stdin/stdout (the MCP
// stdio transport). Only the tools capability is implemented — that is all an
// agent needs here.
package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/isumin/hopmux/core/sshconfig"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// server carries what the tools need: the parsed ssh config.
type server struct {
	version string
	entries []sshconfig.Entry
	hosts   []string

	wmu sync.Mutex
	out *bufio.Writer
}

// Run serves MCP on stdin/stdout until the client disconnects (EOF).
func Run(version string) error {
	entries, err := sshconfig.Parse("~/.ssh/config")
	if err != nil {
		return fmt.Errorf("reading ssh config: %w", err)
	}
	s := &server{version: version, entries: entries, out: bufio.NewWriter(os.Stdout)}
	for _, e := range entries {
		s.hosts = append(s.hosts, e.Alias)
	}

	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var wg sync.WaitGroup
	for sc.Scan() {
		line := sc.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue // not JSON — nothing sane to answer
		}
		// Notifications (no id) expect no response.
		if len(req.ID) == 0 || string(req.ID) == "null" {
			continue
		}
		// Each request runs on its own goroutine so a long tool call (a dataset
		// copy, a slow probe) doesn't block pings or later calls.
		wg.Add(1)
		go func(req rpcRequest) {
			defer wg.Done()
			res := s.handle(req)
			s.wmu.Lock()
			defer s.wmu.Unlock()
			b, _ := json.Marshal(res)
			s.out.Write(b)
			s.out.WriteByte('\n')
			s.out.Flush()
		}(req)
	}
	// stdin closed: finish in-flight calls before exiting so their responses
	// aren't lost (matters for piped/batch use; a live client just disconnects).
	wg.Wait()
	return sc.Err()
}

func (s *server) handle(req rpcRequest) rpcResponse {
	res := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		if p.ProtocolVersion == "" {
			p.ProtocolVersion = "2024-11-05"
		}
		res.Result = map[string]any{
			"protocolVersion": p.ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "hopmux", "version": s.version},
		}
	case "ping":
		res.Result = map[string]any{}
	case "tools/list":
		res.Result = map[string]any{"tools": toolSpecs()}
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			res.Error = &rpcError{Code: -32602, Message: "bad params: " + err.Error()}
			return res
		}
		text, isErr := s.call(p.Name, p.Arguments)
		res.Result = map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
			"isError": isErr,
		}
	default:
		res.Error = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
	return res
}
