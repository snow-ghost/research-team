//go:build linux

package researchweb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestQueueBDD_RestartRetainsOnlyUnstartedWorkWithExplicitHold(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			s, target := submittedService(t, backend, "verified")
			s.cancel()
			s.wg.Wait()
			v := act(t, s, Action{Type: "TASK", Target: target, Kind: "proof", Title: "Queued source", Text: "No model call before explicit release"})
			if err := s.Start(RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[len(v.Tasks)-1].ID, Profile: "reader", Workspace: "source", Confirm: true}); err != nil {
				t.Fatal(err)
			}
			v = stateOf(t, s)
			id := v.Attempts[0].ID
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			next := serviceFor(t, s.Options)
			v = stateOf(t, next)
			a := v.attempt(id)
			if !v.Paused || a == nil || !a.QueueHeld || !queuedBeforeExecution(*a) {
				t.Fatal("unstarted job lost or automatically resumed", a)
			}
			r := QueueCommand{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Kind: "release", Note: "Explicit release while global pause remains", Confirm: true}
			if err := next.ControlQueue(id, r); err != nil {
				t.Fatal(err)
			}
			if err := next.ControlQueue(id, r); err != nil {
				t.Fatal("idempotent release", err)
			}
			v = stateOf(t, next)
			if !v.Paused || v.attempt(id).QueueHeld || v.attempt(id).Status != "queued" {
				t.Fatal("queue release bypassed global pause")
			}
			if err := next.Store.Change(0, "", "", "Unknown outcome fixture", target, "test", func(d *Data) error {
				a := d.attempt(id)
				a.RemoteOutcome = "unknown"
				a.Status = "interrupted"
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			r.ExpectedRevision = stateOf(t, next).Revision
			r.RequestID = identifier("cmd")
			if next.ControlQueue(id, r) == nil {
				t.Fatal("unknown request replayed through queue release")
			}
		})
	}
}

