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
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestAdaptiveBDD_NegativeRepairUsesFormalizerAndIndependentReviewer(t *testing.T) {
	var checks, proofs atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		encoded, _ := json.Marshal(body)
		text := string(encoded)
		candidate := `{"outcome":"counterexample_candidate","evidence":"n = 0 violates the statement"}`
		if strings.Contains(text, "ROLE_proof") {
			proofs.Add(1)
		}
		if strings.Contains(text, "ROLE_formalize") {
			if !strings.Contains(text, "candidate_template") || !strings.Contains(text, "ResearchRefutation.candidate") {
				t.Error("negative template missing")
			}
			candidate = "```lean\nimport Goal\ntheorem ResearchRefutation.candidate : ResearchRefutation.Statement := by simp [ResearchRefutation.Statement, Statement]\n```"
		}
		if strings.Contains(text, "ROLE_review") {
			candidate = `{"summary":"Negative statement checked","findings":[]}`
		}
		completeModel(w, candidate)
	}))
	defer model.Close()
	o := teamOptions(t, model.URL+"/v1")
	o.Checker = checkerFunc(func(_ context.Context, g leancheck.Goal, source, _ string) (leancheck.Report, error) {
		status := "verified"
		if checks.Add(1) == 1 {
			status = "failed"
		}
		if g.Declaration != "ResearchRefutation.Statement" {
			t.Error("formalizer checked the positive statement")
		}
		return leancheck.Report{Status: status, AuditSHA256: leancheck.AuditDigest(), Diagnostics: "repair this tactic", GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(source)}, nil
	})
	s := serviceFor(t, o)
	v := studyFor(t, s)
	goal := v.Studies[0].Goal
	if err := s.SetFormalGoal(goal, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: leancheck.Goal{Source: "def Statement : Prop := False", Declaration: "Statement", Candidate: "Candidate"}}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	if err := s.StartTeam(TeamRequest{Strategy: "adaptive", Methods: []string{"specialization", "representation"}, ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: v.Studies[0].ID, Profiles: map[string]string{"proof": "proof", "counterexample": "counterexample", "formalize": "formalize", "review": "review"}, Workspace: "source", MaxAttempts: 6, RequireLean: true, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	id := stateOf(t, s).Teams[0].ID
	v = waitTeam(t, s, id, "awaiting_review")
	team := v.team(id)
	verification := v.verification(team.Verification)
	if proofs.Load() != 0 || checks.Load() != 2 || team.UsedAttempts != 4 || verification.Attempt != team.Current["formalize"] || v.attempt(team.Current["review"]).ReviewOf != verification.Attempt || v.entity(goal).Status == "refuted" {
		t.Fatalf("bad negative routing: %+v %+v", team, verification)
	}
}

func TestSkillsBDD_ImmutableVersionsAndNoToolEscalation(t *testing.T) {
	s := serviceFor(t, optionsFor(t, "http://127.0.0.1:1/v1"))
	contract := execution.SkillContract{Preconditions: []string{"Pinned Lean goal"}, Inputs: []string{"Goal"}, Output: "Lean source", RequiredTools: []string{"check_lean"}, StopCriteria: []string{"Verified or budget exhausted"}}
	r := SkillRequest{Name: "formal-proof", Label: "Формализация", Instructions: "Prove the imported goal", Contract: contract, Confirm: true}
	for i := 0; i < 2; i++ {
		r.ExpectedRevision, r.RequestID = stateOf(t, s).Revision, identifier("cmd")
		if err := s.CreateSkill(r); err != nil {
			t.Fatal(err)
		}
		r.Instructions = "Revised procedure"
	}
	v := stateOf(t, s)
	if v.SkillRevisions[0].Configuration.Instructions != "Prove the imported goal" || v.SkillRevisions[0].SHA256 == v.SkillRevisions[1].SHA256 {
		t.Fatal("old skill overwritten")
	}
	p := s.profile("reader")
	if err := resolveSkills(&v.Data, []string{"formal-proof@1"}, &p); err == nil {
		t.Fatal("skill widened tool permissions")
	}
	p.Model.Tools = []string{"check_lean"}
	if err := resolveSkills(&v.Data, []string{"formal-proof@1"}, &p); err != nil || p.Skills[len(p.Skills)-1].Version != "1" {
		t.Fatal(err)
	}
}

func TestTeamBDD_MalformedFormalizationIsRepairedWithoutEditingResponse(t *testing.T) {
	var formalizations atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		encoded, _ := json.Marshal(body)
		text := string(encoded)
		candidate := "Mathematical proof"
		if strings.Contains(text, "ROLE_counterexample") {
			candidate = `{"outcome":"none_found","evidence":"Boundary checked"}`
		}
		if strings.Contains(text, "ROLE_formalize") {
			candidate = "No completed Lean block"
			if formalizations.Add(1) > 1 {
				if !strings.Contains(text, "coordinator_diagnostic") {
					t.Error("format diagnostic missing")
				}
				candidate = "```lean\nimport Goal\ntheorem Candidate : Statement := by trivial\n```"
			}
		}
		if strings.Contains(text, "ROLE_review") {
			candidate = `{"summary":"Reviewed","findings":[]}`
		}
		completeModel(w, candidate)
	}))
	defer model.Close()
	o := teamOptions(t, model.URL+"/v1")
	o.Checker = checkerFunc(func(_ context.Context, g leancheck.Goal, source, _ string) (leancheck.Report, error) {
		return leancheck.Report{Status: "verified", AuditSHA256: leancheck.AuditDigest(), GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(source)}, nil
	})
	s := serviceFor(t, o)
	v := studyFor(t, s)
	goal := v.Studies[0].Goal
	if err := s.SetFormalGoal(goal, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	if err := s.StartTeam(TeamRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: v.Studies[0].ID, Profiles: map[string]string{"proof": "proof", "counterexample": "counterexample", "formalize": "formalize", "review": "review"}, Workspace: "source", MaxAttempts: 6, RequireLean: true, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	id := stateOf(t, s).Teams[0].ID
	v = waitTeam(t, s, id, "awaiting_review")
	if formalizations.Load() != 2 || v.team(id).UsedAttempts != 5 || len(v.Verifications) != 1 {
		t.Fatal("invalid automatic format repair", v.Teams)
	}
	for _, a := range v.Attempts {
		if a.Role == "formalize" && a.ParentAttempt == "" {
			result, err := s.readResult(a)
			if err != nil || result.Candidate != "No completed Lean block" {
				t.Fatal("raw response was edited", err)
			}
		}
	}
}

func TestSchedulerBDD_IdleStateIsNotReloadedAndQueueSurvivesMaintenance(t *testing.T) {
	o := optionsFor(t, "http://127.0.0.1:1/v1")
	s := serviceFor(t, o)
	v := studyFor(t, s)
	goal := v.Studies[0].Goal
	err := s.Store.Change(0, "", "", "Fixture", goal, "test", func(d *Data) error {
		d.Paused = true
		d.addTask(goal, "Queued task", "proof", "Prove")
		task := &d.Tasks[len(d.Tasks)-1]
		task.Attempt = "held-attempt"
		d.Attempts = append(d.Attempts, Attempt{ID: task.Attempt, TaskID: task.ID, Target: goal, TargetRevision: d.entity(goal).Revision, Profile: "reader", Workspace: "source", Status: "queued", RemoteOutcome: "not_started"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PrepareMaintenance(MaintenanceRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Confirm: true}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	before := s.SchedulerMetrics()
	time.Sleep(400 * time.Millisecond)
	after := s.SchedulerMetrics()
	if after["cycles"] != before["cycles"] || after["full_reads"] != before["full_reads"] || after["revision_probes"] <= before["revision_probes"] {
		t.Fatalf("idle full traversal: %v -> %v", before, after)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	restarted := serviceFor(t, o)
	v = stateOf(t, restarted)
	if v.attempt("held-attempt").Status != "queued" || !maintenanceValid(v.Data) {
		t.Fatal("maintenance lost the queued attempt")
	}
}
