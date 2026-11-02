package execution

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

const KimiContentPolicy = "kimi_text_v1"

type kimiAgentExecutor struct {
	profile Profile
}

func (e *kimiAgentExecutor) Run(ctx context.Context, task Task) (result Result, err error) {
	result, packet, err := prepare(e.profile, task)
	if err != nil {
		return result, err
	}
	defer func() { finish(&result, err) }()
	result.ContentPolicy = KimiContentPolicy
	ctx, cancel := context.WithTimeout(ctx, duration(e.profile))
	defer cancel()
	info, err := os.Stat(task.Workspace)
	if err != nil || !info.IsDir() {
		return result, errors.New("workspace unavailable")
	}
	spec := e.profile.External
	info, err = os.Stat(spec.ConfigHome)
	if err != nil || !info.IsDir() {
		return result, errors.New("kimi config_home unavailable")
	}
	home, err := os.MkdirTemp("", "research-kimi-agent-")
	if err != nil {
		return result, errors.New("cannot prepare kimi-agent home")
	}
	defer os.RemoveAll(home)
	for _, dir := range []string{"config", "data", "cache"} {
		if err := os.Mkdir(filepath.Join(home, dir), 0700); err != nil {
			return result, errors.New("cannot prepare kimi-agent directories")
		}
	}
	env := []string{"HOME=" + home, "KIMI_CODE_HOME=" + spec.ConfigHome, "PATH=" + spec.SearchPath,
		"XDG_CONFIG_HOME=" + filepath.Join(home, "config"), "XDG_DATA_HOME=" + filepath.Join(home, "data"),
		"XDG_CACHE_HOME=" + filepath.Join(home, "cache"), "LANG=C.UTF-8", "NO_COLOR=1"}
	probe, stopProbe := context.WithTimeout(ctx, 5*time.Second)
	version, err := runProcess(probe, spec.Executable, []string{"--version"}, env, home, "", 4096)
	stopProbe()
	if err != nil {
		return result, err
	}
	actualVersion := strings.TrimSpace(string(version))
	if !acpVersionPattern.MatchString(actualVersion) {
		return result, errors.New("unexpected kimi-agent version response")
	}
	if spec.ExpectedVersion != "" && actualVersion != spec.ExpectedVersion {
		return result, errors.New("kimi-agent version mismatch")
	}
	result.Events = append(result.Events, Event{Type: "process_start_requested"})
	err = withACPProcessDiagnostics(ctx, spec.Executable, []string{"acp"}, env, home, e.profile.Limits.MaxOutputBytes,
		func(report acpProcessReport) { recordProcessDiagnostics(ctx, &result, report, "") },
		func(turnCtx context.Context, writer io.Writer, reader io.Reader) error {
			sessionCtx, stop := context.WithCancel(turnCtx)
			defer stop()
			client := &coddyACPClient{model: spec.Model, mode: "plan", cancel: stop, limit: e.profile.Limits.MaxOutputBytes}
			client.emit = func(event Event) {
				event.Input, event.Output = Preview(event.Input, 8192), Preview(event.Output, 8192)
				if len(client.events) < 2000 {
					client.events = append(client.events, event)
				}
				Publish(ctx, event)
			}
			conn := acp.NewClientSideConnection(client, writer, reader)
			conn.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
			stageStarted := time.Now()
			init, rpcErr := conn.Initialize(sessionCtx, acp.InitializeRequest{ProtocolVersion: 1})
			recordACPFailure(ctx, &result, "initialize", stageStarted, rpcErr, "")
			if rpcErr != nil || init.ProtocolVersion != 1 || init.AgentInfo == nil || init.AgentInfo.Name != "Kimi Code CLI" ||
				!acpVersionPattern.MatchString(init.AgentInfo.Version) || init.AgentInfo.Version != actualVersion ||
				(spec.ExpectedVersion != "" && init.AgentInfo.Version != spec.ExpectedVersion) {
				result.Events = append(result.Events, Event{Type: "acp_initialize_failed"})
				return ErrProtocol
			}
			result.AgentName = init.AgentInfo.Name
			result.AgentVersion = init.AgentInfo.Version
			client.mu.Lock()
			client.creating = true
			client.mu.Unlock()
			stageStarted = time.Now()
			session, rpcErr := conn.NewSession(sessionCtx, acp.NewSessionRequest{Cwd: task.Workspace, McpServers: []acp.McpServer{}})
			recordACPFailure(ctx, &result, "session", stageStarted, rpcErr, "")
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
			defer func() {
				closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer closeCancel()
				_, _ = conn.CloseSession(closeCtx, acp.CloseSessionRequest{SessionId: session.SessionId})
			}()
			for _, setting := range [][2]string{{"mode", "plan"}, {"model", spec.Model}} {
				stageStarted = time.Now()
				response, rpcErr := conn.SetSessionConfigOption(sessionCtx, acp.SetSessionConfigOptionRequest{
					ValueId: &acp.SetSessionConfigOptionValueId{SessionId: session.SessionId,
						ConfigId: acp.SessionConfigId(setting[0]), Value: acp.SessionConfigValueId(setting[1])}})
				recordACPFailure(ctx, &result, "configure:"+setting[0], stageStarted, rpcErr, "")
				if rpcErr != nil || !coddyOptionEquals(response.ConfigOptions, setting[0], setting[1]) ||
					!coddyOptionEquals(response.ConfigOptions, "mode", "plan") {
					result.Events = append(result.Events, Event{Type: "acp_configuration_failed"})
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
			recordACPFailure(ctx, &result, "prompt", stageStarted, rpcErr, "")
			client.mu.Lock()
			defer client.mu.Unlock()
			client.flushText()
			result.Events = append(result.Events, client.events...)
			result.Partial = client.text.String()
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
			case acp.StopReasonMaxTokens:
				result.Events = append(result.Events, Event{Type: "acp_token_limit_reached"})
				return ErrLimit
			case acp.StopReasonMaxTurnRequests:
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
	if len(result.Candidate) > e.profile.Limits.MaxOutputBytes {
		err = ErrLimit
	}
	return result, err
}
