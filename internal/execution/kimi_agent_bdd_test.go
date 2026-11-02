//go:build linux

package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func kimiProfile(executable, configHome string) Profile {
	return Profile{ID: "kimi-reader", Kind: "external",
		Skills: []Skill{{ID: "audit", Version: "1", Instructions: "Check all assumptions."}},
		Limits: Limits{TimeoutSeconds: 10, MaxOutputBytes: 1 << 20},
		External: &ExternalConfig{Provider: "kimi-agent", Executable: executable, ExpectedVersion: "2.4.0",
			Model: "kimi-code/k3.1-pro", ConfigHome: configHome, SearchPath: "/usr/bin:/bin",
			ExecutionBoundary: "operator_managed"}}
}

func kimiFixture(t *testing.T, scenario string) string {
	t.Helper()
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return fixture(t, fmt.Sprintf("exec env GORACE=atexit_sleep_ms=0 %q -test.run=^TestKimiACPHelper$ -- %s \"$@\"\n", testBinary, scenario))
}

func TestKimiACPHelper(t *testing.T) {
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
		if scenario == "wrong_version" {
			fmt.Println("wrong-version")
		} else if scenario == "new_version" {
			fmt.Println("2.5.99")
		} else {
			fmt.Println("2.4.0")
		}
		os.Exit(0)
	}
	if !reflect.DeepEqual(args, []string{"acp"}) || !filepath.IsAbs(os.Getenv("KIMI_CODE_HOME")) ||
		os.Getenv("HOME") == os.Getenv("KIMI_CODE_HOME") {
		exit()
	}
	decoder, encoder := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	send := func(value any) {
		if encoder.Encode(value) != nil {
			os.Exit(22)
		}
	}
	reply := func(id any, value any) { send(map[string]any{"jsonrpc": "2.0", "id": id, "result": value}) }
	state := map[string]string{"mode": "default", "model": "kimi-code/k3.1-pro"}
	options := func() []any {
		out := []any{}
		for _, id := range []string{"mode", "model"} {
			out = append(out, map[string]any{"id": id, "name": id, "type": "select",
				"currentValue": state[id], "options": []any{map[string]any{"value": state[id], "name": state[id]}}})
		}
		return out
	}
	notify := func(session, kind, text string) {
		send(map[string]any{"jsonrpc": "2.0", "method": "session/update",
			"params": map[string]any{"sessionId": session, "update": map[string]any{
				"sessionUpdate": kind, "content": map[string]any{"type": "text", "text": text}}}})
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
			name := "Kimi Code CLI"
			if scenario == "wrong_agent" {
				name = "coddy-agent"
			}
			agentVersion := "2.4.0"
			if scenario == "new_version" {
				agentVersion = "2.5.99"
			}
			reply(frame.ID, map[string]any{"protocolVersion": 1,
				"agentInfo": map[string]string{"name": name, "version": agentVersion}})
		case "session/new":
			var p struct {
				Cwd        string
				McpServers []any
			}
			_ = json.Unmarshal(frame.Params, &p)
			if !filepath.IsAbs(p.Cwd) || len(p.McpServers) != 0 {
				exit()
			}
			reply(frame.ID, map[string]any{"sessionId": "kimi-session-one", "configOptions": options()})
		case "session/set_config_option":
			var p struct{ ConfigID, Value string }
			_ = json.Unmarshal(frame.Params, &p)
			state[p.ConfigID] = p.Value
			if scenario == "wrong_model" && p.ConfigID == "model" {
				state["model"] = "wrong"
			}
			reply(frame.ID, map[string]any{"configOptions": options()})
		case "session/close":
			reply(frame.ID, map[string]any{})
		case "session/prompt":
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
					"params": map[string]any{"sessionId": "kimi-session-one",
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
					"params": map[string]any{"sessionId": "kimi-session-one",
						"update": map[string]string{"sessionUpdate": "current_mode_update", "currentModeId": "default"}}})
			}
			if scenario == "config_changed" {
				state["model"] = "unapproved"
				send(map[string]any{"jsonrpc": "2.0", "method": "session/update",
					"params": map[string]any{"sessionId": "kimi-session-one",
						"update": map[string]any{"sessionUpdate": "config_option_update", "configOptions": options()}}})
			}
			notify("kimi-session-one", "agent_thought_chunk", "SYNTHETIC_PRIVATE_REASONING")
			if scenario != "empty" {
				notify("kimi-session-one", "agent_message_chunk", "Candidate from Kimi")
			}
			reply(frame.ID, map[string]any{"stopReason": "end_turn",
				"usage": map[string]int{"inputTokens": 11, "outputTokens": 7, "totalTokens": 18}})
		}
	}
}

