//go:build linux

package researchweb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/graphsearch"
)

func computationGraph() graphsearch.Input {
	return graphsearch.Input{Degree: 3, Generators: []graphsearch.Generator{{Name: "swap", Permutation: []int{1, 0, 2}}, {Name: "rotate", Permutation: []int{1, 2, 0}}}, Start: []int{0, 1, 2}, Bidirectional: true, MaxStates: 16, MaxDepth: 8}
}

func computationToolCall(w http.ResponseWriter, name string, args any, n int) {
	body, _ := json.Marshal(args)
	json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "compute-" + string(rune('0'+n)), "type": "function", "function": map[string]any{"name": name, "arguments": string(body)}}}}, "finish_reason": "tool_calls"}}, "usage": map[string]int{"prompt_tokens": 7, "completion_tokens": 4}})
}

func TestComputationToolBDD_ArtifactsAreBoundedAndIntegrityChecked(t *testing.T) {
	s := serviceFor(t, optionsFor(t, "http://127.0.0.1:1/v1"))
	a := Attempt{ID: "run-computation-test"}
	calls := 0
	var mu sync.Mutex
	longest, err := s.computationTool(a, "search_longest_states", &calls, &mu, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(longestStatesArguments{Graph: computationGraph()})
	if err := longest.Validate(string(raw)); err != nil {
		t.Fatal(err)
	}
	if err := longest.Validate(strings.TrimSuffix(string(raw), "}") + `,"extra":true}`); err == nil {
		t.Fatal("unknown computation argument accepted")
	}
	output, err := longest.Run(context.Background(), string(raw))
	if err != nil {
		t.Fatal(err)
	}
	var summary computationSummary
	if json.Unmarshal([]byte(output), &summary) != nil || summary.Eccentricity != 2 || summary.Explored != 6 || summary.Accepted || summary.MathematicalValidation {
		t.Fatalf("unexpected computation summary: %s", output)
	}
	verify, err := s.computationTool(a, "verify_word", &calls, &mu, nil)
	if err != nil {
		t.Fatal(err)
	}
	wordArgs, _ := json.Marshal(verifyWordArguments{Graph: computationGraph(), Word: []string{"swap", "rotate^-1"}, Target: []int{2, 1, 0}, VerifyShortest: true})
	output, err = verify.Run(context.Background(), string(wordArgs))
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal([]byte(output), &summary) != nil || summary.Verification == nil || !summary.Verification.Matches || summary.Verification.Shortest == nil || !*summary.Verification.Shortest {
		t.Fatalf("word certificate missing: %s", output)
	}
	artifacts := s.computationSummaries(a.ID)
	if len(artifacts) != 2 || calls != 2 {
		t.Fatalf("missing artifacts: %+v", artifacts)
	}
	for _, artifact := range artifacts {
		if len(artifact.Targets) > 1 || len(artifact.Farthest) > 1 || artifact.ReportSHA256 == "" {
			t.Fatal("unbounded artifact context", artifact)
		}
	}
	root := filepath.Join(s.Options.Config.DataDir, "attempts", a.ID, "computations")
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 2 {
		t.Fatal("missing computation records", err)
	}
	if err := atomicFile(filepath.Join(root, entries[0].Name(), "report.json"), []byte(`{"changed":true}`)); err != nil {
		t.Fatal(err)
	}
	if len(s.computationSummaries(a.ID)) != 1 {
		t.Fatal("tampered report entered continuation context")
	}
}

func TestComputationToolBDD_ModelVerifiesWordAndContinuationReceivesSummary(t *testing.T) {
	var calls atomic.Int32
	target := []int{2, 1, 0}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct{ Role, Content string }
			Tools    []struct{ Function struct{ Name string } }
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("invalid model request")
		}
		n := int(calls.Add(1))
		switch n {
		case 1:
			if len(body.Tools) != 2 {
				t.Error("computation tools not offered", body.Tools)
			}
			computationToolCall(w, "run_bfs", runBFSArguments{Graph: computationGraph(), Targets: [][]int{target}}, n)
		case 2:
			last := body.Messages[len(body.Messages)-1]
			if last.Role != "tool" || !strings.Contains(last.Content, `"distance":2`) || !strings.Contains(last.Content, `"accepted":false`) {
				t.Error("BFS result missing", last)
			}
			computationToolCall(w, "verify_word", verifyWordArguments{Graph: computationGraph(), Word: []string{"swap", "rotate^-1"}, Target: target, VerifyShortest: true}, n)
		case 3:
			last := body.Messages[len(body.Messages)-1]
			if last.Role != "tool" || !strings.Contains(last.Content, `"shortest":true`) {
				t.Error("shortest-word result missing", last)
			}
			completeModel(w, "BFS word independently verified; no acceptance claimed")
		default:
			t.Error("unexpected model call", n)
		}
	}))
	defer model.Close()
	o := optionsFor(t, model.URL+"/v1")
	p := o.Profiles["reader"]
	p.Model.Tools = []string{"run_bfs", "verify_word"}
	p.Limits.MaxSteps = 3
	p.Limits.MaxToolCalls = 2
	p.Limits.TimeoutSeconds = 120
	o.Profiles[p.ID] = p
	s := serviceFor(t, o)
	v := studyFor(t, s)
	goal := v.Studies[0].Goal
	v = act(t, s, Action{Type: "TASK", Target: goal, Kind: "proof", Title: "BFS certificate", Text: "Find and independently verify the shortest word"})
	if err := s.Start(RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[0].ID, Profile: p.ID, Workspace: "source", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	a := waitAttempt(t, s, stateOf(t, s).Attempts[0].ID, func(a Attempt) bool { return !active(a.Status) })
	if a.Status != "candidate" || calls.Load() != 3 {
		result, _ := s.Result(a.ID)
		t.Fatalf("attempt failed: %+v result=%+v", a, result)
	}
	entries, err := os.ReadDir(filepath.Join(o.Config.DataDir, "attempts", a.ID, "computations"))
	if err != nil || len(entries) != 2 {
		t.Fatal("missing model computation artifacts", err)
	}
	v = stateOf(t, s)
	if v.entity(goal).Status != "open" {
		t.Fatal("computation accepted the goal")
	}
	continuation, err := json.Marshal(s.continuationContext(v.Data, Attempt{ParentAttempt: a.ID}))
	if err != nil || !strings.Contains(string(continuation), `"computations"`) || !strings.Contains(string(continuation), `"certificate_sha256"`) {
		t.Fatal("continuation lost verified computation summary", string(continuation))
	}
	handler, err := NewHTTP(s, strings.Repeat("k", 64))
	if err != nil {
		t.Fatal(err)
	}
	defer handler.Close()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:4187/api/attempts/"+a.ID+"/computations", nil)
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("k", 64))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"certificate_sha256"`) || !strings.Contains(response.Body.String(), `"accepted":false`) {
		t.Fatal("computation API lost verified artifacts", response.Code, response.Body.String())
	}
}

func TestComputationToolBDD_RolesCannotUseResearchComputeForReview(t *testing.T) {
	for _, name := range []string{"run_bfs", "search_longest_states", "verify_word"} {
		p := execution.Profile{Model: &execution.ModelConfig{Tools: []string{name}}}
		if err := validateRoleTools(p, "review", false); err == nil {
			t.Fatal("review received computation tool", name)
		}
		if name != "verify_word" {
			if err := validateRoleTools(p, "formalize", false); err == nil {
				t.Fatal("formalizer received search tool", name)
			}
		} else if err := validateRoleTools(p, "formalize", false); err != nil {
			t.Fatal("formalizer cannot verify a certificate", err)
		}
	}
}

func TestComputationToolBDD_PinnedGraphCannotBeChanged(t *testing.T) {
	config := ComputationGraphConfig{Kind: "wrapped_k_cycles", Degree: 3, CycleLength: 2, MaxStates: 16, MaxDepth: 8}
	graph, err := buildComputationGraph(config)
	if err != nil || len(graph.Generators) != 3 {
		t.Fatal("pinned graph not constructed", graph, err)
	}
	serverConfig := Config{Workspaces: []Workspace{{ID: "source", Graph: &config}}}
	pinned, err := computationGraphFor(serverConfig, "source")
	if err != nil || pinned == nil {
		t.Fatal(err)
	}
	s := serviceFor(t, optionsFor(t, "http://127.0.0.1:1/v1"))
	calls := 0
	var mu sync.Mutex
	tool, err := s.computationTool(Attempt{ID: "run-pinned"}, "verify_word", &calls, &mu, pinned)
	if err != nil {
		t.Fatal(err)
	}
	valid, _ := json.Marshal(verifyWordArguments{Graph: graph, Word: []string{"left0", "left1"}, Target: []int{2, 1, 0}, VerifyShortest: true})
	if err := tool.Validate(string(valid)); err != nil {
		t.Fatal("server-pinned graph rejected", err)
	}
	invalid, _ := json.Marshal(verifyWordArguments{Graph: computationGraph(), Word: []string{"left0", "left1"}, Target: []int{2, 1, 0}, VerifyShortest: true})
	if err := tool.Validate(string(invalid)); err == nil {
		t.Fatal("model replaced the pinned graph")
	}
}
