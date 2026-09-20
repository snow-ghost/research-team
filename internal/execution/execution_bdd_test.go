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
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func modelProfile(base string) Profile {
	return Profile{ID: "proof-reader", Kind: "model",
		Limits: Limits{TimeoutSeconds: 5, MaxSteps: 3, MaxToolCalls: 2, MaxOutputTokens: 128, MaxOutputBytes: 8192},
		Skills: []Skill{{ID: "scope-audit", Version: "1", Instructions: "Check assumptions and list missing obligations."}},
		Model:  &ModelConfig{Protocol: "chat_completions", BaseURL: base, Model: "test-model", TokenEnv: "TEST_TOKEN", AllowLoopbackHTTP: true, Tools: []string{"read_file"}},
	}
}

func taskFor(t *testing.T) Task {
	t.Helper()
	return Task{ID: "T-1", AttemptID: "A-1", Snapshot: "S-4", LeaseEpoch: 7, Workspace: t.TempDir(), Objective: "Check the lemma."}
}

func buildTest(t *testing.T, p Profile) Executor {
	t.Helper()
	executor, err := Build(p, func(string) (string, bool) { return "private-test-token", true })
	if err != nil {
		t.Fatal(err)
	}
	return executor
}

func finalReply(w http.ResponseWriter, text string) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": text}, "finish_reason": "stop"}},
		"usage":   map[string]int{"prompt_tokens": 10, "completion_tokens": 5},
	})
}

func TestModelBDD_ReadEvidenceThenSubmitCandidate(t *testing.T) {
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer private-test-token" {
			t.Error("wrong endpoint or auth")
		}
		var payload struct {
			Model    string
			Messages []message
			Tools    []any
		}
		if json.NewDecoder(r.Body).Decode(&payload) != nil {
			t.Error("invalid request")
		}
		if payload.Model != "test-model" || len(payload.Tools) != 1 {
			t.Error("profile not applied")
		}
		if count.Add(1) == 1 {
			if !strings.Contains(payload.Messages[1].Content, "scope-audit") {
				t.Error("skill missing")
			}
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"lemma.txt\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`)
		} else {
			last := payload.Messages[len(payload.Messages)-1]
			if last.Role != "tool" || last.ToolCallID != "c1" || last.Content != "n >= 2" {
				t.Error("tool result not bound to call")
			}
			finalReply(w, "Candidate; assumption n >= 2. private-test-token")
		}
	}))
	defer server.Close()
	task := taskFor(t)
	if err := os.WriteFile(filepath.Join(task.Workspace, "lemma.txt"), []byte("n >= 2"), 0600); err != nil {
		t.Fatal(err)
	}
	executor := buildTest(t, modelProfile(server.URL+"/v1"))
	result, err := executor.Run(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	if count.Load() != 2 || result.Status != "candidate" || result.LeaseEpoch != 7 || result.Snapshot != "S-4" ||
		len(result.PacketSHA256) != 64 || strings.Contains(result.Candidate, "private-test-token") ||
		result.Usage == nil || result.Usage.InputTokens != 20 || result.Usage.OutputTokens != 7 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestModelBDD_RejectUntrustedCallsAndIncompleteReplies(t *testing.T) {
	tests := []struct {
		name, body string
		maxSteps   int
		want       error
	}{
		{"unknown_tool", `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"1","type":"function","function":{"name":"shell","arguments":"{\"path\":\"x\"}"}}]},"finish_reason":"tool_calls"}]}`, 3, ErrUnsupported},
		{"invalid_arguments", `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"x\",\"extra\":true}"}}]},"finish_reason":"tool_calls"}]}`, 3, ErrProtocol},
		{"truncated_reply", `{"choices":[{"message":{"role":"assistant","content":"partial proof"},"finish_reason":"length"}]}`, 3, ErrProtocol},
		{"step_limit", `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"x\"}"}}]},"finish_reason":"tool_calls"}]}`, 1, ErrLimit},
		{"malformed_json", "{", 3, ErrProtocol},
		{"oversized_reply", strings.Repeat("x", 9000), 3, ErrLimit},
		{"duplicate_call", `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"x\"}"}},{"id":"1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"x\"}"}}]},"finish_reason":"tool_calls"}]}`, 3, ErrUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tt.body) }))
			defer server.Close()
			p := modelProfile(server.URL)
			p.Limits.MaxSteps = tt.maxSteps
			result, err := buildTest(t, p).Run(context.Background(), taskFor(t))
			if !errors.Is(err, tt.want) || result.Candidate != "" {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
}

