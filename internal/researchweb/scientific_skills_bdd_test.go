//go:build linux

package researchweb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestScientificSkillsBDD_TemplatesAreVersionedAndRoleBounded(t *testing.T) {
	s := serviceFor(t, optionsFor(t, "http://127.0.0.1:1/v1"))
	v := studyFor(t, s)
	target := v.Studies[0].Goal
	if err := s.SetFormalGoal(target, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		role     string
		priority int
	}{
		{"logic-map", "decompose", 80},
		{"constraint-analysis", "proof", 70},
		{"counterexample-first", "counterexample", 100},
		{"lemma-reuse", "proof", 100},
		{"logika", "review", 100},
		{"triz", "decompose", 60},
		{"goldratt-toc", "decompose", 75},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("..", "..", "examples", "skills", test.name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var request SkillRequest
			if err := json.Unmarshal(body, &request); err != nil {
				t.Fatal(err)
			}
			request.ExpectedRevision = stateOf(t, s).Revision
			request.RequestID = identifier("cmd")
			if err := s.CreateSkill(request); err != nil {
				t.Fatal(err)
			}
			candidate := "Unverified candidate within the role contract"
			if test.name == "counterexample-first" {
				candidate = `{"schema":"counterexample-report@2","outcome":"none_found","evidence":"Checked n <= 4","limitations":"Finite boundary search only","unresolved_obligations":[]}`
			}
			if test.name == "logika" {
				candidate = `{"rubric":"proof-review@2","summary":"Formal validity and premise verification checked separately","findings":[]}`
			}
			trial, err := s.TrialSkill(test.name+"@1", SkillTrialRequest{
				ExpectedRevision: stateOf(t, s).Revision,
				RequestID:        identifier("cmd"),
				Target:           target,
				Profile:          "reader",
				Role:             test.role,
				Candidate:        candidate,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !trial.Applicable || trial.MathematicalVerification || len(trial.Selections) != 1 || !trial.Selections[0].Selected {
				t.Fatalf("unexpected skill trial: %+v", trial)
			}
			if (test.name == "counterexample-first" || test.name == "logika") && (len(trial.Checks) != 1 || !trial.Checks[0].Passed) {
				t.Fatalf("typed role report rejected: %+v", trial.Checks)
			}
			v := stateOf(t, s)
			revision := v.SkillRevisions[len(v.SkillRevisions)-1]
			if revision.ID != test.name+"@1" || revision.Configuration.Contract.Applicability.Priority != test.priority || revision.SHA256 != hash(revision.Configuration) {
				t.Fatalf("invalid immutable revision: %+v", revision)
			}
			p := s.profile("reader")
			p.Skills = nil
			if err := resolveSkills(&v.Data, []string{revision.ID}, &p); err != nil {
				t.Fatal(err)
			}
			wrongRole := "review"
			if test.name == "logika" {
				wrongRole = "answer"
			}
			selections := selectAssignmentSkills(&v.Data, skillFacts{Target: target, Role: wrongRole, Remaining: 3}, &p)
			if len(p.Skills) != 0 || len(selections) != 1 || selections[0].Selected {
				t.Fatal("skill escaped its assigned roles", selections)
			}
		})
	}

	v = stateOf(t, s)
	p := s.profile("reader")
	p.Skills = nil
	if err := resolveSkills(&v.Data, []string{"lemma-reuse@1"}, &p); err != nil {
		t.Fatal(err)
	}
	selections := selectAssignmentSkills(&v.Data, skillFacts{Target: target, Role: "proof", Remaining: 1}, &p)
	if len(p.Skills) != 0 || len(selections) != 1 || selections[0].Selected {
		t.Fatal("lemma reuse ignored the verification and review reserve", selections)
	}
}
