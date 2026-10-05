package execution

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

const CoddyContentPolicy = "coddy_text_v2"

type coddyAgentExecutor struct {
	profile Profile
	lookup  func(string) (string, bool)
}

func (e *coddyAgentExecutor) Run(ctx context.Context, task Task) (result Result, err error) {
	result, packet, err := prepare(e.profile, task)
	if err != nil {
		return result, err
	}
	defer func() { finish(&result, err) }()
	result.ContentPolicy = CoddyContentPolicy
	ctx, cancel := context.WithTimeout(ctx, duration(e.profile))
	defer cancel()
	info, err := os.Stat(task.Workspace)
	if err != nil || !info.IsDir() {
		return result, errors.New("workspace unavailable")
	}
	home, err := os.MkdirTemp("", "research-coddy-agent-")
	if err != nil {
		return result, errors.New("cannot prepare coddy-agent home")
	}
	defer os.RemoveAll(home)
	for _, dir := range []string{"config", "data", "cache", "skills", "sessions"} {
		if err := os.Mkdir(filepath.Join(home, dir), 0700); err != nil {
			return result, errors.New("cannot prepare coddy-agent directories")
		}
	}
	spec := e.profile.External
	env := []string{"HOME=" + home, "CODDY_HOME=" + home, "PATH=" + spec.SearchPath,
		"XDG_CONFIG_HOME=" + filepath.Join(home, "config"), "XDG_DATA_HOME=" + filepath.Join(home, "data"),
		"XDG_CACHE_HOME=" + filepath.Join(home, "cache"), "LANG=C.UTF-8", "NO_COLOR=1"}
	probe, stopProbe := context.WithTimeout(ctx, 5*time.Second)
	version, err := runProcess(probe, spec.Executable, []string{"--version"}, env, home, "", 4096)
	stopProbe()
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(string(version)) != spec.ExpectedVersion {
		return result, errors.New("coddy-agent version mismatch")
	}
	token, err := secret(e.lookup, spec.SecretEnv["OPENAI_API_KEY"])
	if err != nil {
		return result, err
	}
	if token != "" {
		env = append(env, "OPENAI_API_KEY="+token)
	}
	// Keep credentials in the child's environment, not in the configuration file.
	configPath := filepath.Join(home, "agent.json")
	if err := os.WriteFile(configPath, coddyAgentConfig(e.profile, home), 0600); err != nil {
		return result, errors.New("cannot write coddy-agent configuration")
	}
	args := []string{"acp", "--config", configPath, "--home", home, "--cwd", task.Workspace,
		"--sessions-dir", filepath.Join(home, "sessions"), "--mcp-project-trust", "deny",
		"--skills-auto-discovery=false", "--log-output", "stderr", "--log-level", "warn"}
	program := spec.Executable
	if spec.ExecutionBoundary == "docker" {
		var cleanup func() error
		program, args, cleanup, err = coddyContainer(*spec, home, task.Workspace, env, args)
		if err != nil {
			return result, err
		}
		defer func() {
			if cleanupErr := cleanup(); cleanupErr != nil {
				result.Events = append(result.Events, Event{Type: "container_cleanup_failed"})
				err = errors.Join(err, cleanupErr)
				result.Candidate = ""
			}
		}()
	}
	result.Events = append(result.Events, Event{Type: "process_start_requested"})
	err = withACPProcessDiagnostics(ctx, program, args, env, home, e.profile.Limits.MaxOutputBytes,
		func(report acpProcessReport) { recordProcessDiagnostics(ctx, &result, report, token) },
		func(turnCtx context.Context, writer io.Writer, reader io.Reader) error {
			sessionCtx, stop := context.WithCancel(turnCtx)
			defer stop()
			client := &coddyACPClient{model: "research/" + spec.Model, cancel: stop,
				limit: e.profile.Limits.MaxOutputBytes}
			client.emit = func(event Event) {
				event = maskedEvent(event, token)
				event.Input, event.Output = Preview(event.Input, 8192), Preview(event.Output, 8192)
				if len(client.events) < 2000 {
					client.events = append(client.events, event)
				}
				Publish(ctx, event)
			}
			wire := newCoddyWireReader(reader, client, e.profile.Limits.MaxOutputBytes)
			conn := acp.NewClientSideConnection(client, writer, wire)
			conn.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
			stageStarted := time.Now()
			init, rpcErr := conn.Initialize(sessionCtx, acp.InitializeRequest{ProtocolVersion: 1})
			recordACPFailure(ctx, &result, "initialize", stageStarted, rpcErr, token)
			if rpcErr != nil || init.ProtocolVersion != 1 || init.AgentInfo == nil ||
				init.AgentInfo.Name != "coddy-agent" || init.AgentInfo.Version != spec.ExpectedVersion {
				result.Events = append(result.Events, Event{Type: "acp_initialize_failed"})
				return ErrProtocol
			}
			client.mu.Lock()
			client.creating = true
			client.mu.Unlock()
			stageStarted = time.Now()
			session, rpcErr := conn.NewSession(sessionCtx, acp.NewSessionRequest{
				Cwd: task.Workspace, McpServers: []acp.McpServer{}})
			recordACPFailure(ctx, &result, "session", stageStarted, rpcErr, token)
			if rpcErr != nil || session.SessionId == "" || len(session.SessionId) > 256 {
				result.Events = append(result.Events, Event{Type: "acp_session_failed"})
				return ErrProtocol
			}
			client.mu.Lock()
			if client.err != nil || (client.earlySession != "" && client.earlySession != session.SessionId) {
				client.mu.Unlock()
				return ErrProtocol
			}
			client.session = session.SessionId
			client.creating = false
			client.mu.Unlock()
			result.SessionID = string(session.SessionId)
			result.Events = append(result.Events, Event{Type: "acp_session_created"})
			for _, setting := range [][2]string{{"mode", "ask"}, {"model", client.model}, {"permission_mode", "ask"}} {
				stageStarted = time.Now()
				response, rpcErr := conn.SetSessionConfigOption(sessionCtx, acp.SetSessionConfigOptionRequest{
					ValueId: &acp.SetSessionConfigOptionValueId{SessionId: session.SessionId,
						ConfigId: acp.SessionConfigId(setting[0]), Value: acp.SessionConfigValueId(setting[1])}})
				recordACPFailure(ctx, &result, "configure:"+setting[0], stageStarted, rpcErr, token)
				if rpcErr != nil || !coddyOptionEquals(response.ConfigOptions, setting[0], setting[1]) {
					result.Events = append(result.Events, Event{Type: "acp_configuration_failed"})
					return ErrProtocol
				}
				if !coddyOptionEquals(response.ConfigOptions, "mode", "ask") {
					return ErrProtocol
				}
				if setting[0] == "permission_mode" &&
					!coddyOptionEquals(response.ConfigOptions, "model", client.model) {
					return ErrProtocol
				}
			}
			client.mu.Lock()
			client.running = true
			client.mu.Unlock()
			result.RemoteOutcome = "unknown"
			result.Events = append(result.Events, Event{Type: "acp_prompt_started"})
			stageStarted = time.Now()
			response, rpcErr := conn.Prompt(sessionCtx, acp.PromptRequest{
				SessionId: session.SessionId, Prompt: []acp.ContentBlock{acp.TextBlock(packet)}})
			recordACPFailure(ctx, &result, "prompt", stageStarted, rpcErr, token)
			client.mu.Lock()
			defer client.mu.Unlock()
			client.flushText()
			result.Events = append(result.Events, client.events...)
			result.Partial = strings.ReplaceAll(client.text.String(), tokenOrImpossible(token), "[REDACTED]")
			result.Usage = client.reportedUsage()
			if result.Usage != nil && (rpcErr != nil || client.err != nil || response.StopReason == acp.StopReasonCancelled) {
				result.Usage.Incomplete = true
			}
			if client.denied {
				result.Events = append(result.Events, Event{Type: "permission_denied"})
			}
			if client.err != nil {
				return client.err
			}
			if rpcErr != nil {
				result.Events = append(result.Events, Event{Type: "acp_prompt_failed"})
				return ErrProtocol
			}
			result.RemoteOutcome = "response_received"
			if response.Usage != nil {
				u := response.Usage
				if u.InputTokens < 0 || u.OutputTokens < 0 || u.InputTokens > 1000000000 || u.OutputTokens > 1000000000 {
					return ErrProtocol
				}
				result.Usage = &Usage{InputTokens: int64(u.InputTokens), OutputTokens: int64(u.OutputTokens), Source: "acp_response"}
				record(ctx, &result, Event{Type: "usage", Usage: result.Usage})
			}
			switch response.StopReason {
			// Coddy 1.1.64 uses max_turns instead of ACP's max_turn_requests.
			case acp.StopReasonMaxTokens:
				result.Events = append(result.Events, Event{Type: "acp_token_limit_reached"})
				return ErrLimit
			case acp.StopReasonMaxTurnRequests, acp.StopReason("max_turns"):
				result.Events = append(result.Events, Event{Type: "acp_turn_limit_reached"})
				return ErrLimit
			case acp.StopReasonCancelled:
				return context.Canceled
			case acp.StopReasonEndTurn:
				if strings.TrimSpace(client.text.String()) == "" {
					result.Events = append(result.Events, Event{Type: "acp_empty_response"})
					return ErrProtocol
				}
			default:
				return ErrProtocol
			}
			result.Candidate = client.text.String()
			result.Partial = ""
			result.Events = append(result.Events, Event{Type: "acp_turn_completed"})
			return nil
		})
	result.Events = append(result.Events, Event{Type: "process_stopped"})
	if token != "" {
		result.Candidate = strings.ReplaceAll(result.Candidate, token, "[REDACTED]")
		result.Partial = strings.ReplaceAll(result.Partial, token, "[REDACTED]")
		result.SessionID = strings.ReplaceAll(result.SessionID, token, "[REDACTED]")
	}
	if len(result.Candidate) > e.profile.Limits.MaxOutputBytes {
		err = ErrLimit
	}
	return result, err
}

