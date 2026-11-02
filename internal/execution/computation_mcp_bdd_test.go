//go:build linux

package execution

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

type graphValidationError struct{}

func (graphValidationError) Error() string { return "graph mismatch" }
func (graphValidationError) ExpectedGraph() any {
	return map[string]any{"degree": 3, "cycle_length": 2}
}

func TestCoddyComputationMCPBDD_BoundedBFSToolUsesPinnedHandler(t *testing.T) {
	var calls atomic.Int32
	p := coddyProfile("/usr/bin/true")
	p.External.ExpectedVersion = "1.2.54"
	p.External.Tools = []string{"run_bfs"}
	p.External.ToolProxy = "/usr/bin/true"
	p.Limits.MaxToolCalls = 1
	if err := p.Validate(); err != nil {
		t.Fatal("Coddy computation profile rejected", err)
	}
	ctx := WithTools(context.Background(), RuntimeTool{Name: "run_bfs", Description: "Bounded BFS", Parameters: json.RawMessage(`{"type":"object","properties":{"graph":{"type":"object"},"targets":{"type":"array"}},"required":["graph","targets"],"additionalProperties":false}`), Validate: func(raw string) error {
		var args map[string]any
		if json.Unmarshal([]byte(raw), &args) != nil || args["graph"] == nil || args["targets"] == nil {
			return ErrProtocol
		}
		if args["graph"].(map[string]any)["degree"] == float64(2) {
			return graphValidationError{}
		}
		return nil
	}, Run: func(context.Context, string) (string, error) {
		calls.Add(1)
		return `{"status":"completed","accepted":false,"targets":[{"distance":2,"word":["swap","rotate"]}]}`, nil
	}})
	home := t.TempDir()
	servers, closeServer, err := coddyLeanMCP(ctx, p, home)
	if err != nil {
		t.Fatal(err)
	}
	defer closeServer()
	if len(servers) != 1 || len(servers[0].Stdio.Env) != 0 {
		t.Fatal("unexpected MCP process configuration")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(home, "tools.sock"))
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	call := func(method, params string) string {
		t.Helper()
		resp, err := client.Post("http://tool/rpc", "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"`+method+`","params":`+params+`}`))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return string(body)
	}
	if output := call("tools/list", "{}"); !strings.Contains(output, "run_bfs") || strings.Contains(output, "run_command") {
		t.Fatal(output)
	}
	params := `{"name":"run_bfs","arguments":{"graph":{"degree":3},"targets":[[2,1,0]]}}`
	if output := call("tools/call", params); !strings.Contains(output, `\"distance\":2`) || !strings.Contains(output, `\"accepted\":false`) {
		t.Fatal(output)
	}
	invalid := `{"name":"run_bfs","arguments":{"graph":{"degree":2},"targets":[[2,1,0]]}}`
	if output := call("tools/call", invalid); !strings.Contains(output, "graph_mismatch") || !strings.Contains(output, "expected_graph") || calls.Load() != 1 {
		t.Fatal("graph mismatch feedback missing", output)
	}
	if output := call("tools/call", params); !strings.Contains(output, "tool call limit") || calls.Load() != 1 {
		t.Fatal("MCP call limit not enforced", output)
	}
}
