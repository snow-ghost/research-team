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
	"sync/atomic"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

func toolCompletion(w http.ResponseWriter, source string, n int, extra bool) {
	args := map[string]string{"source": source}
	if extra {
		args["goal"] = "DifferentGoal"
	}
	b, _ := json.Marshal(args)
	json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "check-" + string(rune('0'+n)), "type": "function", "function": map[string]string{"name": "check_lean", "arguments": string(b)}}}}, "finish_reason": "tool_calls"}}, "usage": map[string]int{"prompt_tokens": 7, "completion_tokens": 4}})
}

func TestLeanToolBDD_FailedIntermediateCorrectedBeforeFinalCandidate(t *testing.T) {
	var calls atomic.Int32
	bad := "import Goal\ntheorem Candidate : Statement := by rfl\n"
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct{ Role, Content string }
		}
		json.NewDecoder(r.Body).Decode(&body)
		n := int(calls.Add(1))
		if n == 1 {
			toolCompletion(w, bad, n, false)
			return
		}
		last := body.Messages[len(body.Messages)-1]
		if last.Role != "tool" || !strings.Contains(last.Content, `"accepted":false`) {
			t.Error("checker result missing", last)
		}
		if n == 2 {
			if !strings.Contains(last.Content, `"status":"failed"`) {
				t.Error("expected failure feedback", last)
			}
			toolCompletion(w, submittedProof, n, false)
			return
		}
		if !strings.Contains(last.Content, `"status":"verified"`) {
			t.Error("expected success feedback", last)
		}
		completeModel(w, "```lean\n"+submittedProof+"```")
	}))
	defer model.Close()
	o := optionsFor(t, model.URL+"/v1")
	p := o.Profiles["reader"]
	p.Model.Tools = []string{"check_lean"}
	p.Limits.MaxSteps = 3
	p.Limits.MaxToolCalls = 2
	p.Limits.TimeoutSeconds = 120
	o.Profiles[p.ID] = p
	o.Checker = checkerFunc(func(_ context.Context, g leancheck.Goal, s, _ string) (leancheck.Report, error) {
		status := "verified"
		if s == bad {
			status = "failed"
		}
		return leancheck.Report{Status: status, GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(s)}, nil
	})
	if file := os.Getenv("RESEARCH_TEST_LEAN_CONFIG"); file != "" {
		var c leancheck.Config
		if err := ReadJSON(file, &c); err != nil {
			t.Fatal(err)
		}
		o.Lean = &c
		o.Checker = leancheck.DockerChecker{Config: c}
	}
	s := serviceFor(t, o)
	v := studyFor(t, s)
	goal := v.Studies[0].Goal
	if err := s.SetFormalGoal(goal, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: leancheck.Goal{Source: "def Statement : Prop := True\n", Declaration: "Statement", Candidate: "Candidate"}}); err != nil {
		t.Fatal(err)
	}
	v = act(t, s, Action{Type: "TASK", Target: goal, Kind: "formalize", Title: "Checked formalization", Text: "Use the bounded checker"})
	if err := s.Start(RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[0].ID, Profile: p.ID, Workspace: "source", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	a := waitAttempt(t, s, stateOf(t, s).Attempts[0].ID, func(a Attempt) bool { return !active(a.Status) })
	if a.Status != "candidate" || calls.Load() != 3 {
		t.Fatal(a, calls.Load())
	}
	checks, err := os.ReadDir(filepath.Join(o.Config.DataDir, "attempts", a.ID, "toolchecks"))
	if err != nil || len(checks) != 2 {
		t.Fatal("missing intermediate records", err)
	}
	v = stateOf(t, s)
	if len(v.Verifications) != 0 || v.entity(goal).Status != "open" || v.entity(goal).Proof != "" {
		t.Fatal("tool silently accepted proof")
	}
}

func TestLeanToolBDD_ModelCannotReplaceGoalOrEnvironment(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { toolCompletion(w, submittedProof, 1, true) }))
	defer model.Close()
	o := optionsFor(t, model.URL+"/v1")
	p := o.Profiles["reader"]
	p.Model.Tools = []string{"check_lean"}
	p.Limits.MaxToolCalls = 1
	o.Profiles[p.ID] = p
	var checks atomic.Int32
	o.Checker = checkerFunc(func(context.Context, leancheck.Goal, string, string) (leancheck.Report, error) {
		checks.Add(1)
		return leancheck.Report{}, nil
	})
	s := serviceFor(t, o)
	v := studyFor(t, s)
	goal := v.Studies[0].Goal
	if err := s.SetFormalGoal(goal, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}}); err != nil {
		t.Fatal(err)
	}
	v = act(t, s, Action{Type: "TASK", Target: goal, Kind: "formalize", Title: "Hostile argument", Text: "Fixed goal"})
	if err := s.Start(RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[0].ID, Profile: p.ID, Workspace: "source", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	a := waitAttempt(t, s, stateOf(t, s).Attempts[0].ID, func(a Attempt) bool { return !active(a.Status) })
	if a.Status == "candidate" || checks.Load() != 0 {
		t.Fatal("goal override reached checker")
	}
}
