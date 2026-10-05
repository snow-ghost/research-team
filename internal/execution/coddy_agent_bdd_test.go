//go:build linux

package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func coddyProfile(executable string) Profile {
	return Profile{ID: "coddy-reader", Kind: "external",
		Skills: []Skill{{ID: "audit", Version: "1", Instructions: "Check all assumptions."}},
		Limits: Limits{TimeoutSeconds: 10, MaxSteps: 2, MaxOutputTokens: 512, MaxOutputBytes: 1 << 20},
		External: &ExternalConfig{Provider: "coddy-agent", Executable: executable, ExpectedVersion: "1.1.64",
			Model: "test/model", BaseURL: "https://provider.example/v1", SearchPath: "/usr/bin:/bin",
			ExecutionBoundary: "operator_managed", SecretEnv: map[string]string{"OPENAI_API_KEY": "TEST_TOKEN"}}}
}

func coddyFixture(t *testing.T, scenario string) string {
	t.Helper()
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return fixture(t, fmt.Sprintf("exec env GORACE=atexit_sleep_ms=0 %q -test.run=^TestCoddyACPHelper$ -- %s \"$@\"\n", testBinary, scenario))
}

// The subprocess is a deliberately small protocol peer, not an agent implementation.
func TestCoddyACPHelper(t *testing.T) {
	marker := -1
	for i, arg := range os.Args {
		if arg == "--" {
			marker = i
			break
		}
	}
	if marker < 0 {
		return
	}
	scenario, args := os.Args[marker+1], os.Args[marker+2:]
	exit := func() { os.Exit(21) }
	if os.Getenv("UNRELATED_SECRET") != "" {
		exit()
	}
	if reflect.DeepEqual(args, []string{"--version"}) {
		if os.Getenv("OPENAI_API_KEY") != "" {
			exit()
		}
		if scenario == "wrong_version" {
			fmt.Println("wrong-version")
		} else {
			fmt.Println("1.1.64")
		}
		os.Exit(0)
	}
	if len(args) < 9 || args[0] != "acp" || os.Getenv("OPENAI_API_KEY") != "private-test-token" {
		exit()
	}
	if scenario == "not_reading" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	if scenario == "stdout_overflow" || scenario == "stderr_overflow" {
		var dst io.Writer = os.Stdout
		if scenario == "stderr_overflow" {
			dst = os.Stderr
		}
		_, _ = io.WriteString(dst, strings.Repeat("x", 2<<20))
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	cfgData, err := os.ReadFile(args[2])
	if err != nil || strings.Contains(string(cfgData), "private-test-token") ||
		!strings.Contains(string(cfgData), "${OPENAI_API_KEY}") {
		exit()
	}
	homeInfo, _ := os.Stat(os.Getenv("HOME"))
	cfgInfo, _ := os.Stat(args[2])
	if homeInfo.Mode().Perm() != 0700 || cfgInfo.Mode().Perm() != 0600 {
		exit()
	}
	decoder, encoder := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	send := func(value any) {
		if encoder.Encode(value) != nil {
			os.Exit(22)
		}
	}
	reply := func(id any, value any) { send(map[string]any{"jsonrpc": "2.0", "id": id, "result": value}) }
	state := map[string]string{"mode": "agent", "model": "research/test/model", "permission_mode": "ask"}
	options := func() []any {
		out := []any{}
		for _, id := range []string{"mode", "model", "permission_mode"} {
			out = append(out, map[string]any{"id": id, "name": id, "type": "select",
				"currentValue": state[id], "options": []any{map[string]any{"value": state[id], "name": state[id]}}})
		}
		return out
	}
	notifyContent := func(session, kind, contentType, text string) {
		send(map[string]any{"jsonrpc": "2.0", "method": "session/update",
			"params": map[string]any{"sessionId": session, "update": map[string]any{
				"sessionUpdate": kind, "content": map[string]any{"type": contentType, "text": text}}}})
	}
	notify := func(session, kind, text string) { notifyContent(session, kind, "text", text) }
	tokenUsage := func(session string, input, output, total int64) {
		send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": session, "update": map[string]any{"sessionUpdate": "token_usage", "inputTokens": input, "outputTokens": output, "totalTokens": total}}})
	}
	for {
		var frame struct {
			ID     any
			Method string
			Params json.RawMessage
		}
		if decoder.Decode(&frame) != nil {
			os.Exit(0)
		}
		switch frame.Method {
		case "initialize":
			var p struct {
				ClientCapabilities struct {
					FS       struct{ ReadTextFile, WriteTextFile bool }
					Terminal bool
				}
			}
			_ = json.Unmarshal(frame.Params, &p)
			if p.ClientCapabilities.Terminal || p.ClientCapabilities.FS.ReadTextFile || p.ClientCapabilities.FS.WriteTextFile {
				exit()
			}
			version := 1
			if scenario == "wrong_protocol" {
				version = 9
			}
			reply(frame.ID, map[string]any{"protocolVersion": version,
				"agentInfo": map[string]string{"name": "coddy-agent", "version": "1.1.64"}})
		case "session/new":
			var p struct {
				Cwd        string
				McpServers []any
			}
			_ = json.Unmarshal(frame.Params, &p)
			if !filepath.IsAbs(p.Cwd) || len(p.McpServers) != 0 {
				exit()
			}
			if scenario == "early_catalog" || scenario == "wrong_early_session" {
				sid := "session-one"
				if scenario == "wrong_early_session" {
					sid = "another"
				}
				send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
					"sessionId": sid, "update": map[string]any{"sessionUpdate": "available_commands_update", "availableCommands": []any{}}}})
			}
			reply(frame.ID, map[string]any{"sessionId": "session-one", "configOptions": options()})
		case "session/set_config_option":
			var p struct{ ConfigID, Value string }
			_ = json.Unmarshal(frame.Params, &p)
			if scenario == "setting_error" {
				send(map[string]any{"jsonrpc": "2.0", "id": frame.ID,
					"error": map[string]any{"code": -32602, "message": "private-test-token"}})
				continue
			}
			state[p.ConfigID] = p.Value
			if scenario == "wrong_model" {
				state["model"] = "wrong"
			}
			if scenario == "model_changed_at_last_setting" && p.ConfigID == "permission_mode" {
				state["model"] = "wrong"
			}
			reply(frame.ID, map[string]any{"configOptions": options()})
		case "session/prompt":
			if scenario == "prompt_error" {
				fmt.Fprintln(os.Stderr, `{"level":"error","message":"provider failed","token":"private-test-token","authorization":"Bearer OTHER_CREDENTIAL"}`)
				send(map[string]any{"jsonrpc": "2.0", "id": frame.ID, "error": map[string]any{
					"code": -32603, "message": "model did not respond; credential private-test-token",
					"data": map[string]any{"request_id": "request-503", "http_status": 503, "authorization": "OTHER_CREDENTIAL", "raw_response": "PRIVATE_ERROR_BODY"},
				}})
				continue
			}
			if scenario == "process_crash" {
				fmt.Fprintln(os.Stderr, "agent crashed: private-test-token")
				os.Exit(42)
			}
			if scenario == "stderr_tail" {
				_, _ = io.WriteString(os.Stderr, strings.Repeat("old diagnostic\n", 6000))
				_, _ = io.WriteString(os.Stderr, "last diagnostic: private-test-")
				_, _ = io.WriteString(os.Stderr, "token\n")
			}
			if scenario == "missing_end" {
				os.Exit(0)
			}
			var p struct{ Prompt []struct{ Text string } }
			_ = json.Unmarshal(frame.Params, &p)
			if len(p.Prompt) != 1 || !strings.Contains(p.Prompt[0].Text, "Check all assumptions.") {
				exit()
			}
			if scenario == "permission" {
				send(map[string]any{"jsonrpc": "2.0", "id": "permission-1", "method": "session/request_permission",
					"params": map[string]any{"sessionId": "session-one",
						"toolCall": map[string]any{"toolCallId": "tool-1", "title": "write"},
						"options":  []any{map[string]string{"optionId": "allow", "name": "Allow", "kind": "allow_always"}}}})
				var response struct {
					Result struct{ Outcome struct{ Outcome string } }
				}
				if decoder.Decode(&response) != nil || response.Result.Outcome.Outcome != "cancelled" {
					exit()
				}
			}
			if scenario == "mode_changed" {
				send(map[string]any{"jsonrpc": "2.0", "method": "session/update",
					"params": map[string]any{"sessionId": "session-one",
						"update": map[string]string{"sessionUpdate": "current_mode_update", "currentModeId": "agent"}}})
			}
			if scenario == "config_changed" {
				state["model"] = "unapproved"
				send(map[string]any{"jsonrpc": "2.0", "method": "session/update",
					"params": map[string]any{"sessionId": "session-one",
						"update": map[string]any{"sessionUpdate": "config_option_update", "configOptions": options()}}})
			}
			session := "session-one"
			if scenario == "wrong_session" {
				session = "another"
			}
			notify(session, "agent_thought_chunk", "not a result")
			if strings.HasPrefix(scenario, "usage_") {
				notifyContent(session, "agent_message_chunk", "reasoning", "SYNTHETIC_PRIVATE_REASONING")
				if scenario == "usage_zero" {
					tokenUsage(session, 0, 0, 0)
				} else {
					tokenUsage(session, 10, 2, 12)
				}
				if scenario == "usage_multi" {
					tokenUsage(session, 5, 3, 20)
				}
				if scenario == "usage_duplicate" {
					tokenUsage(session, 10, 2, 12)
				}
				if scenario == "usage_invalid" {
					tokenUsage(session, -1, 3, 14)
				}
				if scenario == "usage_gap" {
					tokenUsage(session, 0, 0, 12)
				}
			}
			if scenario != "empty" {
				notify(session, "agent_message_chunk", "candidate ")
				notify(session, "agent_message_chunk", "private-test-token")
			}
			reason := "end_turn"
			switch scenario {
			case "max_tokens", "max_turns", "max_turn_requests", "refusal", "cancelled":
				reason = scenario
			}
			reply(frame.ID, map[string]string{"stopReason": reason})
		}
	}
}