func TestModelBDD_HTTPFailureDoesNotDiscloseToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, r.Header.Get("Authorization"))
	}))
	defer server.Close()
	result, err := buildTest(t, modelProfile(server.URL)).Run(context.Background(), taskFor(t))
	if err == nil || strings.Contains(err.Error(), "private-test-token") || result.Candidate != "" {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestModelBDD_RedirectIsNotFollowed(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer server.Close()
	_, err := buildTest(t, modelProfile(server.URL)).Run(context.Background(), taskFor(t))
	if err == nil || targetCalls.Load() != 0 {
		t.Fatal("redirect followed")
	}
}

func TestModelBDD_CancellationLeavesRemoteOutcomeUnknown(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { <-started; cancel() }()
	result, err := buildTest(t, modelProfile(server.URL)).Run(ctx, taskFor(t))
	if !errors.Is(err, context.Canceled) || result.Status != "cancelled" || result.RemoteOutcome != "unknown" {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestModelBDD_UnknownUsageIsNotZero(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"candidate"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	result, err := buildTest(t, modelProfile(server.URL)).Run(context.Background(), taskFor(t))
	if err != nil || result.Usage != nil {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestProfileBDD_UnsupportedCapabilitiesFailBeforeRequest(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Profile)
	}{
		{"protocol", func(p *Profile) { p.Model.Protocol = "responses" }},
		{"http_remote", func(p *Profile) { p.Model.BaseURL = "http://example.com/v1" }},
		{"url_credentials", func(p *Profile) { p.Model.BaseURL = "https://key:secret@example.com/v1" }},
		{"url_query", func(p *Profile) { p.Model.BaseURL = "https://example.com/v1?token=secret" }},
		{"tool", func(p *Profile) { p.Model.Tools = []string{"shell"} }},
		{"token_limit_field", func(p *Profile) { p.Model.TokenLimitField = "guess" }},
		{"duplicate_skill", func(p *Profile) { p.Skills = append(p.Skills, p.Skills[0]) }},
		{"mixed_executor", func(p *Profile) { p.External = &ExternalConfig{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := modelProfile("https://example.com/v1")
			tt.mutate(&p)
			if p.Validate() == nil {
				t.Fatal("invalid profile accepted")
			}
		})
	}
}

func TestProfileBDD_FactoryOwnsImmutableCopy(t *testing.T) {
	p := modelProfile("https://example.com/v1")
	executor := buildTest(t, p).(*modelExecutor)
	p.Model.Model = "changed"
	p.Skills[0].Instructions = "changed"
	p.Model.Tools[0] = "shell"
	if executor.profile.Model.Model != "test-model" || executor.profile.Skills[0].Instructions == "changed" || executor.allows("shell") {
		t.Fatal("caller changed running profile")
	}
}

func TestWorkspaceBDD_RejectsTraversalAndEscapingSymlink(t *testing.T) {
	task := taskFor(t)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(task.Workspace, "link")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(task.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, path := range []string{outside, "../secret.txt", "link", "."} {
		if _, err := readWorkspaceFile(root, path); err == nil {
			t.Errorf("read escaped or non-file %s", path)
		}
	}
}

func TestTaskBDD_IdentityAndPacketArePinned(t *testing.T) {
	p := modelProfile("https://example.com/v1")
	task := taskFor(t)
	first, _, err := prepare(p, task)
	if err != nil {
		t.Fatal(err)
	}
	task.Snapshot = "S-5"
	second, _, _ := prepare(p, task)
	if first.PacketSHA256 == second.PacketSHA256 {
		t.Fatal("snapshot not hashed")
	}
	task.LeaseEpoch = 0
	if _, _, err := prepare(p, task); err == nil {
		t.Fatal("missing fencing identity")
	}
}

func TestModelBDD_ToolBudget(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := calls.Add(1)
		fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"c%d","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"missing\"}"}}]},"finish_reason":"tool_calls"}]}`, id)
	}))
	defer server.Close()
	p := modelProfile(server.URL)
	p.Limits.MaxToolCalls = 1
	result, err := buildTest(t, p).Run(context.Background(), taskFor(t))
	if !errors.Is(err, ErrLimit) || calls.Load() != 2 || result.Status != "limit_reached" {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestModelBDD_Deadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	result, err := buildTest(t, modelProfile(server.URL)).Run(ctx, taskFor(t))
	if !errors.Is(err, context.DeadlineExceeded) || result.Status != "timed_out" {
		t.Fatalf("%+v %v", result, err)
	}
}
