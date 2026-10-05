//go:build linux

package researchweb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestNativeReviewBDD_DistinctProfileAndOriginalAttemptBinding(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { completeModel(w, "```lean\n"+submittedProof+"```\n") }))
	defer model.Close()
	o := optionsFor(t, model.URL+"/v1")
	other := o.Profiles["reader"]
	other.ID = "other-reader"
	o.Profiles[other.ID] = other
	o.Checker = checkerFunc(func(_ context.Context, g leancheck.Goal, source, _ string) (leancheck.Report, error) {
		return leancheck.Report{Status: "verified", GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(source)}, nil
	})
	s := serviceFor(t, o)
	v := studyFor(t, s)
	goal := v.Studies[0].Goal
	if err := s.SetFormalGoal(goal, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}}); err != nil {
		t.Fatal(err)
	}
	v = act(t, s, Action{Type: "TASK", Target: goal, Kind: "formalize", Title: "Source", Text: "Prepare source"})
	if err := s.Start(RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[0].ID, Profile: "reader", Workspace: "source", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	a := waitAttempt(t, s, stateOf(t, s).Attempts[0].ID, func(a Attempt) bool { return a.Status == "candidate" })
	v = stateOf(t, s)
	if err := s.StartVerification(VerifyRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Attempt: a.ID}); err != nil {
		t.Fatal(err)
	}
	id := stateOf(t, s).Verifications[0].ID
	waitVerification(t, s, id)
	v = act(t, s, Action{Type: "TASK", Target: goal, Kind: "review", Title: "Review", Text: "Exact file review"})
	r := RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[1].ID, Profile: "reader", Workspace: "source", ReviewVerification: id, Confirm: true}
	if err := s.Start(r); err == nil {
		t.Fatal("native source author reviewed self")
	}
	r.Profile = "other-reader"
	r.RequestID = identifier("cmd")
	if err := s.Start(r); err != nil {
		t.Fatal(err)
	}
	review := waitAttempt(t, s, stateOf(t, s).Attempts[1].ID, func(a Attempt) bool { return a.Status == "candidate" })
	if review.ReviewOf != a.ID || review.ProofBinding == nil || review.ProofBinding.Verification != id {
		t.Fatal("native standalone review lost exact author attempt", review)
	}
}