func TestCoddyAgentBDD_CompletedTurnIsAnUnverifiedCandidate(t *testing.T) {
	t.Setenv("UNRELATED_SECRET", "must-not-leak")
	for _, scenario := range []string{"success", "permission", "early_catalog"} {
		t.Run(scenario, func(t *testing.T) {
			p := coddyProfile(coddyFixture(t, scenario))
			task := taskFor(t)
			task.Objective = "Literal $(touch SHOULD_NOT_EXIST)"
			r, err := buildTest(t, p).Run(context.Background(), task)
			if err != nil || r.Status != "candidate" || r.Candidate != "candidate [REDACTED]" ||
				r.Usage != nil || r.SessionID != "session-one" || r.ProfileSHA256 == "" || r.PacketSHA256 == "" {
				t.Fatalf("%+v %v", r, err)
			}
			if _, err := os.Stat(filepath.Join(task.Workspace, "SHOULD_NOT_EXIST")); !os.IsNotExist(err) {
				t.Fatal("task executed as shell input")
			}
			found := false
			for _, event := range r.Events {
				found = found || event.Type == "permission_denied"
			}
			if found != (scenario == "permission") {
				t.Fatal("permission decision not recorded")
			}
		})
	}
}

func TestCoddyAgentBDD_IncompleteOrUntrustedTurnsAreRejected(t *testing.T) {
	for _, scenario := range []string{"wrong_protocol", "wrong_model", "setting_error", "missing_end",
		"wrong_session", "wrong_early_session", "mode_changed", "config_changed", "model_changed_at_last_setting",
		"empty", "max_tokens", "max_turns", "max_turn_requests", "refusal", "cancelled", "usage_invalid"} {
		t.Run(scenario, func(t *testing.T) {
			r, err := buildTest(t, coddyProfile(coddyFixture(t, scenario))).Run(context.Background(), taskFor(t))
			if err == nil || r.Candidate != "" || r.Status == "candidate" || strings.Contains(err.Error(), "private-test-token") {
				t.Fatalf("%+v %v", r, err)
			}
			if strings.HasPrefix(scenario, "max_") && !errors.Is(err, ErrLimit) {
				t.Fatalf("expected limit status: %+v %v", r, err)
			}
		})
	}
}

