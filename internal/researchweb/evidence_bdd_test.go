//go:build linux

package researchweb

import (
	"context"
	"encoding/json"
	"github.com/snow-ghost/research-team/internal/leancheck"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEvidenceBDD_AutomaticBindingAndInvalidation(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		text := ""
		for _, m := range body.Messages {
			text += m.Content
		}
		answer := "```lean\nimport Goal\ntheorem Candidate : Statement := by trivial\n```"
		if strings.Contains(text, "ROLE_counterexample") {
			answer = `{"outcome":"none_found","evidence":"Only the finite fixture was checked."}`
		}
		if strings.Contains(text, "ROLE_review") {
			answer = `{"summary":"Exact source reviewed","findings":[]}`
		}
		completeModel(w, answer)
	}))
	defer model.Close()
	o := teamOptions(t, model.URL+"/v1")
	o.Checker = checkerFunc(func(_ context.Context, g leancheck.Goal, source, _ string) (leancheck.Report, error) {
		return leancheck.Report{Status: "verified", GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(source)}, nil
	})
	s := serviceFor(t, o)
	v := studyFor(t, s)
	goal := v.Studies[0].Goal
	if err := s.SetFormalGoal(goal, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	if err := s.StartTeam(TeamRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: v.Studies[0].ID, Profiles: map[string]string{"proof": "proof", "counterexample": "counterexample", "formalize": "formalize", "review": "review"}, Workspace: "source", MaxAttempts: 6, RequireLean: true, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	v = waitTeam(t, s, stateOf(t, s).Teams[0].ID, "awaiting_review")
	if len(v.Results) != 1 || v.Results[0].Status != "ready" || v.Results[0].ReviewBindingMethod != "typed_binding" || v.entity(goal).ResearchResult != v.Results[0].ID {
		t.Fatal("unbound automatic result", v.Results)
	}
	review := v.attempt(v.Results[0].ReviewAttempt)
	if review.ProofBinding == nil || *review.ProofBinding != v.Results[0].Binding {
		t.Fatal("review did not receive exact binding")
	}
	obs, err := s.ObserveStudy(v.Studies[0].ID)
	if err != nil || obs.Completed != 4 || obs.Active != 0 {
		t.Fatal("incorrect observation", obs, err)
	}
	// Keep the completion event from changing the snapshot during these evidence commands.
	s.mu.Lock()
	defer s.mu.Unlock()
	bound := v.Results[0]
	v = act(t, s, Action{Type: "REVIEW", Target: goal, Decision: "accept", Text: "Checked the exact source and separate reports."})
	v = act(t, s, Action{Type: "CHALLENGE", Target: goal, Text: "Dependency-state regression trial."})
	if v.Results[0].Status != "stale" {
		t.Fatal("challenged result remained valid")
	}
	finding := v.Findings[len(v.Findings)-1].ID
	act(t, s, Action{Type: "RESOLVE_FINDING", Finding: finding})
	act(t, s, Action{Type: "CLOSE_FINDING", Finding: finding, Text: "No mathematical change; the exact checked source is unchanged."})
	v = act(t, s, Action{Type: "SUBMIT_REVIEW", Target: goal})
	if err := s.BindResult(ResultRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Target: goal, ReviewAttempt: bound.ReviewAttempt, CounterAttempt: bound.CounterAttempt}); err != nil {
		t.Fatal("unchanged exact reports cannot be rebound", err)
	}
	v = act(t, s, Action{Type: "REVIEW", Target: goal, Decision: "accept", Text: "Rechecked unchanged proof and closed findings."})
	if len(v.Attempts) != 4 || v.entity(goal).Status != "accepted" {
		t.Fatal("reopening silently launched or failed acceptance")
	}
	v = act(t, s, Action{Type: "CHALLENGE", Target: goal, Text: "Replace the pinned goal for the next regression."})
	changed := *v.entity(goal).FormalGoal
	changed.Source = "def Statement : Prop := False"
	if err := s.SetFormalGoal(goal, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: changed}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	if v.Results[0].Status != "stale" {
		t.Fatal("result survived goal replacement")
	}
}

func TestDecompositionBDD_ConfirmationVersionPinsAndCoverage(t *testing.T) {
	report := `{"summary":"Induction","claims":[{"title":"Base","statement":"P(0)","assumptions":"Nat"},{"title":"Step","statement":"P(n) implies P(n+1)","assumptions":"n : Nat"}],"coverage":{"title":"Induction coverage","statement":"Base and step imply forall n, P(n)","assumptions":"Nat induction"}}`
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { completeModel(w, report) }))
	defer model.Close()
	s := serviceFor(t, optionsFor(t, model.URL+"/v1"))
	v := studyFor(t, s)
	goal := v.Studies[0].Goal
	v = act(t, s, Action{Type: "TASK", Target: goal, Kind: "decompose", Title: "Split", Text: "Propose claims and coverage"})
	if err := s.Start(RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[0].ID, Profile: "reader", Workspace: "source", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	a := waitAttempt(t, s, stateOf(t, s).Attempts[0].ID, func(a Attempt) bool { return a.Status == "candidate" })
	v = stateOf(t, s)
	if err := s.ImportDecomposition(ProposalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Attempt: a.ID}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	p := v.Proposals[0]
	if err := s.ApplyDecomposition(p.ID, ProposalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd")}); err == nil {
		t.Fatal("unconfirmed proposal applied")
	}
	if err := s.ApplyDecomposition(p.ID, ProposalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Confirm: true}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	p = v.Proposals[0]
	if len(p.Children) != 3 || p.Status != "applied" || len(v.entity(goal).Dependencies) != 1 {
		t.Fatal("missing coverage graph")
	}
	cover := v.entity(p.Children[2])
	if cover.Kind != "obligation" || len(cover.Dependencies) != 2 || v.entity(p.Children[0]).Status != "open" {
		t.Fatal("parts automatically accepted or omitted")
	}
}
