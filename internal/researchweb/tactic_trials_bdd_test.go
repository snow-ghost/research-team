//go:build linux

package researchweb

import (
	"context"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestTacticsBDD_FivePinnedSourcesHaveOwnProvenanceAndDoNotAcceptGoal(t *testing.T) {
	o := optionsFor(t, "http://127.0.0.1:1/v1")
	o.Checker = checkerFunc(func(_ context.Context, g leancheck.Goal, source, _ string) (leancheck.Report, error) {
		return leancheck.Report{Status: "verified", AuditSHA256: leancheck.AuditDigest(), GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(source)}, nil
	})
	s := serviceFor(t, o)
	v := studyFor(t, s)
	id := v.Studies[0].Goal
	if err := s.SetFormalGoal(id, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	r := TacticRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), MaxTrials: 5, Confirm: true}
	if err := s.QueueTactics(id, r); err != nil {
		t.Fatal(err)
	}
	if err := s.QueueTactics(id, r); err != nil {
		t.Fatal("repeated command queued another portfolio", err)
	}
	v = stateOf(t, s)
	if len(v.Verifications) != 5 || len(v.Attempts) != 0 || v.entity(id).Status == "accepted" {
		t.Fatal("tactic trials created model calls or acceptance")
	}
	seen := map[string]bool{}
	for _, check := range v.Verifications {
		if seen[check.Source] || check.Origin != "submitted" || check.Author != "procedure:"+leancheck.PortfolioVersion || check.Goal != *v.entity(id).FormalGoal {
			t.Fatal("sources or provenance mixed", check)
		}
		seen[check.Source] = true
	}
}
