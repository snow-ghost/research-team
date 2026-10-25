//go:build linux

package researchweb

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

type moduleMatcherFunc func(context.Context, leancheck.Goal, string, leancheck.ModuleReport, string, string) (leancheck.MatchReport, error)

func (f moduleMatcherFunc) MatchModule(c context.Context, g leancheck.Goal, p string, m leancheck.ModuleReport, n, d string) (leancheck.MatchReport, error) {
	return f(c, g, p, m, n, d)
}

func TestCatalogBDD_BoundedChecksPreserveFailureCacheAndDoNotAccept(t *testing.T) {
	s, p := packageFixture(t)
	s.Options.Lean = &leancheck.Config{}
	v := act(t, s, Action{Type: "CREATE_STUDY", Title: "Catalog target", Statement: "True", Assumptions: "Prop"})
	target := v.Studies[len(v.Studies)-1].Goal
	if err := s.SetFormalGoal(target, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: leancheck.Goal{Source: "def CatalogGoal : Prop := True", Declaration: "CatalogGoal", Candidate: "CatalogCandidate"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Change(0, "", "", "Catalog fixture", target, "test", func(d *Data) error {
		e := d.entity(p.Target.ID)
		for i := 0; i < 3; i++ {
			d.Library = append(d.Library, LibraryEntry{ID: fmt.Sprintf("library-catalog-%02d", i), Lemma: e.ID, LemmaRevision: e.Revision, Result: e.ResearchResult, Status: "ready", Goal: *e.FormalGoal, Module: fmt.Sprintf("CatalogModule%d", i), Report: &leancheck.ModuleReport{Status: "ready", AuditSHA256: leancheck.AuditDigest(), ArtifactSHA256: "fixture", Artifacts: map[string]string{"module.olean": "fixture"}}})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	s.Options.Matcher = moduleMatcherFunc(func(_ context.Context, g leancheck.Goal, _ string, m leancheck.ModuleReport, _, _ string) (leancheck.MatchReport, error) {
		calls++
		r := leancheck.MatchReport{Status: "no_match", GoalSHA256: leancheck.Digest(g), ArtifactSHA256: m.ArtifactSHA256, MatcherSHA256: leancheck.MatcherDigest(), Bindings: []leancheck.Argument{}, Missing: []leancheck.MissingArgument{}}
		if calls == 1 {
			return r, errors.New("native failure")
		}
		return r, nil
	})
	req := CatalogSearchRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Target: target, Limit: 2, TimeoutSeconds: 30, Confirm: true}
	result, err := s.SearchCatalog(context.Background(), req)
	after := stateOf(t, s)
	if err != nil || calls != 2 || result.Status != "bounded" || len(result.Matches) != 2 || result.Matches[0].Report.Status != "failed" || after.entity(target).Status != "open" {
		t.Fatal(result, err, calls)
	}
	if _, err := s.SearchCatalog(context.Background(), req); err != nil || calls != 2 {
		t.Fatal("cached checks replayed", err)
	}
	if err := s.ApplyLemmaMatch(result.Matches[0].ID, ApplyMatchRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Confirm: true}); err == nil {
		t.Fatal("failed native check applied")
	}
}