func TestKimiAgentBDD_CompletedTurnIsAnUnverifiedCandidate(t *testing.T) {
	t.Setenv("UNRELATED_SECRET", "must-not-leak")
	for _, scenario := range []string{"success", "permission"} {
		t.Run(scenario, func(t *testing.T) {
			p := kimiProfile(kimiFixture(t, scenario), t.TempDir())
			r, err := buildTest(t, p).Run(context.Background(), taskFor(t))
			if err != nil || r.Status != "candidate" || r.Candidate != "Candidate from Kimi" ||
				r.ContentPolicy != KimiContentPolicy || r.SessionID != "kimi-session-one" ||
				r.Usage == nil || r.Usage.InputTokens != 11 || r.Usage.OutputTokens != 7 || r.Usage.Source != "acp_response" {
				t.Fatalf("%+v %v", r, err)
			}
			if strings.Contains(r.Candidate+r.Partial, "SYNTHETIC_PRIVATE_REASONING") {
				t.Fatal("reasoning leaked into result")
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

func TestKimiAgentBDD_ACPCompatibilityAcceptsNewVersionAndRecordsMetadata(t *testing.T) {
	p := kimiProfile(kimiFixture(t, "new_version"), t.TempDir())
	p.External.ExpectedVersion = ""
	p.External.Compatibility = ACPCompatibility
	r, err := buildTest(t, p).Run(context.Background(), taskFor(t))
	if err != nil || r.Status != "candidate" || r.AgentName != "Kimi Code CLI" || r.AgentVersion != "2.5.99" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestKimiAgentBDD_ACPCompatibilityRejectsMalformedVersion(t *testing.T) {
	p := kimiProfile(kimiFixture(t, "wrong_version"), t.TempDir())
	p.External.ExpectedVersion = ""
	p.External.Compatibility = ACPCompatibility
	r, err := buildTest(t, p).Run(context.Background(), taskFor(t))
	if err == nil || r.Status == "candidate" || r.AgentVersion != "" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestKimiAgentBDD_IncompleteOrUntrustedTurnsAreRejected(t *testing.T) {
	for _, scenario := range []string{"wrong_version", "wrong_agent", "wrong_model", "mode_changed", "config_changed", "empty", "missing_end"} {
		t.Run(scenario, func(t *testing.T) {
			p := kimiProfile(kimiFixture(t, scenario), t.TempDir())
			r, err := buildTest(t, p).Run(context.Background(), taskFor(t))
			if err == nil || r.Candidate != "" || r.Status == "candidate" {
				t.Fatalf("%+v %v", r, err)
			}
		})
	}
}

func TestKimiAgentBDD_ProfileRequiresManagedHomeAndNoExtraCapabilities(t *testing.T) {
	p := kimiProfile("/opt/homebrew/bin/kimi", t.TempDir())
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Profile){
		func(p *Profile) { p.External.ConfigHome = "relative" },
		func(p *Profile) { p.External.SecretEnv = map[string]string{"OPENAI_API_KEY": "TOKEN"} },
		func(p *Profile) { p.External.BaseURL = "https://provider.example/v1" },
		func(p *Profile) { p.Limits.MaxSteps = 1 },
		func(p *Profile) { p.Limits.MaxOutputTokens = 100 },
		func(p *Profile) { p.External.ExpectedVersion = "2.3.0" },
		func(p *Profile) { p.External.Compatibility = "experimental" },
	} {
		candidate := p
		external := *p.External
		candidate.External = &external
		change(&candidate)
		if err := candidate.Validate(); err == nil {
			t.Fatal("invalid Kimi profile accepted")
		}
	}
}
