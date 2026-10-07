//go:build linux

package researchweb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

const falseGoal = "import Mathlib\nnamespace NegativeTrial\ndef Statement : Prop := ∀ n : ℕ, n * n = n\nend NegativeTrial\n"
const negativeProof = "import Goal\nnamespace ResearchRefutation\ntheorem candidate : Statement := by\n  intro h\n  have h2 := h 2\n  norm_num at h2\nend ResearchRefutation\n"

func TestRefutationBDD_LegacyAuditCannotAuthorizeAcceptance(t *testing.T) {
	s, goal := submittedService(t, "sqlite", "verified")
	id := identifier("verify")
	if err := s.Store.Change(0, "", "", "Historical test report", id, "checker", func(d *Data) error {
		e := d.entity(goal)
		g := refutationGoal(*e.FormalGoal)
		d.Verifications = append(d.Verifications, Verification{ID: id, Target: goal, TargetRevision: e.Revision, Purpose: "refutation", OriginalGoalSHA256: leancheck.Digest(*e.FormalGoal), Goal: g, Source: "legacy source", Status: "verified", Report: &leancheck.Report{Status: "verified", GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest("legacy source")}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	v := stateOf(t, s)
	err := s.AcceptRefutation(id, RefutationAcceptanceRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Confirm: true, Note: "Reviewed"})
	if err == nil || !strings.Contains(err.Error(), "актуальной программой аудита") {
		t.Fatal("legacy audit authorized refutation acceptance", err)
	}
	after := stateOf(t, s)
	if after.entity(goal).Status == "refuted" {
		t.Fatal("goal refuted by legacy audit")
	}
}

func TestRefutationBDD_NegativeProofReviewAcceptanceAndMemory(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var b struct{ Messages []struct{ Content string } }
				json.NewDecoder(r.Body).Decode(&b)
				text := ""
				for _, m := range b.Messages {
					text += m.Content
				}
				if strings.Contains(text, "ROLE_counterexample") {
					candidate, _ := json.Marshal(CounterReport{Outcome: "counterexample_candidate", Evidence: "n=2: 4 is not 2", RefutationSource: negativeProof})
					completeModel(w, string(candidate))
				} else if strings.Contains(text, "ROLE_review") {
					if !strings.Contains(text, "refutation") || !strings.Contains(text, "source_sha256") {
						t.Error("missing negative review binding")
					}
					completeModel(w, `{"summary":"Checked n=2, the negation and all assumptions","findings":[{"severity":"question","text":"Independently inspect artifact hashes"}]}`)
				} else {
					completeModel(w, "No universal proof: n=2.")
				}
			}))
			defer model.Close()
			o := teamOptions(t, model.URL+"/v1")
			o.Checker = checkerFunc(func(_ context.Context, g leancheck.Goal, source, _ string) (leancheck.Report, error) {
				return leancheck.Report{AuditSHA256: leancheck.AuditDigest(), Status: "verified", GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(source)}, nil
			})
			if file := os.Getenv("RESEARCH_TEST_LEAN_CONFIG"); file != "" {
				var c leancheck.Config
				if err := ReadJSON(file, &c); err != nil {
					t.Fatal(err)
				}
				o.Lean, o.Checker = &c, leancheck.DockerChecker{Config: c}
			}
			if backend == "postgres" {
				dsn, lookup := postgresDSN(t), o.Lookup
				o.Config.Database = DatabaseConfig{Driver: "postgres", DSNEnv: "TEST_DB"}
				o.Lookup = func(k string) (string, bool) {
					if k == "TEST_DB" {
						return dsn, true
					}
					return lookup(k)
				}
			}
			s := serviceFor(t, o)
			v := studyFor(t, s)
			goal := v.Studies[0].Goal
			if err := s.SetFormalGoal(goal, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: leancheck.Goal{Source: falseGoal, Declaration: "NegativeTrial.Statement", Candidate: "NegativeTrial.candidate"}}); err != nil {
				t.Fatal(err)
			}
			v = stateOf(t, s)
			if err := s.StartTeam(TeamRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: v.Studies[0].ID, Workspace: "source", MaxAttempts: 6, RequireLean: true, Confirm: true, Profiles: map[string]string{"proof": "proof", "counterexample": "counterexample", "formalize": "formalize", "review": "review"}}); err != nil {
				t.Fatal(err)
			}
			id := stateOf(t, s).Teams[0].ID
			v = waitTeam(t, s, id, "awaiting_review")
			verification := v.team(id).Verification
			if v.team(id).Stage != "refutation_acceptance" || v.entity(goal).Status != "in_review" || hasVerifiedProof(&v.Data, v.entity(goal)) {
				t.Fatal("negative proof bypassed acceptance", v.Teams)
			}
			if err := s.AttachVerification(verification, AttachVerificationRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd")}); err == nil {
				t.Fatal("negative proof attached as positive")
			}
			r := RefutationAcceptanceRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Confirm: true, Note: "Проверены n=2, отрицание и отдельная рецензия."}
			if err := s.AcceptRefutation(verification, r); err == nil {
				t.Fatal("open review question bypassed")
			}
			finding := v.Findings[0].ID
			act(t, s, Action{Type: "RESOLVE_FINDING", Finding: finding})
			v = act(t, s, Action{Type: "CLOSE_FINDING", Finding: finding, Text: "Checked the test artifact and its binding."})
			r.ExpectedRevision = v.Revision
			r.RequestID = identifier("cmd")
			if err := s.AcceptRefutation(verification, r); err != nil {
				t.Fatal(err)
			}
			if err := s.AcceptRefutation(verification, r); err != nil {
				t.Fatal("idempotency", err)
			}
			v = waitTeam(t, s, id, "completed")
			if v.entity(goal).Status != "refuted" || hasVerifiedProof(&v.Data, v.entity(goal)) {
				t.Fatal("incorrect decision")
			}
			if err := s.AddMemory(MemoryRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Target: goal, Kind: "counterexample", Content: "n=2: 4 != 2", Conditions: "Natural numbers", Verification: verification}); err != nil {
				t.Fatal(err)
			}
			hits, err := s.SearchMemory("n=2", v.Studies[0].ID, false)
			if err != nil || len(hits) != 1 || hits[0].Trust != "verified_refutation" {
				t.Fatal(hits, err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s = serviceFor(t, o)
			hits, err = s.SearchMemory("n=2", v.Studies[0].ID, false)
			if err != nil || len(hits) != 1 || hits[0].Trust != "verified_refutation" {
				t.Fatal("restart lost trusted refutation", hits, err)
			}
		})
	}
}

