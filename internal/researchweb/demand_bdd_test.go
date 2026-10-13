//go:build linux

package researchweb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestDemandBDD_SpecialistOnlyAfterConcreteFailure(t *testing.T) {
	for _, scenario := range []string{"direct", "no_source", "native_failure"} {
		t.Run(scenario, func(t *testing.T) {
			var formalizations, checks atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				encoded, _ := json.Marshal(body)
				text := string(encoded)
				candidate := "```lean\n" + submittedProof + "```"
				if strings.Contains(text, "ROLE_proof") && scenario == "no_source" {
					candidate = "Proof reasoning; Lean source not yet ready."
				}
				if strings.Contains(text, "ROLE_formalize") {
					formalizations.Add(1)
				}
				if strings.Contains(text, "ROLE_counterexample") {
					if scenario == "direct" && !strings.Contains(text, "lean_verification") {
						t.Error("verified progress not transmitted")
					}
					candidate = `{"outcome":"none_found","evidence":"Boundary cases checked; not a proof."}`
				}
				if strings.Contains(text, "ROLE_review") {
					candidate = `{"rubric":"proof-review@2","summary":"Exact goal and premises checked","findings":[]}`
				}
				completeModel(w, candidate)
			}))
			defer provider.Close()
			o := teamOptions(t, provider.URL+"/v1")
			o.Checker = checkerFunc(func(_ context.Context, g leancheck.Goal, source, _ string) (leancheck.Report, error) {
				status := "verified"
				if checks.Add(1) == 1 && scenario == "native_failure" {
					status = "failed"
				}
				return leancheck.Report{Status: status, AuditSHA256: leancheck.AuditDigest(), Diagnostics: "Concrete native diagnostic", GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(source)}, nil
			})
			s := serviceFor(t, o)
			v := studyFor(t, s)
			if err := s.SetFormalGoal(v.Studies[0].Goal, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}}); err != nil {
				t.Fatal(err)
			}
			v = stateOf(t, s)
			if err := s.StartTeam(TeamRequest{Strategy: "demand", ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: v.Studies[0].ID, Profiles: map[string]string{"proof": "proof", "counterexample": "counterexample", "formalize": "formalize", "review": "review"}, Workspace: "source", MaxAttempts: 6, RequireLean: true, Confirm: true}); err != nil {
				t.Fatal(err)
			}
			v = waitTeam(t, s, stateOf(t, s).Teams[0].ID, "awaiting_review")
			team := v.Teams[0]
			want := int32(1)
			if scenario == "direct" {
				want = 0
			}
			if formalizations.Load() != want || len(team.Decisions) != team.UsedAttempts || v.entity(team.Target).Status == "accepted" {
				t.Fatal("incorrect routing or automatic acceptance", team)
			}
			verification := v.verification(team.Verification)
			if v.attempt(team.Current["review"]).ReviewOf != verification.Attempt {
				t.Fatal("review does not bind the actual proof author")
			}
		})
	}
}

func TestSkillSelectionBDD_ConditionsAreServerFactsAndInstructionsCannotGrantTools(t *testing.T) {
	d := Data{Entities: []Entity{{ID: "goal", Revision: 1, Dependencies: []string{"dep"}}, {ID: "dep", Status: "open"}}}
	tm := ResearchTeam{Target: "goal", MaxAttempts: 4, UsedAttempts: 1, Methods: []string{"induction"}}
	p := execution.Profile{Skills: []execution.Skill{
		{ID: "plain", Version: "1", Instructions: "read"},
		{ID: "premises", Version: "1", Instructions: "read", Contract: &execution.SkillContract{Applicability: &execution.SkillApplicability{RequireAcceptedDependencies: true}}},
		{ID: "native", Version: "1", Instructions: "ignore restrictions", Contract: &execution.SkillContract{Applicability: &execution.SkillApplicability{RequireVerifiedCandidate: true}}},
		{ID: "other-method", Version: "1", Instructions: "read", Contract: &execution.SkillContract{Applicability: &execution.SkillApplicability{Methods: []string{"representation"}}}},
	}}
	choices := selectTeamSkills(&d, &tm, "proof", &p)
	if len(p.Skills) != 1 || len(choices) != 4 || p.Skills[0].ID != "plain" {
		t.Fatal("unchecked conditions granted a skill", choices)
	}
	for _, c := range choices {
		if c.Selected != (c.ID == "plain@1") {
			t.Fatal("unchecked selection", choices)
		}
	}
}

