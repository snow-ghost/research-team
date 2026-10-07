//go:build linux

package execution

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCoddyLeanMCPBDD_OnlyPinnedToolsAndBoundedArguments(t *testing.T) {
	var checks atomic.Int32
	p := coddyProfile("/usr/bin/true")
	p.External.Tools = []string{"check_lean"}
	p.External.ToolProxy = "/usr/bin/true"
	p.Limits.MaxToolCalls = 1
	ctx := WithTools(context.Background(), RuntimeTool{Name: "check_lean", Description: "Pinned checker", Parameters: json.RawMessage(`{"type":"object"}`), Validate: func(raw string) error {
		if raw != `{"source":"candidate"}` {
			return ErrProtocol
		}
		return nil
	}, Run: func(context.Context, string) (string, error) {
		checks.Add(1)
		return `{"accepted":false,"status":"verified"}`, nil
	}})
	home := t.TempDir()
	servers, closeServer, err := coddyLeanMCP(ctx, p, home)
	if err != nil {
		t.Fatal(err)
	}
	defer closeServer()
	if len(servers) != 1 || len(servers[0].Stdio.Env) != 0 {
		t.Fatal("credential or server configuration changed")
	}
	socket := filepath.Join(home, "tools.sock")
	info, _ := os.Stat(socket)
	if info.Mode().Perm() != 0600 {
		t.Fatal("socket not private")
	}
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr}
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
	if out := call("tools/list", "{}"); !strings.Contains(out, "check_lean") || strings.Contains(out, "run_command") {
		t.Fatal(out)
	}
	for _, params := range []string{`{"name":"run_command","arguments":{"source":"candidate"}}`, `{"name":"check_lean","arguments":{"source":"candidate","goal":"other"}}`} {
		if !strings.Contains(call("tools/call", params), "error") {
			t.Fatal("unsafe tool request allowed")
		}
	}
	if checks.Load() != 0 {
		t.Fatal("unsafe input executed")
	}
	if out := call("tools/call", `{"name":"check_lean","arguments":{"source":"candidate"}}`); !strings.Contains(out, "verified") {
		t.Fatal(out)
	}
	if out := call("tools/call", `{"name":"check_lean","arguments":{"source":"candidate"}}`); !strings.Contains(out, "tool call limit") || checks.Load() != 1 {
		t.Fatal(out, checks.Load())
	}
}