func TestRefutationBDD_FailedStaleAndUnreviewedNeverAccepted(t *testing.T) {
	for _, outcome := range []string{"failed", "stale", "unreviewed"} {
		t.Run(outcome, func(t *testing.T) {
			status := "verified"
			if outcome == "failed" {
				status = "failed"
			}
			s, target := submittedService(t, "sqlite", status)
			v := stateOf(t, s)
			if err := s.SubmitRefutation(target, ProofSourceRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Source: negativeProof, Author: "Codex", Basis: "Negative candidate"}); err != nil {
				t.Fatal(err)
			}
			id := stateOf(t, s).Verifications[0].ID
			v = waitVerification(t, s, id)
			if outcome == "stale" {
				g := *v.entity(target).FormalGoal
				g.Source += "\n-- changed goal"
				if err := s.SetFormalGoal(target, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: g}); err != nil {
					t.Fatal(err)
				}
				v = stateOf(t, s)
			}
			if err := s.AcceptRefutation(id, RefutationAcceptanceRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Confirm: true, Note: "Reject incomplete evidence"}); err == nil {
				t.Fatal("invalid refutation accepted")
			}
			if err := s.AddMemory(MemoryRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Target: target, Kind: "counterexample", Content: "Unchecked candidate", Verification: id}); err == nil {
				t.Fatal("unchecked counterexample trusted")
			}
		})
	}
}
