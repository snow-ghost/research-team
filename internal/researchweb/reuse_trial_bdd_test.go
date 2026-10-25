//go:build linux

package researchweb

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestReuseTrialBDD_TwoChecksNoModelNoAcceptanceAndStaleCacheRefused(t *testing.T) {
	s, p := packageFixture(t)
	s.Options.Lean = &leancheck.Config{}
	v := act(t, s, Action{Type: "CREATE_STUDY", Title: "Independent transfer target", Statement: "True", Assumptions: "Prop"})
	target := v.Studies[len(v.Studies)-1].Goal
	g := leancheck.Goal{Source: "def ReuseTarget : Prop := True", Declaration: "ReuseTarget", Candidate: "reuseCandidate"}
	if err := s.SetFormalGoal(target, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: g}); err != nil {
		t.Fatal(err)
	}
	id := "library-reuse-test"
	module := "ResearchLemma_66666666666666666666666666666666"
	dir := filepath.Join(s.Options.Config.DataDir, "library", id, "checked")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	body := []byte("fixture bytes; native checker replaced")
	if err := os.WriteFile(filepath.Join(dir, module+".olean"), body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Change(0, "", "", "Reuse fixture", target, "test", func(d *Data) error {
		e := d.entity(p.Target.ID)
		d.Library = append(d.Library, LibraryEntry{ID: id, Lemma: e.ID, LemmaRevision: e.Revision, Result: e.ResearchResult, Status: "ready", Goal: *e.FormalGoal, Module: module, Report: &leancheck.ModuleReport{Status: "ready", Module: module, AuditSHA256: leancheck.AuditDigest(), ArtifactSHA256: leancheck.Digest(body), Artifacts: map[string]string{module + ".olean": leancheck.Digest(body)}}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	s.Options.ChainChecker = checkerFunc(func(_ context.Context, g leancheck.Goal, source, _ string) (leancheck.Report, error) {
		calls++
		status := "failed"
		if strings.Contains(g.Source, module) {
			status = "verified"
		}
		return leancheck.Report{Status: status, Phase: "compile", AuditSHA256: leancheck.AuditDigest(), GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(source)}, nil
	})
	r := ReuseTrialRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Target: target, Library: id, Depth: 3, TimeoutSeconds: 30, Confirm: true}
	match, err := s.CompareLemmaReuse(context.Background(), r)
	if err != nil || calls != 2 || match.Reuse == nil || match.Reuse.Without.Status != "not_proved" || match.Report.Status != "verified" {
		t.Fatal(match, err, calls)
	}
	if _, err := s.CompareLemmaReuse(context.Background(), r); err != nil || calls != 2 {
		t.Fatal("cached pair re-executed", err, calls)
	}
	v = stateOf(t, s)
	if v.entity(target).Status != "open" || len(v.Attempts) != len(p.Attempts) {
		t.Fatal("trial performed acceptance or model execution")
	}
	g.Source += "\n-- updated target"
	if err := s.SetFormalGoal(target, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: g}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompareLemmaReuse(context.Background(), r); err == nil || calls != 2 {
		t.Fatal("stale paired trial reused", err)
	}
}
