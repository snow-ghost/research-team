package execution

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

// Stdio carries only MCP JSON-RPC. The Unix socket exposes no filesystem or shell operation.
func RunToolProxy(ctx context.Context, socket string, in io.Reader, out io.Writer) error {
	if !filepath.IsAbs(socket) {
		return ErrProtocol
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 75 * time.Second}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), 256<<10)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if !json.Valid(line) {
			return ErrProtocol
		}
		req, err := http.NewRequestWithContext(ctx, "POST", "http://tool/rpc", bytes.NewReader(line))
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return errors.New("tool transport unavailable")
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, (128<<10)+1))
		resp.Body.Close()
		if readErr != nil || resp.StatusCode != 200 || len(body) > 128<<10 {
			return ErrProtocol
		}
		if len(body) > 0 {
			if !json.Valid(body) {
				return ErrProtocol
			}
			if _, err = out.Write(append(bytes.TrimSpace(body), '\n')); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

func coddyLeanMCP(ctx context.Context, p Profile, home string) ([]acp.McpServer, func(), error) {
	if len(p.External.Tools) == 0 {
		return []acp.McpServer{}, func() {}, nil
	}
	handlers := map[string]RuntimeTool{}
	for _, name := range p.External.Tools {
		h, ok := runtimeTool(ctx, name)
		if !ok {
			return nil, nil, ErrUnsupported
		}
		handlers[name] = h
	}
	proxy := filepath.Join(home, "tool-proxy")
	src, err := os.Open(p.External.ToolProxy)
	if err != nil {
		return nil, nil, errors.New("trusted tool proxy unavailable")
	}
	defer src.Close()
	dst, err := os.OpenFile(proxy, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0500)
	if err != nil {
		return nil, nil, err
	}
	n, copyErr := io.Copy(dst, io.LimitReader(src, 128<<20))
	closeErr := dst.Close()
	if copyErr != nil || closeErr != nil || n >= 128<<20 {
		return nil, nil, errors.New("tool proxy copy failed")
	}
	socket := filepath.Join(home, "tools.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return nil, nil, err
	}
	if err = os.Chmod(socket, 0600); err != nil {
		listener.Close()
		return nil, nil, err
	}
	var mu sync.Mutex
	calls := 0
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 75 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/rpc" {
			http.Error(w, "denied", 403)
			return
		}
		var frame struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256<<10))
		if err != nil || json.Unmarshal(body, &frame) != nil || frame.JSONRPC != "2.0" {
			http.Error(w, "invalid", 400)
			return
		}
		if len(frame.ID) == 0 {
			if frame.Method != "notifications/initialized" && frame.Method != "notifications/cancelled" {
				http.Error(w, "invalid", 400)
			}
			return
		}
		var result any
		var rpcError any
		switch frame.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "research-lean", "version": "1"}}
		case "ping":
			result = map[string]any{}
		case "tools/list":
			tools := []any{}
			for _, name := range p.External.Tools {
				h := handlers[name]
				tools = append(tools, map[string]any{"name": name, "description": h.Description, "inputSchema": h.Parameters})
			}
			result = map[string]any{"tools": tools}
		case "tools/call":
			var call struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if json.Unmarshal(frame.Params, &call) != nil {
				rpcError = map[string]any{"code": -32602, "message": "invalid arguments"}
				break
			}
			h, ok := handlers[call.Name]
			if !ok || h.Validate(string(call.Arguments)) != nil {
				rpcError = map[string]any{"code": -32602, "message": "tool or arguments denied"}
				break
			}
			mu.Lock()
			if calls >= p.Limits.MaxToolCalls {
				mu.Unlock()
				result = map[string]any{"isError": true, "content": []any{map[string]string{"type": "text", "text": "tool call limit"}}}
				break
			}
			calls++
			// The pinned compiler is sequential; no model-controlled parallel compilation.
			output, toolErr := h.Run(r.Context(), string(call.Arguments))
			mu.Unlock()
			if len(output) > 64000 {
				output = "tool output limit"
				toolErr = ErrLimit
			}
			result = map[string]any{"isError": toolErr != nil, "content": []any{map[string]string{"type": "text", "text": output}}}
		default:
			rpcError = map[string]any{"code": -32601, "message": "method denied"}
		}
		w.Header().Set("Content-Type", "application/json")
		response := map[string]any{"jsonrpc": "2.0", "id": frame.ID}
		if rpcError != nil {
			response["error"] = rpcError
		} else {
			response["result"] = result
		}
		_ = json.NewEncoder(w).Encode(response)
	})}
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	closeServer := func() { _ = server.Close(); <-done }
	return []acp.McpServer{{Stdio: &acp.McpServerStdio{Name: "research", Command: proxy, Args: []string{"--tool-proxy", socket}, Env: []acp.EnvVariable{}}}}, closeServer, nil
}
