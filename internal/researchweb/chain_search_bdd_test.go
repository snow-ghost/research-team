//go:build linux

package researchweb

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestChainsAPI_BoundedCheckCachesWithoutAcceptingAndRejectsStaleGoal(t *testing.T) {
	s, p := packageFixture(t)
	s.Options.Lean = &leancheck.Config{}
	v := act(t, s, Action{Type: "CREATE_STUDY", Title: "Chain target", Statement: "True", Assumptions: "Prop"})
	target := v.Studies[len(v.Studies)-1].Goal
	goal := leancheck.Goal{Source: "def ChainTarget : Prop := True", Declaration: "ChainTarget", Candidate: "chainCandidate"}
	if err := s.SetFormalGoal(target, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: goal}); err != nil {
		t.Fatal(err)
	}
	id, module := "library-chain-test", "ChainFixture"
	dir := filepath.Join(s.Options.Config.DataDir, "library", id, "checked")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	body := []byte("synthetic module bytes; checker replaced in this test")
	if err := os.WriteFile(filepath.Join(dir, module+".olean"), body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Change(0, "", "", "Chain fixture", target, "test", func(d *Data) error {
		e := d.entity(p.Target.ID)
		d.Library = append(d.Library, LibraryEntry{ID: id, Lemma: e.ID, LemmaRevision: e.Revision, Result: e.ResearchResult, Status: "ready", Goal: *e.FormalGoal, Module: module, Report: &leancheck.ModuleReport{Status: "ready", Module: module, AuditSHA256: leancheck.AuditDigest(), ArtifactSHA256: leancheck.Digest(body), Artifacts: map[string]string{module + ".olean": leancheck.Digest(body)}}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	s.Options.ChainChecker = checkerFunc(func(_ context.Context, g leancheck.Goal, source, _ string) (leancheck.Report, error) {
		calls++
		return leancheck.Report{Status: "verified", AuditSHA256: leancheck.AuditDigest(), GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(source)}, nil
	})
	r := ChainSearchRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Target: target, Limit: 2, Depth: 3, MaxTrials: 4, TimeoutSeconds: 30, Confirm: true}
	result, err := s.SearchLemmaChains(context.Background(), r)
	if err != nil || result.Status != "verified" || len(result.Matches) != 1 || result.Matches[0].Chain == nil || calls != 1 {
		t.Fatal(result, err, calls)
	}
	if _, err := s.SearchLemmaChains(context.Background(), r); err != nil || calls != 1 {
		t.Fatal("cached search replayed", err, calls)
	}
	v = stateOf(t, s)
	if v.entity(target).Status != "open" {
		t.Fatal("native search accepted the goal")
	}
	for _, a := range v.Attempts {
		if a.Target == target {
			t.Fatal("native search started an agent")
		}
	}
	goal.Source = "def ChainTarget : Prop := False"
	if err := s.SetFormalGoal(target, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: goal}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SearchLemmaChains(context.Background(), r); err == nil || calls != 1 {
		t.Fatal("stale match reused", err, calls)
	}
}
