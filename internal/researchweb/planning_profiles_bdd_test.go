//go:build linux

package researchweb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestProfileFactoryBDD_VersionsAreImmutableAndSurviveRestart(t *testing.T) {
	o := optionsFor(t, "http://127.0.0.1:1/v1")
	s, err := NewService(o)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := o.Profiles["reader"]
	r := ProfileRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Name: "researcher", Label: "Researcher", Template: "reader", Model: p.Model.Model, Skills: []execution.Skill{{ID: "induction", Version: "1", Instructions: "Check base and step"}}, Limits: p.Limits, Confirm: true}
	bad := r
	bad.Confirm = false
	if s.CreateProfile(bad) == nil {
		t.Fatal("unconfirmed profile allowed")
	}
	if err = s.CreateProfile(r); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateProfile(r); err != nil {
		t.Fatal("idempotency", err)
	}
	first := s.profile("researcher@1")
	r.ExpectedRevision = stateOf(t, s).Revision
	r.RequestID = identifier("cmd")
	r.Skills[0].Version = "2"
	if err = s.CreateProfile(r); err != nil {
		t.Fatal(err)
	}
	if hash(first) != hash(s.profile("researcher@1")) || s.profile("researcher@2").Skills[0].Version != "2" {
		t.Fatal("old profile changed")
	}
	bad = r
	bad.ExpectedRevision = stateOf(t, s).Revision
	bad.RequestID = identifier("cmd")
	bad.Template = "unregistered"
	if s.CreateProfile(bad) == nil {
		t.Fatal("untrusted template allowed")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = NewService(o)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if hash(first) != hash(s.profile("researcher@1")) {
		t.Fatal("profile lost at restart")
	}
}

func TestPlanningBDD_ProposalNeedsConfirmationAndCoverage(t *testing.T) {
	g := leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}
	report := DecompositionReport{Summary: "Prove a lemma, then coverage", Claims: []ProposedClaim{{Title: "Lemma", Statement: "True", Assumptions: "No extra assumptions", FormalGoal: &g}}, Coverage: ProposedClaim{Title: "Coverage", Statement: "True implies True", Assumptions: "Lemma", FormalGoal: &g}, Strategies: []ProposedStrategy{{Target: 0, Method: "induction", Priority: 20, Rationale: "Base first"}, {Target: 1, Method: "decomposition", Priority: 90, Rationale: "Coverage after lemma"}, {Target: -1, Method: "equivalence", Priority: 100, Rationale: "Final transfer"}}}
	body, _ := json.Marshal(report)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { completeModel(w, string(body)) }))
	defer model.Close()
	o := teamOptions(t, model.URL+"/v1")
	o.Checker = checkerFunc(func(context.Context, leancheck.Goal, string, string) (leancheck.Report, error) {
		return leancheck.Report{}, nil
	})
	s := serviceFor(t, o)
	v := studyFor(t, s)
	target := v.Studies[0].Goal
	if err := s.SetFormalGoal(target, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: g}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	if err := s.RequestPlanning(PlanningRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Target: target, Profile: "proof", Workspace: "source", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	a := waitAttempt(t, s, stateOf(t, s).Attempts[0].ID, func(a Attempt) bool { return !active(a.Status) })
	if a.Status != "candidate" || a.ProfileConfiguration == nil {
		t.Fatal(a)
	}
	v = stateOf(t, s)
	if err := s.ImportDecomposition(ProposalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Attempt: a.ID}); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	v = stateOf(t, s)
	proposal := v.Proposals[0]
	request := ProposalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Confirm: true, Team: &TeamRequest{Profiles: map[string]string{"proof": "proof", "counterexample": "counterexample", "formalize": "formalize", "review": "review"}, Workspace: "source", MaxAttempts: 6, RequireLean: true}}
	bad := request
	bad.Confirm = false
	if s.ApplyDecomposition(proposal.ID, bad) == nil {
		s.mu.Unlock()
		t.Fatal("unconfirmed plan applied")
	}
	if err := s.ApplyDecomposition(proposal.ID, request); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	v = stateOf(t, s)
	if len(v.Branches) != 3 || len(v.Proposals[0].Children) != 2 || len(v.entity(target).Dependencies) != 1 {
		s.mu.Unlock()
		t.Fatal("missing coverage or branches")
	}
	s.mu.Unlock()
	s.advanceBranches()
	s.mu.Lock()
	defer s.mu.Unlock()
	v = stateOf(t, s)
	if len(v.Teams) != 1 || v.Teams[0].Goal != v.Proposals[0].Children[0] {
		t.Fatal("dependent high-priority branch ran before its lemma", v.Teams)
	}
}

func TestPlanningBDD_InvalidStrategiesRejected(t *testing.T) {
	for _, strategy := range []ProposedStrategy{{Target: 2, Method: "induction", Priority: 1, Rationale: "x"}, {Target: 0, Method: "unknown", Priority: 1, Rationale: "x"}, {Target: 0, Method: "induction", Priority: 101, Rationale: "x"}} {
		if validateStrategies(DecompositionReport{Claims: []ProposedClaim{{}}, Strategies: []ProposedStrategy{strategy}}) == nil {
			t.Fatal("invalid strategy allowed", strategy)
		}
	}
}