func TestCoddyAgentBDD_VersionIsCheckedBeforeCredentials(t *testing.T) {
	calls := 0
	e, err := Build(coddyProfile(coddyFixture(t, "wrong_version")), func(string) (string, bool) {
		calls++
		return "private-test-token", true
	})
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.Run(context.Background(), taskFor(t))
	if err == nil || calls != 0 || r.RemoteOutcome != "not_started" {
		t.Fatalf("%+v %v calls=%d", r, err, calls)
	}
}

func TestCoddyAgentBDD_TransportHasByteAndTimeLimits(t *testing.T) {
	for _, scenario := range []string{"stdout_overflow", "stderr_overflow", "not_reading"} {
		t.Run(scenario, func(t *testing.T) {
			p := coddyProfile(coddyFixture(t, scenario))
			p.Limits.TimeoutSeconds = 1
			p.Limits.MaxOutputBytes = 4096
			task := taskFor(t)
			task.Context = strings.Repeat("x", 100000)
			start := time.Now()
			r, err := buildTest(t, p).Run(context.Background(), task)
			if err == nil || r.Candidate != "" || time.Since(start) > 4*time.Second {
				t.Fatalf("%+v %v", r, err)
			}
			if scenario == "not_reading" && r.Status != "timed_out" {
				t.Fatalf("%+v", r)
			}
			if scenario != "not_reading" && r.Status != "limit_reached" {
				t.Fatalf("%+v", r)
			}
		})
	}
}

