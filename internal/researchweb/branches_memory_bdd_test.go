//go:build linux

package researchweb

import (
	"context"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestBranchBDD_PriorityGoalInvalidationAndExplicitPermission(t *testing.T) {
	o := teamOptions(t, "http://127.0.0.1:1/v1")
	o.Checker = checkerFunc(func(context.Context, leancheck.Goal, string, string) (leancheck.Report, error) {
		return leancheck.Report{}, nil
	})
	s := serviceFor(t, o)
	v := studyFor(t, s)
	goal := v.Studies[0].Goal
	if err := s.SetFormalGoal(goal, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}}); err != nil {
		t.Fatal(err)
	}
	// Hold the scheduler so priority is evaluated after both commands commit.
	s.mu.Lock()
	for i, method := range []string{"induction", "representation"} {
		v = stateOf(t, s)
		r := BranchRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Method: method, Rationale: "Compare methods", Priority: 10 + i*50, Confirm: true, Team: TeamRequest{Study: v.Studies[0].ID, Target: goal, Profiles: map[string]string{"proof": "proof", "counterexample": "counterexample", "formalize": "formalize", "review": "review"}, Workspace: "source", MaxAttempts: 6, RequireLean: true}}
		bad := r
		bad.Confirm = false
		if err := s.CreateBranch(bad); err == nil {
			s.mu.Unlock()
			t.Fatal("unconfirmed branch created")
		}
		if err := s.CreateBranch(r); err != nil {
			s.mu.Unlock()
			t.Fatal(err)
		}
		if err := s.CreateBranch(r); err != nil {
			s.mu.Unlock()
			t.Fatal("idempotency", err)
		}
	}
	s.mu.Unlock()
	s.advanceBranches()
	s.mu.Lock()
	v = stateOf(t, s)
	if len(v.Branches) != 2 || v.Branches[0].Team != "" || v.Branches[1].Team == "" || len(v.Teams) != 1 {
		s.mu.Unlock()
		t.Fatal("priority violated", v.Branches)
	}
	g := *v.entity(goal).FormalGoal
	g.Source += "\n-- new goal"
	if err := s.SetFormalGoal(goal, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: g}); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
	s.advanceBranches()
	v = stateOf(t, s)
	for _, b := range v.Branches {
		if b.Status != "invalidated" {
			t.Fatal("stale branch survived", b)
		}
	}
	if v.Teams[0].Status != "cancelled" {
		t.Fatal("invalidated team still running")
	}
}

func TestMemoryBDD_NotesAreAdvisoryScopedAndVersioned(t *testing.T) {
	s, goal := submittedService(t, "sqlite", "verified")
	v := stateOf(t, s)
	r := MemoryRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Target: goal, Kind: "note", Content: "Try induction", Conditions: "n is natural"}
	if err := s.AddMemory(r); err != nil {
		t.Fatal(err)
	}
	if err := s.AddMemory(r); err != nil {
		t.Fatal("idempotency", err)
	}
	v = stateOf(t, s)
	study := v.Studies[0].ID
	if hits := memoryForTarget(v.Data, goal); len(hits) != 1 || hits[0].Trust != "unverified_note" {
		t.Fatal(hits)
	}
	other := studyFor(t, s)
	hits, err := s.SearchMemory("induction", other.Studies[len(other.Studies)-1].ID, true)
	if err != nil || len(hits) != 0 {
		t.Fatal("private notes leaked across studies", hits, err)
	}
	v = stateOf(t, s)
	g := *v.entity(goal).FormalGoal
	g.Source += "\n-- new version"
	if err := s.SetFormalGoal(goal, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: g}); err != nil {
		t.Fatal(err)
	}
	hits, err = s.SearchMemory("induction", study, false)
	if err != nil || len(hits) != 1 || hits[0].Trust != "stale" {
		t.Fatal(hits, err)
	}
	if hits := memoryForTarget(stateOf(t, s).Data, goal); len(hits) != 0 {
		t.Fatal("stale memory sent to executor")
	}
}

func TestBudgetBDD_RequestReservationCannotBeResetByAnotherBranch(t *testing.T) {
	s := serviceFor(t, optionsFor(t, "http://127.0.0.1:1/v1"))
	v := studyFor(t, s)
	study, target := v.Studies[0].ID, v.Studies[0].Goal
	p := s.Options.Profiles["reader"]
	p.Limits.MaxSteps = 3
	if err := s.SetStudyBudget(study, BudgetRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Budget: StudyBudget{MaxAttempts: 10, MaxModelRequests: 5}, Confirm: true, Note: "Shared request cap"}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	d := v.Data
	if _, err := s.reserveStudyBudget(&d, target, p); err != nil {
		t.Fatal(err)
	}
	d.Attempts = append(d.Attempts, Attempt{ID: identifier("run"), Target: target, Status: "running", ReservedModelRequests: 3})
	if _, err := s.reserveStudyBudget(&d, target, p); err == nil {
		t.Fatal("active reservation ignored")
	}
	d.Attempts[0].Status = "interrupted"
	d.Attempts[0].RemoteOutcome = "unknown"
	if ob := s.observeBudget(&d, study); ob.ChargedModelRequests != 3 || ob.KnownModelRequests != 0 {
		t.Fatal("unknown outcome lost reserve", ob)
	}
	if _, err := s.reserveStudyBudget(&d, target, p); err == nil {
		t.Fatal("unknown request replay allowed")
	}
	d.Attempts[0].ReservedModelRequests = 0
	if _, err := s.reserveStudyBudget(&d, target, p); err == nil {
		t.Fatal("historical unknown count treated as zero")
	}
}
