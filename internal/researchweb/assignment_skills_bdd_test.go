//go:build linux

package researchweb

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
)

func conditionalSkill(id, role, group string, priority int) execution.Skill {
	return execution.Skill{ID: id, Version: "1", Instructions: id, Contract: &execution.SkillContract{Applicability: &execution.SkillApplicability{Roles: []string{role}, ExclusiveGroup: group, Priority: priority, RequireFormalGoal: true}}}
}

func TestAssignmentSkillsBDD_DeterministicGroupsFilterBeforeReservation(t *testing.T) {
	d := Data{Entities: []Entity{{ID: "target", Revision: 1, FormalGoal: &leancheck.Goal{Source: "def P : Prop := True", Declaration: "P", Candidate: "Candidate"}}}}
	skills := []execution.Skill{conditionalSkill("z", "proof", "strategy", 40), conditionalSkill("a", "proof", "strategy", 40), conditionalSkill("wrong", "review", "strategy", 90), conditionalSkill("shared", "proof", "", 10)}
	var expected []SkillSelection
	for _, order := range [][]execution.Skill{skills, {skills[3], skills[2], skills[1], skills[0]}} {
		p := execution.Profile{Skills: order}
		selected := selectAssignmentSkills(&d, skillFacts{Target: "target", Role: "proof", Remaining: 3}, &p)
		if len(p.Skills) != 2 || p.Skills[0].ID != "a" || p.Skills[1].ID != "shared" {
			t.Fatal("wrong group precedence", p.Skills)
		}
		if expected != nil && !reflect.DeepEqual(expected, selected) {
			t.Fatal("selection depends on input order")
		}
		expected = selected
	}
}

func TestAssignmentSkillsBDD_TeamUsesStudyRemainderExcludingImportedHistory(t *testing.T) {
	d := Data{Studies: []Study{{ID: "study", Budget: &StudyBudget{MaxAttempts: 1}}}, Entities: []Entity{{ID: "target", Study: "study", FormalGoal: &leancheck.Goal{Source: "def P : Prop := True", Declaration: "P", Candidate: "Candidate"}}}, Attempts: []Attempt{{ID: "archived", Target: "target", ImportedPackageSHA256: "source-package"}}}
	skill := conditionalSkill("requires-two", "proof", "", 0)
	skill.Contract.Applicability.MinRemainingAttempts = 2
	p := execution.Profile{Skills: []execution.Skill{skill}}
	team := ResearchTeam{Target: "target", MaxAttempts: 6}
	selections := selectTeamSkills(&d, &team, "proof", &p)
	if len(p.Skills) != 0 || len(selections) != 1 || selections[0].Selected || remainingStudyAttempts(&d, "target") != 1 {
		t.Fatal("team ignored common budget or charged imported history", selections)
	}
	skill.Contract.Applicability.MinRemainingAttempts = 1
	p.Skills = []execution.Skill{skill}
	if selections = selectTeamSkills(&d, &team, "proof", &p); len(p.Skills) != 1 || !selections[0].Selected {
		t.Fatal("remaining attempt was hidden", selections)
	}
	d.Attempts = append(d.Attempts, Attempt{ID: "local", Target: "target"})
	p.Skills = []execution.Skill{skill}
	if selections = selectTeamSkills(&d, &team, "proof", &p); len(p.Skills) != 0 || selections[0].Selected {
		t.Fatal("exhausted common budget accepted a skill", selections)
	}
}

func TestAssignmentSkillsBDD_StandaloneAndPlanningRejectWithoutProviderCall(t *testing.T) {
	for _, kind := range []string{"proof", "decompose"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int32
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); completeModel(w, "unexpected") }))
			defer model.Close()
			o := optionsFor(t, model.URL+"/v1")
			p := o.Profiles["reader"]
			p.Skills = []execution.Skill{conditionalSkill("blocked", "review", "", 0)}
			o.Profiles["reader"] = p
			s := serviceFor(t, o)
			v := studyFor(t, s)
			target := v.Studies[0].Goal
			if err := s.SetFormalGoal(target, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: leancheck.Goal{Source: "def P : Prop := True", Declaration: "P", Candidate: "Candidate"}}); err != nil {
				t.Fatal(err)
			}
			v = stateOf(t, s)
			if kind == "proof" {
				v = act(t, s, Action{Type: "TASK", Target: target, Kind: kind, Title: "Proof", Text: "Prove exact goal"})
				if err := s.Start(RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[len(v.Tasks)-1].ID, Profile: "reader", Workspace: "source", Confirm: true}); err == nil {
					t.Fatal("inapplicable standalone launch accepted")
				}
			} else if err := s.RequestPlanning(PlanningRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Target: target, Profile: "reader", Workspace: "source", Confirm: true}); err == nil {
				t.Fatal("inapplicable planning launch accepted")
			}
			v = stateOf(t, s)
			if len(v.Attempts) != 0 || calls.Load() != 0 || v.Revision < 1 {
				t.Fatal("rejection consumed a request")
			}
		})
	}
}

func TestAssignmentSkillsBDD_StandalonePinsSelectionAndChosenMethod(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { completeModel(w, "candidate") }))
	defer model.Close()
	o := optionsFor(t, model.URL+"/v1")
	p := o.Profiles["reader"]
	p.Skills = []execution.Skill{conditionalSkill("proof", "proof", "", 0), conditionalSkill("review", "review", "", 0)}
	o.Profiles["reader"] = p
	s := serviceFor(t, o)
	v := studyFor(t, s)
	target := v.Studies[0].Goal
	if err := s.SetFormalGoal(target, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: leancheck.Goal{Source: "def P : Prop := True", Declaration: "P", Candidate: "Candidate"}}); err != nil {
		t.Fatal(err)
	}
	v = act(t, s, Action{Type: "TASK", Target: target, Kind: "proof", Title: "Proof", Text: "Proof"})
	r := RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[len(v.Tasks)-1].ID, Profile: "reader", Workspace: "source", Method: "induction", Confirm: true}
	if err := s.Start(r); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(r); err != nil {
		t.Fatal("idempotency", err)
	}
	a := waitAttempt(t, s, stateOf(t, s).Attempts[0].ID, func(a Attempt) bool { return a.Status == "candidate" })
	if a.AssignmentMethod != "induction" || len(a.SkillSelections) != 2 || len(a.ProfileConfiguration.Skills) != 1 || a.ProfileConfiguration.Skills[0].ID != "proof" {
		t.Fatal("selection was not pinned", a)
	}
	if len(s.profile("reader").Skills) != 2 {
		t.Fatal("selection mutated registered profile")
	}
}
