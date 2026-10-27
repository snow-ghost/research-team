//go:build linux

package researchweb

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

type goalPreparerFunc func(context.Context, []leancheck.Goal, string) (leancheck.GoalPreparation, error)

func (f goalPreparerFunc) PrepareGoals(c context.Context, g []leancheck.Goal, d string) (leancheck.GoalPreparation, error) {
	return f(c, g, d)
}

func TestPlanPreparationBDD_CrossStudyReuseIncludesPinnedDefinitionPacksOnce(t *testing.T) {
	s := &Service{Options: Options{Lean: &leancheck.Config{}, Config: Config{DataDir: t.TempDir()}}}
	d := emptyData()
	d.Studies = []Study{{ID: "first", Definitions: &PreparedPlanDefinitions{CheckID: "first-pack"}}, {ID: "second", Definitions: &PreparedPlanDefinitions{CheckID: "second-pack"}}}
	d.Entities = []Entity{{ID: "goal", Study: "second", Dependencies: []string{"lemma", "other"}}, {ID: "lemma", Study: "first"}, {ID: "other", Study: "first"}}
	env, _ := s.libraryEnvironment(&d, &d.Entities[0])
	for _, id := range []string{"first-pack", "second-pack"} {
		path := filepath.Join(s.Options.Config.DataDir, "preparations", id, "checked")
		if !slices.Contains(env.Libraries, path) {
			t.Fatal("missing shared definitions", id)
		}
	}
	if len(env.Libraries) != 2 {
		t.Fatal("duplicate definition packs", env.Libraries)
	}
}

func TestPlanPreparationBDD_FailedGoalsCannotBeAppliedAndRevisionPreservesOriginal(t *testing.T) {
	o := optionsFor(t, "http://127.0.0.1:1/v1")
	o.Preparer = goalPreparerFunc(func(_ context.Context, g []leancheck.Goal, _ string) (leancheck.GoalPreparation, error) {
		return leancheck.GoalPreparation{Version: leancheck.PreparationVersion, Status: "failed", GoalsSHA256: leancheck.Digest(g), Diagnostics: "unknown module prefix Goal"}, nil
	})
	s := serviceFor(t, o)
	v := studyFor(t, s)
	target := v.Studies[0].Goal
	g := leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}
	if err := s.SetFormalGoal(target, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: g}); err != nil {
		t.Fatal(err)
	}
	plan := DecompositionReport{Summary: "Original invalid goal", Claims: []ProposedClaim{{Title: "Part", Statement: "True", Assumptions: "Prop", FormalGoal: &g}}, Coverage: ProposedClaim{Title: "Coverage", Statement: "True", Assumptions: "Part", FormalGoal: &g}}
	id := identifier("proposal")
	if err := s.Store.Change(0, "", "", "Proposal fixture", target, "test", func(d *Data) error {
		d.Proposals = append(d.Proposals, Decomposition{ID: id, Target: target, TargetRevision: d.entity(target).Revision, Report: plan, Status: "proposed"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	if err := s.planPreparationMatches(&v.Data, v.proposal(id)); err == nil {
		t.Fatal("unchecked plan allowed")
	}
	r := WorkflowCommand{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Confirm: true, Note: "Compile without model work"}
	if err := s.RequestPlanPreparation(id, r); err != nil {
		t.Fatal(err)
	}
	if err := s.RequestPlanPreparation(id, r); err != nil {
		t.Fatal("idempotency", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		v = stateOf(t, s)
		if v.proposal(id).Preparation.Status == "failed" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if v.proposal(id).Preparation.Status != "failed" || s.planPreparationMatches(&v.Data, v.proposal(id)) == nil || len(v.Attempts) != 0 || len(v.Verifications) != 0 || len(v.Entities) != 1 {
		t.Fatal("preparation accepted a goal or launched a model")
	}
	fixed := plan
	fixed.Summary = "Operator corrected source, original result retained"
	revision := PlanRevisionRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Report: fixed, Note: "Fix only the invalid source, retain raw response", Confirm: true}
	if err := s.RevisePlan(id, revision); err != nil {
		t.Fatal(err)
	}
	if err := s.RevisePlan(id, revision); err != nil {
		t.Fatal("revision idempotency", err)
	}
	v = stateOf(t, s)
	next := v.Proposals[len(v.Proposals)-1]
	if v.proposal(id).Report.Summary != plan.Summary || v.proposal(id).Status != "superseded" || next.Parent != id || next.Author != "operator" || next.Preparation != nil {
		t.Fatal("proposal provenance changed", next)
	}
	if s.planPreparationMatches(&v.Data, &next) == nil {
		t.Fatal("old check reused after editing")
	}
}
