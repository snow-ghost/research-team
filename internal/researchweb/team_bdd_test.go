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
	"sync/atomic"
	"testing"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
)

type checkerFunc func(context.Context, leancheck.Goal, string, string) (leancheck.Report, error)

func (f checkerFunc) Check(c context.Context, g leancheck.Goal, s, d string) (leancheck.Report, error) {
	return f(c, g, s, d)
}
func teamOptions(t *testing.T, base string) Options {
	o := optionsFor(t, base)
	for _, role := range []string{"proof", "counterexample", "formalize", "review"} {
		p := o.Profiles["reader"]
		p.ID = role
		p.Skills = []execution.Skill{{ID: role, Version: "1", Instructions: "ROLE_" + role}}
		o.Profiles[role] = p
	}
	return o
}
func waitTeam(t *testing.T, s *Service, id, status string) View {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	if s.Options.Lean != nil {
		deadline = time.Now().Add(120 * time.Second)
	}
	for time.Now().Before(deadline) {
		v := stateOf(t, s)
		if team := v.team(id); team != nil && team.Status == status {
			return v
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("team did not reach %s: %+v", status, stateOf(t, s).Teams)
	return View{}
}
func TestTeamBDD_FormalRepairReviewAndOperatorAcceptance(t *testing.T) {
	var modelCalls, checks atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modelCalls.Add(1)
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		text := ""
		for _, m := range body.Messages {
			text += m.Content
		}
		candidate := "Proof argument."
		if strings.Contains(text, "ROLE_counterexample") {
			candidate = `{"outcome":"none_found","evidence":"Checked zero and boundary cases."}`
		}
		if strings.Contains(text, "ROLE_formalize") {
			candidate = "```lean\nimport Goal\ntheorem Candidate : Statement := by simp [Statement]\n```"
		}
		if strings.Contains(text, "ROLE_review") {
			candidate = `{"summary":"Reviewed","findings":[]}`
		}
		completeModel(w, candidate)
	}))
	defer model.Close()
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			o := teamOptions(t, model.URL+"/v1")
			o.Checker = checkerFunc(func(_ context.Context, g leancheck.Goal, source, _ string) (leancheck.Report, error) {
				n := checks.Add(1)
				status := "verified"
				if n%2 == 1 {
					status = "failed"
				}
				return leancheck.Report{AuditSHA256: leancheck.AuditDigest(), Status: status, Phase: "audit", Diagnostics: "repair required", GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(source)}, nil
			})
			if backend == "postgres" {
				dsn := postgresDSN(t)
				o.Config.Database = DatabaseConfig{Driver: "postgres", DSNEnv: "TEST_DB"}
				lookup := o.Lookup
				o.Lookup = func(key string) (string, bool) {
					if key == "TEST_DB" {
						return dsn, true
					}
					return lookup(key)
				}
			}
			s := serviceFor(t, o)
			v := studyFor(t, s)
			goal := v.Studies[0].Goal
			if err := s.SetFormalGoal(goal, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}}); err != nil {
				t.Fatal(err)
			}
			v = stateOf(t, s)
			r := TeamRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: v.Studies[0].ID, Profiles: map[string]string{"proof": "proof", "counterexample": "counterexample", "formalize": "formalize", "review": "review"}, Workspace: "source", MaxAttempts: 6, RequireLean: true, Confirm: true}
			if err := s.StartTeam(r); err != nil {
				t.Fatal(err)
			}
			if err := s.StartTeam(r); err != nil {
				t.Fatal("idempotency", err)
			}
			id := stateOf(t, s).Teams[0].ID
			v = waitTeam(t, s, id, "awaiting_review")
			team := v.team(id)
			if team.UsedAttempts != 5 || v.entity(goal).Status != "in_review" || len(v.Verifications) != 2 {
				t.Fatalf("bad pipeline: %+v", team)
			}
			proof := v.entity(goal)
			if !hasVerifiedProof(&v.Data, proof) || !teamReviewReady(&v.Data, proof) {
				t.Fatal("verification or separate review missing")
			}
			if a := v.attempt(team.Current["review"]); "executor:"+a.Profile == proof.ProofAuthor || a.ReviewOf != proof.ProofAttempt {
				t.Fatal("self review")
			}
			act(t, s, Action{Type: "REVIEW", Target: goal, Decision: "accept", Text: "Проверена точная цель, условия и независимая рецензия."})
			waitTeam(t, s, id, "completed")
		})
	}
}
func TestTeamBDD_CounterexampleBlocksAndBudgetStopsRejection(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		completeModel(w, `{"outcome":"counterexample_candidate","evidence":"Potential counterexample"}`)
	}))
	defer model.Close()
	s := serviceFor(t, teamOptions(t, model.URL+"/v1"))
	v := studyFor(t, s)
	if err := s.StartTeam(TeamRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: v.Studies[0].ID, Profiles: map[string]string{"proof": "proof", "counterexample": "counterexample", "review": "review"}, Workspace: "source", MaxAttempts: 4, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	id := stateOf(t, s).Teams[0].ID
	v = waitTeam(t, s, id, "blocked")
	if len(v.Attempts) != 2 || len(v.Findings) != 1 || v.Entities[0].Status == "refuted" {
		t.Fatal("counterexample was treated as proven")
	}
}
func TestTeamBDD_MissingOrCorruptReportsBlockTransition(t *testing.T) {
	for _, stage := range []string{"exploring", "review"} {
		for _, corrupt := range []bool{false, true} {
			name := stage + "/missing"
			if corrupt {
				name = stage + "/corrupt"
			}
			t.Run(name, func(t *testing.T) {
				s := serviceFor(t, teamOptions(t, "http://127.0.0.1:1/v1"))
				v := studyFor(t, s)
				d := v.Data
				goal := d.Studies[0].Goal
				current := map[string]string{}
				profiles := map[string]string{}
				for _, role := range []string{"proof", "counterexample", "review"} {
					id := identifier("run")
					current[role] = id
					profiles[role] = role
					d.Attempts = append(d.Attempts, Attempt{ID: id, Profile: role, Target: goal, TargetRevision: d.entity(goal).Revision, Status: "candidate", ResultSHA256: "invalid-digest"})
					if corrupt {
						dir := filepath.Join(s.Options.Config.DataDir, "attempts", id)
						if err := os.MkdirAll(dir, 0700); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filepath.Join(dir, "result.json"), []byte("{}"), 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
				team := ResearchTeam{ID: identifier("team"), Study: d.Studies[0].ID, Goal: goal, Target: goal, TargetRevision: d.entity(goal).Revision,
					Profiles: profiles, Workspace: "source", Current: current, Status: "running", Stage: stage, MaxAttempts: 6, UsedAttempts: 3}
				if err := s.advanceTeam(&d, &team); err != nil {
					t.Fatal(err)
				}
				if team.Status != "blocked" || len(d.Attempts) != 3 || d.entity(goal).Proof != "" {
					t.Fatal("unavailable report bypassed transition")
				}
			})
		}
	}
}