func TestWorkflowBDD_PlanGatePartsAndIndependentAcceptance(t *testing.T) {
	goal := func(name string) *leancheck.Goal {
		return &leancheck.Goal{Source: "def " + name + " : Prop := True\n", Declaration: name, Candidate: "proof" + name}
	}
	plan := DecompositionReport{Summary: "One lemma and explicit coverage", Claims: []ProposedClaim{{Title: "Part", Statement: "A separate claim", Assumptions: "Prop", FormalGoal: goal("Part")}}, Coverage: ProposedClaim{Title: "Coverage", Statement: "Parts imply the goal", Assumptions: "Accepted part", FormalGoal: goal("Coverage")}, Strategies: []ProposedStrategy{{Target: 0, Method: "specialization", Priority: 80, Rationale: "Prove the part"}, {Target: 1, Method: "equivalence", Priority: 60, Rationale: "Prove coverage"}, {Target: -1, Method: "decomposition", Priority: 40, Rationale: "Check the exact original goal"}}}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body any
		_ = json.NewDecoder(r.Body).Decode(&body)
		data, _ := json.Marshal(body)
		text := string(data)
		answer := "```lean\n" + submittedProof + "```"
		if strings.Contains(text, "Предложи план исследования") {
			b, _ := json.Marshal(plan)
			answer = string(b)
		} else if strings.Contains(text, "ROLE_counterexample") {
			answer = `{"outcome":"none_found","evidence":"Checked the explicit premises; not a proof by enumeration"}`
		} else if strings.Contains(text, "ROLE_review") {
			answer = `{"rubric":"proof-review@2","summary":"Exact checked source reviewed","findings":[]}`
		}
		completeModel(w, answer)
	}))
	defer provider.Close()
	o := teamOptions(t, provider.URL+"/v1")
	o.Preparer = goalPreparerFunc(func(_ context.Context, goals []leancheck.Goal, _ string) (leancheck.GoalPreparation, error) {
		return leancheck.GoalPreparation{Version: leancheck.PreparationVersion, Status: "ready", GoalsSHA256: leancheck.Digest(goals)}, nil
	})
	o.Checker = checkerFunc(func(_ context.Context, g leancheck.Goal, source, _ string) (leancheck.Report, error) {
		return leancheck.Report{Status: "verified", AuditSHA256: leancheck.AuditDigest(), GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(source)}, nil
	})
	s := serviceFor(t, o)
	v := studyFor(t, s)
	study, target := v.Studies[0].ID, v.Studies[0].Goal
	if err := s.SetFormalGoal(target, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: *goal("Statement")}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStudyBudget(study, BudgetRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Budget: StudyBudget{MaxAttempts: 30, MaxModelRequests: 60}, Note: "Finite local fixture budget", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	act(t, s, Action{Type: "PAUSE"})
	r := WorkflowRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Study: study, Planner: "reader", Team: TeamRequest{Profiles: map[string]string{"proof": "proof", "counterexample": "counterexample", "formalize": "formalize", "review": "review"}, Workspace: "source", MaxAttempts: 6, Strategy: "demand", RequireLean: true}, Confirm: true}
	if err := s.CreateWorkflow(r); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateWorkflow(r); err != nil {
		t.Fatal("idempotency", err)
	}
	if len(stateOf(t, s).Attempts) != 0 {
		t.Fatal("prepared workflow spent while globally paused")
	}
	act(t, s, Action{Type: "PAUSE"})
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		v = stateOf(t, s)
		if v.study(study).Workflow.Status == "awaiting_plan" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	w := v.study(study).Workflow
	if w.Status != "awaiting_plan" || len(v.Branches) != 0 || len(v.Entities) != 1 {
		t.Fatal("plan not gated", w)
	}
	if err := s.RequestPlanPreparation(w.Proposal, WorkflowCommand{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Note: "Compile all fixture goals before approval", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	for time.Now().Before(deadline) {
		v = stateOf(t, s)
		if v.proposal(w.Proposal).Preparation.Status == "ready" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := s.ControlWorkflow(study, WorkflowCommand{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Kind: "approve_plan", Note: "Explicit approval of statements and coverage", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	for time.Now().Before(deadline) {
		v = stateOf(t, s)
		if v.study(study).Workflow.Status == "completed" {
			break
		}
		for _, team := range v.Teams {
			if team.Status == "awaiting_review" && v.entity(team.Target).Status == "in_review" {
				err := s.Act(Action{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Type: "REVIEW", Target: team.Target, Decision: "accept", Text: "Independent fixture acceptance, not coordinator acceptance"})
				if err != nil && err != ErrConflict {
					t.Fatal(err)
				}
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	v = stateOf(t, s)
	if v.study(study).Workflow.Status != "completed" || v.entity(target).Status != "accepted" || len(v.Branches) != 3 {
		t.Fatal("workflow failed to complete the gated chain", v.study(study).Workflow, v.Teams)
	}
	for _, a := range v.Attempts {
		if a.Role != "review" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(s.Options.Config.DataDir, "attempts", a.ID, "request-packet.json"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "candidate_template") || !strings.Contains(string(body), "claim_under_review") || strings.Contains(string(body), o.Config.DataDir) {
			t.Fatal("review packet contains ambiguous templates or local paths")
		}
		h, err := NewHTTP(s, strings.Repeat("k", 64))
		if err != nil {
			t.Fatal(err)
		}
		for _, auth := range []bool{false, true} {
			r := httptest.NewRequest("GET", "http://127.0.0.1:4187/api/attempts/"+a.ID+"/request-packet", nil)
			if auth {
				r.Header.Set("Authorization", "Bearer "+strings.Repeat("k", 64))
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if !auth && w.Code != 401 || auth && (w.Code != 200 || w.Body.String() != string(body) || w.Header().Get("Cache-Control") != "no-store") {
				t.Fatal("request packet authorization or content differs", w.Code)
			}
		}
		h.Close()
	}
}

func TestWorkflowBDD_ClarificationNeedsOperatorAnswerAndSeparateResume(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		completeModel(w, `{"outcome":"needs_input","questions":["What finite domain is intended?"]}`)
	}))
	defer provider.Close()
	o := teamOptions(t, provider.URL+"/v1")
	o.Checker = checkerFunc(func(_ context.Context, _ leancheck.Goal, _, _ string) (leancheck.Report, error) {
		t.Error("clarification must not request a proof check")
		return leancheck.Report{}, nil
	})
	s := serviceFor(t, o)
	v := studyFor(t, s)
	study, target := v.Studies[0].ID, v.Studies[0].Goal
	if err := s.SetFormalGoal(target, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStudyBudget(study, BudgetRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Budget: StudyBudget{MaxAttempts: 10, MaxModelRequests: 20}, Note: "Finite local fixture", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateWorkflow(WorkflowRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Study: study, Planner: "reader", Team: TeamRequest{Profiles: map[string]string{"proof": "proof", "counterexample": "counterexample", "formalize": "formalize", "review": "review"}, Workspace: "source", MaxAttempts: 6, Strategy: "demand", RequireLean: true}, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		v = stateOf(t, s)
		if v.study(study).Workflow.Status == "needs_input" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	w := v.study(study).Workflow
	if w.Status != "needs_input" || len(w.QuestionIDs) != 1 || len(v.Attempts) != 1 {
		t.Fatal("clarification did not stop new assignments", w)
	}
	command := func(kind string) error {
		return s.ControlWorkflow(study, WorkflowCommand{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Kind: kind, Note: "Explicit operator decision", Confirm: true})
	}
	if command("resume") == nil {
		t.Fatal("unanswered question resumed")
	}
	id := w.QuestionIDs[0]
	answer := WorkflowAnswer{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Answer: "Only finite port sets with positive n", Confirm: true}
	if err := s.AnswerWorkflowQuestion(id, answer); err != nil {
		t.Fatal(err)
	}
	if err := s.AnswerWorkflowQuestion(id, answer); err != nil {
		t.Fatal("answer idempotency", err)
	}
	answer.ExpectedRevision = stateOf(t, s).Revision
	answer.RequestID = identifier("cmd")
	if s.AnswerWorkflowQuestion(id, answer) == nil {
		t.Fatal("answer overwritten")
	}
	if len(stateOf(t, s).Attempts) != 1 {
		t.Fatal("answer automatically launched a request")
	}
	act(t, s, Action{Type: "PAUSE"})
	if err := command("resume"); err != nil {
		t.Fatal(err)
	}
	if err := command("pause"); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	if v.study(study).Workflow.Status != "paused" {
		t.Fatal("workflow pause not saved")
	}
	if err := command("resume"); err != nil {
		t.Fatal(err)
	}
	if !stateOf(t, s).Paused || len(stateOf(t, s).Attempts) != 1 {
		t.Fatal("resume bypassed global pause")
	}
	if err := command("stop"); err != nil {
		t.Fatal(err)
	}
	if err := command("resume"); err == nil {
		t.Fatal("stopped workflow resumed")
	}
}
