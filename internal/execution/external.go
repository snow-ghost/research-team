package execution

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type externalExecutor struct {
	profile Profile
	lookup  func(string) (string, bool)
}

var supportedVersions = map[string]string{
	"codex":       "codex-cli 0.155.1",
	"claude":      "2.1.274 (Claude Code)",
	"opencode":    "1.2.27",
	"coddy-agent": "1.1.64",
}

func (e *externalExecutor) Run(ctx context.Context, task Task) (result Result, err error) {
	result, packet, err := prepare(e.profile, task)
	if err != nil {
		return result, err
	}
	defer func() { finish(&result, err) }()
	ctx, cancel := context.WithTimeout(ctx, duration(e.profile))
	defer cancel()
	info, err := os.Stat(task.Workspace)
	if err != nil || !info.IsDir() {
		return result, errors.New("workspace unavailable")
	}
	home, err := os.MkdirTemp("", "research-agent-home-")
	if err != nil {
		return result, errors.New("cannot create executor home")
	}
	defer os.RemoveAll(home)
	for _, dir := range []string{"config", "data", "cache", "codex"} {
		if err := os.Mkdir(filepath.Join(home, dir), 0700); err != nil {
			return result, errors.New("cannot prepare executor home")
		}
	}
	spec := e.profile.External
	env := []string{
		"HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, "config"),
		"XDG_DATA_HOME=" + filepath.Join(home, "data"), "XDG_CACHE_HOME=" + filepath.Join(home, "cache"),
		"CODEX_HOME=" + filepath.Join(home, "codex"), "PATH=" + spec.SearchPath,
		"LANG=C.UTF-8", "NO_COLOR=1",
	}
	// Probe without passing credentials, before starting a potentially billed run.
	probeCtx, probeCancel := context.WithTimeout(ctx, 5*time.Second)
	version, err := runProcess(probeCtx, spec.Executable, []string{"--version"}, env, home, "", 4096)
	probeCancel()
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(string(version)) != spec.ExpectedVersion {
		return result, errors.New("external agent version mismatch")
	}
	secrets := []string{}
	keys := make([]string, 0, len(spec.SecretEnv))
	for key := range spec.SecretEnv {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value, err := secret(e.lookup, spec.SecretEnv[key])
		if err != nil {
			return result, err
		}
		env = append(env, key+"="+value)
		secrets = append(secrets, value)
	}
	if spec.Provider == "opencode" {
		// This is the V1 configuration. V2 has a different permission schema.
		config := map[string]any{
			"share": "disabled", "autoupdate": false, "plugin": []string{},
			"permission": map[string]string{"*": "deny", "read": "allow", "glob": "allow", "grep": "allow", "list": "allow"},
		}
		data, _ := json.Marshal(config)
		env = append(env, "OPENCODE_CONFIG_CONTENT="+string(data),
			"OPENCODE_DISABLE_AUTOUPDATE=true", "OPENCODE_DISABLE_DEFAULT_PLUGINS=true",
			"OPENCODE_DISABLE_LSP_DOWNLOAD=true")
	}
	result.Events = append(result.Events, Event{Type: "process_started"})
	result.RemoteOutcome = "unknown"
	output, err := runProcess(ctx, spec.Executable, externalArgs(*spec), env, task.Workspace, packet, e.profile.Limits.MaxOutputBytes)
	if err != nil {
		return result, err
	}
	result.Events = append(result.Events, Event{Type: "process_exited"})
	candidate, session, err := parseExternal(spec.Provider, output)
	if err != nil {
		return result, err
	}
	// Exact-value masking is a last line of defense, not a content classifier.
	for _, value := range secrets {
		candidate = strings.ReplaceAll(candidate, value, "[REDACTED]")
		session = strings.ReplaceAll(session, value, "[REDACTED]")
	}
	result.Candidate, result.SessionID = candidate, session
	return result, nil
}

func externalArgs(spec ExternalConfig) []string {
	switch spec.Provider {
	case "codex":
		return []string{"exec", "--json", "--sandbox", "read-only", "--ephemeral", "--ignore-user-config", "--ignore-rules",
			"--skip-git-repo-check", "--color", "never", "--model", spec.Model, "-"}
	case "claude":
		return []string{"--print", "--bare", "--restricted", "--output-format", "stream-json", "--verbose",
			"--tools", "Read", "--allowedTools", "Read", "--disallowedTools", "mcp__*",
			"--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`,
			"--setting-sources", "", "--model", spec.Model}
	case "opencode":
		return []string{"run", "--format", "json", "--model", spec.Model, "--title", "research-attempt"}
	}
	return nil
}

func parseExternal(provider string, output []byte) (string, string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 4096), 16*maxPayload)
	candidate, session := "", ""
	complete := false
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		var frame struct {
			Type            string `json:"type"`
			ThreadID        string `json:"thread_id"`
			SessionID       string `json:"session_id"`
			OpenCodeSession string `json:"sessionID"`
			Subtype         string `json:"subtype"`
			IsError         *bool  `json:"is_error"`
			Result          string `json:"result"`
			Item            struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
			Part struct {
				Text   string `json:"text"`
				Reason string `json:"reason"`
			} `json:"part"`
		}
		if json.Unmarshal(scanner.Bytes(), &frame) != nil || frame.Type == "" {
			return "", "", ErrProtocol
		}
		if frame.Type == "error" || frame.Type == "turn.failed" {
			return "", "", ErrProtocol
		}
		currentSession := ""
		switch provider {
		case "codex":
			currentSession = frame.ThreadID
			switch frame.Type {
			case "turn.started":
				complete = false
				candidate = ""
			case "item.completed":
				if frame.Item.Type == "agent_message" {
					candidate = frame.Item.Text
				}
			case "turn.completed":
				complete = true
			}
		case "claude":
			currentSession = frame.SessionID
			if frame.Type == "result" {
				if frame.Subtype != "success" || frame.IsError == nil || *frame.IsError {
					return "", "", ErrProtocol
				}
				candidate, complete = frame.Result, true
			}
		case "opencode":
			currentSession = frame.OpenCodeSession
			switch frame.Type {
			case "step_start":
				complete = false
				candidate = ""
			case "text":
				candidate += frame.Part.Text
			case "step_finish":
				complete = frame.Part.Reason == "stop"
			}
		default:
			return "", "", ErrUnsupported
		}
		if currentSession != "" {
			if session != "" && session != currentSession {
				return "", "", ErrProtocol
			}
			session = currentSession
		}
	}
	if scanner.Err() != nil || !complete || strings.TrimSpace(candidate) == "" {
		return "", "", ErrProtocol
	}
	return candidate, session, nil
}