func coddyAgentConfig(p Profile, home string) []byte {
	key := ""
	if p.External.SecretEnv["OPENAI_API_KEY"] != "" {
		key = "${OPENAI_API_KEY}"
	}
	model := "research/" + p.External.Model
	// Use structured JSON; Coddy's YAML loader accepts this representation.
	config := map[string]any{
		"providers": []any{map[string]any{"name": "research", "type": "openai",
			"api_base": p.External.BaseURL, "api_key": key, "proxy": "none", "usage_limits_panel": false}},
		"models": []any{map[string]any{"model": model, "max_tokens": p.Limits.MaxOutputTokens,
			"reasoning_levels": []string{}}},
		"agent": map[string]any{"model": model, "max_turns": p.Limits.MaxSteps,
			"llm_retry_max": 0, "wait_for_limit_reset": false},
		"compaction":  map[string]any{"enable": false},
		"memory":      map[string]any{"enable": false},
		"rules":       map[string]any{"auto_discover": false},
		"skills":      map[string]any{"dirs": []string{filepath.Join(home, "skills")}, "auto_discovery": false},
		"mcp_servers": []any{}, "mcp": map[string]any{"project_trust": "deny"},
		"subagents": map[string]any{"enable": false, "project_trust": "deny"},
		"hooks":     map[string]any{"enable": false, "project_trust": "deny"},
		"scheduler": map[string]any{"enable": false},
		"tools":     map[string]any{"permission_mode": "ask"},
		"logger":    map[string]any{"level": "warn", "outputs": []string{"stderr"}, "format": "json"},
	}
	data, _ := json.Marshal(config)
	return data
}

