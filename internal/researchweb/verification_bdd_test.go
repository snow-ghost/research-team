//go:build linux

package researchweb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

func waitVerification(t *testing.T, s *Service, id string) View {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		v := stateOf(t, s)
		if check := v.verification(id); check != nil && check.Status != "queued" && check.Status != "running" {
			return v
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("verification did not finish")
	return View{}
}
func TestVerificationBDD_AcceptanceRequiresExactSourceAndCurrentGoal(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		completeModel(w, "```lean\nimport Goal\ntheorem Candidate : Statement := by trivial\n```")
	}))
	defer model.Close()
	o := optionsFor(t, model.URL+"/v1")
	o.Checker = checkerFunc(func(_ context.Context, g leancheck.Goal, source, _ string) (leancheck.Report, error) {
		return leancheck.Report{Status: "verified", GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(source)}, nil
	})
	s := serviceFor(t, o)
	v := studyFor(t, s)
	goal := v.Studies[0].Goal
	if err := s.SetFormalGoal(goal, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}}); err != nil {
		t.Fatal(err)
	}
	v = act(t, s, Action{Type: "TASK", Target: goal, Kind: "proof", Title: "Proof", Text: "Prove statement"})
	if err := s.Start(RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[0].ID, Profile: "reader", Workspace: "source", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	a := waitAttempt(t, s, stateOf(t, s).Attempts[0].ID, func(a Attempt) bool { return a.Status == "candidate" })
	v = act(t, s, Action{Type: "ATTACH_PROOF", Target: goal, Attempt: a.ID})
	accept := Action{Type: "REVIEW", Target: goal, Decision: "accept", Text: "Проверена точная постановка.", ExpectedRevision: v.Revision, RequestID: identifier("cmd")}
	if err := s.Act(accept); err == nil {
		t.Fatal("accepted without Lean")
	}
	verify := VerifyRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Attempt: a.ID, Source: "different proof"}
	if err := s.StartVerification(verify); err == nil {
		t.Fatal("substituted source verified")
	}
	verify.Source = ""
	if err := s.StartVerification(verify); err != nil {
		t.Fatal(err)
	}
	v = waitVerification(t, s, stateOf(t, s).Verifications[0].ID)
	if !hasVerifiedProof(&v.Data, v.entity(goal)) {
		t.Fatal("attached proof not verifiable")
	}
	changed := *v.entity(goal).FormalGoal
	changed.Source = "def Statement : Prop := False"
	if err := s.SetFormalGoal(goal, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: changed}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	if hasVerifiedProof(&v.Data, v.entity(goal)) || v.entity(goal).Proof != "" {
		t.Fatal("old verification survived goal replacement")
	}
}
func TestTeamBDD_RejectionExhaustsBudgetWithoutAutomaticAcceptance(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A combined report is sufficient for both structured reports in this test.
		completeModel(w, `{"outcome":"none_found","evidence":"Boundary cases","summary":"Reviewed","findings":[]}`)
	}))
	defer model.Close()
	s := serviceFor(t, teamOptions(t, model.URL+"/v1"))
	v := studyFor(t, s)
	if err := s.StartTeam(TeamRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: v.Studies[0].ID, Profiles: map[string]string{"proof": "proof", "counterexample": "counterexample", "review": "review"}, Workspace: "source", MaxAttempts: 4, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	id := stateOf(t, s).Teams[0].ID
	v = waitTeam(t, s, id, "awaiting_review")
	act(t, s, Action{Type: "REVIEW", Target: v.Studies[0].Goal, Decision: "reject", Text: "Нужно разобрать дополнительный случай."})
	v = waitTeam(t, s, id, "blocked")
	if v.Teams[0].UsedAttempts > 4 || v.Entities[0].Status == "accepted" {
		t.Fatal("team bypassed budget or operator decision")
	}
}