func TestCoddyAgentBDD_InvalidConfigurationIsRejected(t *testing.T) {
	for _, mutate := range []func(*Profile){
		func(p *Profile) { p.External.BaseURL = "http://example.com/v1" },
		func(p *Profile) { p.External.BaseURL = "https://name:password@example.com/v1" },
		func(p *Profile) { p.External.BaseURL = "https://${OPENAI_API_KEY}.example.com/v1" },
		func(p *Profile) { p.External.SecretEnv["ANTHROPIC_API_KEY"] = "OTHER" },
		func(p *Profile) { p.Limits.MaxSteps = 0 },
		func(p *Profile) { p.Limits.MaxToolCalls = 1 },
		func(p *Profile) { p.External.Model = "${OPENAI_API_KEY}" },
	} {
		p := coddyProfile("/bin/false")
		mutate(&p)
		if p.Validate() == nil {
			t.Fatal("invalid profile accepted")
		}
	}
}

func TestCoddyAgentBDD_CancellationStopsTheProcessGroup(t *testing.T) {
	dir := t.TempDir()
	executable := fixture(t, "sleep 60 &\nprintf '%s' \"$!\" > child.pid\nwait\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- withACPProcess(ctx, executable, nil, []string{"PATH=/usr/bin:/bin"}, dir, 1024,
			func(ctx context.Context, _ io.Writer, reader io.Reader) error {
				_, _ = io.Copy(io.Discard, reader)
				return ctx.Err()
			})
	}()
	pid := 0
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(filepath.Join(dir, "child.pid"))
		pid, _ = strconv.Atoi(string(data))
		if pid > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ACP process did not stop")
	}
	if pid == 0 {
		t.Fatal("child did not start")
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if os.IsNotExist(err) || strings.Contains(string(data), ") Z ") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Fatal("child remained running")
}