func coddyOptionEquals(options []acp.SessionConfigOption, id, value string) bool {
	found := false
	for _, option := range options {
		if option.Select != nil && string(option.Select.Id) == id {
			if found || string(option.Select.CurrentValue) != value {
				return false
			}
			found = true
		}
	}
	return found
}

type coddyACPClient struct {
	usage        Usage
	usageSeen    bool
	usageUnknown bool
	usageTotal   int64
	mu           sync.Mutex
	session      acp.SessionId
	earlySession acp.SessionId
	creating     bool
	model        string
	running      bool
	text         strings.Builder
	denied       bool
	err          error
	limit        int
	cancel       context.CancelFunc
	emit         func(Event)
	events       []Event
	pendingText  strings.Builder
}

func tokenOrImpossible(token string) string {
	if token == "" {
		return "\x00"
	}
	return token
}

func (c *coddyACPClient) action(event Event) {
	if c.emit != nil {
		c.emit(event)
	}
}
func (c *coddyACPClient) flushText() {
	if c.pendingText.Len() > 0 {
		c.action(Event{Type: "assistant_message", Output: c.pendingText.String()})
		c.pendingText.Reset()
	}
}
func actionJSON(value any) string {
	if value == nil {
		return ""
	}
	body, _ := json.Marshal(value)
	return string(body)
}