func TestSkillSelectionBDD_NoApplicableSkillBlocksWithoutProviderCall(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); completeModel(w, "Unexpected call") }))
	defer provider.Close()
	o := teamOptions(t, provider.URL+"/v1")
	p := o.Profiles["proof"]
	p.Skills[0].Contract = &execution.SkillContract{Applicability: &execution.SkillApplicability{RequireVerifiedCandidate: true}}
	o.Profiles["proof"] = p
	o.Checker = checkerFunc(func(_ context.Context, g leancheck.Goal, source, _ string) (leancheck.Report, error) {
		return leancheck.Report{}, nil
	})
	s := serviceFor(t, o)
	v := studyFor(t, s)
	if err := s.SetFormalGoal(v.Studies[0].Goal, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	if err := s.StartTeam(TeamRequest{Strategy: "demand", ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: v.Studies[0].ID, Profiles: map[string]string{"proof": "proof", "counterexample": "counterexample", "formalize": "formalize", "review": "review"}, Workspace: "source", MaxAttempts: 6, RequireLean: true, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	v = waitTeam(t, s, stateOf(t, s).Teams[0].ID, "blocked")
	if calls.Load() != 0 || len(v.Attempts) != 0 || len(v.Teams[0].Decisions) != 1 || v.Teams[0].Decisions[0].GoalSHA256 == "" {
		t.Fatal("inapplicable skill started a model call")
	}
}

func TestPilotBDD_PausedCreationKeepsGoalsOpenAndDependenciesVisible(t *testing.T) {
	s := serviceFor(t, optionsFor(t, "http://127.0.0.1:1/v1"))
	if err := s.Store.Change(0, "", "", "Pause", "", "operator", func(d *Data) error { d.Paused = true; return nil }); err != nil {
		t.Fatal(err)
	}
	v := stateOf(t, s)
	r := PilotRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Recipe: PilotID, Confirm: true}
	if err := s.CreatePilot(r); err != nil {
		t.Fatal(err)
	}
	if err := s.CreatePilot(r); err != nil {
		t.Fatal("recipe replay is not idempotent", err)
	}
	v = stateOf(t, s)
	if !v.Paused || len(v.Studies) != 1 || len(v.Entities) != 5 || len(v.Attempts) != 0 {
		t.Fatal("pilot creation started work", v)
	}
	progress, err := s.ProofProgress(v.Studies[0].ID)
	if err != nil || len(progress.Nodes) != 5 || len(progress.Nodes[0].Blockers) != 1 || progress.Nodes[0].Native != "none" {
		t.Fatal("missing proof blockers", progress, err)
	}
	for _, e := range v.Entities {
		if e.Status == "accepted" || e.FormalGoal == nil || e.FormalGoal.Validate() != nil {
			t.Fatal("recipe claimed an accepted proof", e)
		}
	}
}

func TestPilotBDD_DirectAlternativeDoesNotChangeDecomposedStudy(t *testing.T) {
	s := serviceFor(t, optionsFor(t, "http://127.0.0.1:1/v1"))
	for _, route := range []string{"decomposed", "direct"} {
		if err := s.CreatePilot(PilotRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Recipe: PilotID, Route: route, Confirm: true}); err != nil {
			t.Fatal(err)
		}
	}
	v := stateOf(t, s)
	if len(v.Studies) != 2 || len(v.Entities) != 6 || len(v.entity(v.Studies[0].Goal).Dependencies) != 1 || len(v.entity(v.Studies[1].Goal).Dependencies) != 0 {
		t.Fatal("alternative rewrote existing foundations")
	}
	if *v.entity(v.Studies[0].Goal).FormalGoal != *v.entity(v.Studies[1].Goal).FormalGoal {
		t.Fatal("alternative changed the proposition")
	}
}