func TestCoddyAgentBDD_RealBinaryWithLocalModel(t *testing.T) {
	binary := os.Getenv("CODDY_AGENT_TEST_BINARY")
	if binary == "" {
		t.Skip("set CODDY_AGENT_TEST_BINARY and optionally CODDY_AGENT_TEST_VERSION; only a local model stub is used")
	}
	for _, reason := range []string{"stop", "length"} {
		t.Run(reason, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/models" {
					_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"test/model","object":"model"}]}`)
					return
				}
				if r.URL.Path != "/v1/chat/completions" {
					http.NotFound(w, r)
					return
				}
				calls.Add(1)
				if r.Header.Get("Authorization") != "Bearer private-test-token" {
					t.Error("incorrect credential")
				}
				var req struct {
					Model         string                                     `json:"model"`
					MaxTokens     int                                        `json:"max_tokens"`
					Messages      []struct{ Content any }                    `json:"messages"`
					Tools         []struct{ Function struct{ Name string } } `json:"tools"`
					Stream        bool                                       `json:"stream"`
					StreamOptions struct {
						IncludeUsage bool `json:"include_usage"`
					} `json:"stream_options"`
				}
				if json.NewDecoder(r.Body).Decode(&req) != nil || req.Model != "test/model" || req.MaxTokens != 16384 || !req.Stream || !req.StreamOptions.IncludeUsage {
					t.Error("incorrect model request")
				}
				data, _ := json.Marshal(req.Messages)
				if !strings.Contains(string(data), "Check all assumptions.") {
					t.Error("skills missing")
				}
				for _, tool := range req.Tools {
					switch tool.Function.Name {
					case "run_command", "write", "edit", "spawn_agent", "load_skill":
						t.Errorf("unexpected tool: %s", tool.Function.Name)
					}
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"SYNTHETIC_PRIVATE_REASONING\"},\"finish_reason\":null}]}\n\n")
				fmt.Fprintf(w, "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Candidate lemma\"},\"finish_reason\":null}]}\n\n")
				fmt.Fprintf(w, "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\n", reason)
				fmt.Fprint(w, "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":7,\"total_tokens\":18}}\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()
			p := coddyProfile(binary)
			p.Limits.MaxOutputTokens = 16384
			if version := os.Getenv("CODDY_AGENT_TEST_VERSION"); version != "" {
				p.External.ExpectedVersion = version
			}
			p.External.BaseURL, p.External.AllowLoopbackHTTP = server.URL+"/v1", true
			home := t.TempDir()
			path := filepath.Join(home, "config.json")
			if err := os.WriteFile(path, coddyAgentConfig(p, home), 0600); err != nil {
				t.Fatal(err)
			}
			check := exec.Command(binary, "acp", "--config", path, "--home", home, "-t")
			check.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin", "OPENAI_API_KEY=private-test-token"}
			if output, err := check.CombinedOutput(); err != nil {
				t.Fatalf("offline config check: %v\n%s", err, output)
			}
			if calls.Load() != 0 {
				t.Fatal("offline check contacted the model")
			}
			result, err := buildTest(t, p).Run(context.Background(), taskFor(t))
			if reason == "stop" {
				if err != nil || result.Status != "candidate" || result.Candidate != "Candidate lemma" {
					t.Fatalf("%+v %v", result, err)
				}
			} else if !errors.Is(err, ErrLimit) || result.Candidate != "" {
				t.Fatalf("%+v %v", result, err)
			}
			if calls.Load() != 1 {
				t.Fatalf("unexpected model calls: %d", calls.Load())
			}
			if result.Usage == nil || result.Usage.InputTokens != 11 || result.Usage.OutputTokens != 7 || result.Usage.Incomplete || result.Usage.Source != "coddy_token_usage" {
				t.Fatalf("usage missing: %+v", result)
			}
			if strings.Contains(result.Candidate+result.Partial, "SYNTHETIC_PRIVATE_REASONING") {
				t.Fatal("reasoning leaked into result")
			}
			for _, event := range result.Events {
				if strings.Contains(event.Output, "SYNTHETIC_PRIVATE_REASONING") {
					t.Fatal("reasoning leaked into events")
				}
			}
		})
	}
}
func TestCoddyAgentBDD_ReasoningIsExcludedAndUsageIsMeasured(t *testing.T) {
	for _, tc := range []struct {
		scenario          string
		input, output     int64
		known, incomplete bool
	}{
		{"usage_multi", 15, 5, true, false}, {"usage_duplicate", 10, 2, true, false}, {"usage_zero", 0, 0, false, false}, {"usage_gap", 10, 2, true, true},
	} {
		t.Run(tc.scenario, func(t *testing.T) {
			p := coddyProfile(coddyFixture(t, tc.scenario))
			published := []Event{}
			ctx := WithObserver(context.Background(), func(e Event) { published = append(published, e) })
			result, err := buildTest(t, p).Run(ctx, taskFor(t))
			if err != nil || result.Candidate != "candidate [REDACTED]" || result.ContentPolicy != CoddyContentPolicy {
				t.Fatalf("%+v %v", result, err)
			}
			if (result.Usage != nil) != tc.known {
				t.Fatal("unknown usage presented as measured")
			}
			if tc.known && (result.Usage.InputTokens != tc.input || result.Usage.OutputTokens != tc.output || result.Usage.Incomplete != tc.incomplete) {
				t.Fatalf("usage %+v", result.Usage)
			}
			body, _ := json.Marshal(struct {
				Result Result
				Events []Event
			}{result, published})
			if strings.Contains(string(body), "SYNTHETIC_PRIVATE_REASONING") {
				t.Fatal("reasoning was persisted")
			}
		})
	}
}