func (c *coddyACPClient) reject(err error) error {
	if c.err == nil {
		c.err = err
	}
	c.cancel()
	return err
}

func (c *coddyACPClient) SessionUpdate(_ context.Context, n acp.SessionNotification) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Coddy sends its command catalog before the session/new response.
	// Bind that provisional identifier only after the response confirms it.
	if c.creating && c.session == "" && n.Update.AvailableCommandsUpdate != nil {
		if n.SessionId == "" || len(n.SessionId) > 256 ||
			(c.earlySession != "" && c.earlySession != n.SessionId) {
			return c.reject(ErrProtocol)
		}
		c.earlySession = n.SessionId
		return nil
	}
	if c.session == "" || n.SessionId != c.session {
		return c.reject(ErrProtocol)
	}
	u := n.Update
	if c.running && u.ToolCall != nil {
		t := u.ToolCall
		output := t.RawOutput
		if output == nil && len(t.Content) > 0 {
			output = t.Content
		}
		c.action(Event{Type: "tool_started", Tool: t.Title, CallID: string(t.ToolCallId), Status: string(t.Status),
			Input: actionJSON(t.RawInput), Output: actionJSON(output)})
	}
	if c.running && u.ToolCallUpdate != nil {
		t := u.ToolCallUpdate
		title, status := "", ""
		if t.Title != nil {
			title = *t.Title
		}
		if t.Status != nil {
			status = string(*t.Status)
		}
		output := t.RawOutput
		if output == nil && len(t.Content) > 0 {
			output = t.Content
		}
		c.action(Event{Type: "tool_updated", Tool: title, CallID: string(t.ToolCallId), Status: status,
			Input: actionJSON(t.RawInput), Output: actionJSON(output)})
	}
	if c.running {
		if u.CurrentModeUpdate != nil && u.CurrentModeUpdate.CurrentModeId != "ask" {
			return c.reject(ErrProtocol)
		}
		if u.ConfigOptionUpdate != nil {
			opts := u.ConfigOptionUpdate.ConfigOptions
			if !coddyOptionEquals(opts, "mode", "ask") || !coddyOptionEquals(opts, "model", c.model) ||
				!coddyOptionEquals(opts, "permission_mode", "ask") {
				return c.reject(ErrProtocol)
			}
		}
	}
	if u.AgentMessageChunk != nil {
		if !c.running || u.AgentMessageChunk.Content.Text == nil {
			return c.reject(ErrProtocol)
		}
		block := u.AgentMessageChunk.Content.Text
		if block.Type == "reasoning" {
			return nil
		}
		if block.Type != "text" {
			return c.reject(ErrProtocol)
		}
		text := u.AgentMessageChunk.Content.Text.Text
		if len(text) > c.limit-c.text.Len() {
			return c.reject(ErrLimit)
		}
		c.text.WriteString(text)
		c.pendingText.WriteString(text)
		if c.pendingText.Len() >= 1024 {
			c.flushText()
		}
	}
	return nil
}

func (c *coddyACPClient) RequestPermission(_ context.Context, p acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if p.SessionId != c.session || c.session == "" {
		return acp.RequestPermissionResponse{}, c.reject(ErrProtocol)
	}
	c.denied = true
	c.action(Event{Type: "permission_denied", CallID: string(p.ToolCall.ToolCallId)})
	return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeCancelled()}, nil
}

func (*coddyACPClient) ReadTextFile(context.Context, acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	return acp.ReadTextFileResponse{}, ErrUnsupported
}
func (*coddyACPClient) WriteTextFile(context.Context, acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	return acp.WriteTextFileResponse{}, ErrUnsupported
}
func (*coddyACPClient) CreateTerminal(context.Context, acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	return acp.CreateTerminalResponse{}, ErrUnsupported
}
func (*coddyACPClient) KillTerminal(context.Context, acp.KillTerminalRequest) (acp.KillTerminalResponse, error) {
	return acp.KillTerminalResponse{}, ErrUnsupported
}
func (*coddyACPClient) TerminalOutput(context.Context, acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	return acp.TerminalOutputResponse{}, ErrUnsupported
}
func (*coddyACPClient) ReleaseTerminal(context.Context, acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	return acp.ReleaseTerminalResponse{}, ErrUnsupported
}
func (*coddyACPClient) WaitForTerminalExit(context.Context, acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	return acp.WaitForTerminalExitResponse{}, ErrUnsupported
}
