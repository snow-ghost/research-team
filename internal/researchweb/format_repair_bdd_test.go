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

func TestFormatRepairBDD_OptInBoundedRepairKeepsOriginalAndAuthorship(t *testing.T) {
	for _, tc := range []struct {
		name       string
		limit      int
		alwaysBad  bool
		want       string
		proofCalls int32
	}{{"disabled", 0, false, "paused", 1}, {"repaired", 1, false, "awaiting_review", 2}, {"exhausted", 1, true, "paused", 2}} {
		t.Run(tc.name, func(t *testing.T) {
			var proofs atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request any
				_ = json.NewDecoder(r.Body).Decode(&request)
				data, _ := json.Marshal(request)
				text := string(data)
				answer := "```lean\n" + submittedProof + "```"
				if strings.Contains(text, "ROLE_proof") {
					n := proofs.Add(1)
					if n == 1 || tc.alwaysBad {
						answer = "Original answer without the required source"
					}
					if n == 2 && !strings.Contains(text, "format_repair") {
						t.Error("repair has no original response or diagnostic")
					}
				}
				if strings.Contains(text, "ROLE_counterexample") {
					answer = `{"outcome":"none_found","evidence":"Bounded cases only"}`
				}
				if strings.Contains(text, "ROLE_review") {
					answer = `{"rubric":"proof-review@2","summary":"Exact proof reviewed","findings":[]}`
				}
				completeModel(w, answer)
			}))
			defer provider.Close()
			o := teamOptions(t, provider.URL+"/v1")
			p := o.Profiles["proof"]
			p.Skills[0].Contract = &execution.SkillContract{ResultChecks: []string{"lean_source"}}
			o.Profiles["proof"] = p
			o.Checker = checkerFunc(func(_ context.Context, g leancheck.Goal, source, _ string) (leancheck.Report, error) {
				return leancheck.Report{Status: "verified", AuditSHA256: leancheck.AuditDigest(), GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(source)}, nil
			})
			s := serviceFor(t, o)
			v := studyFor(t, s)
			if err := s.SetFormalGoal(v.Studies[0].Goal, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}}); err != nil {
				t.Fatal(err)
			}
			v = stateOf(t, s)
			if err := s.StartTeam(TeamRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: v.Studies[0].ID, Strategy: "demand", Profiles: map[string]string{"proof": "proof", "formalize": "formalize", "counterexample": "counterexample", "review": "review"}, Workspace: "source", MaxAttempts: 6, FormatRepairLimit: tc.limit, RequireLean: true, Confirm: true}); err != nil {
				t.Fatal(err)
			}
			v = waitTeam(t, s, stateOf(t, s).Teams[0].ID, tc.want)
			if proofs.Load() != tc.proofCalls || v.Teams[0].FormatRepairsUsed != int(tc.proofCalls)-1 {
				t.Fatal(v.Teams, proofs.Load())
			}
			first := v.Attempts[0]
			r, err := s.readResult(first)
			if err != nil || r.Candidate != "Original answer without the required source" || r.Usage == nil || r.Status != "failed" {
				t.Fatal(r, err)
			}
			if tc.want == "awaiting_review" {
				proof := v.attempt(v.Teams[0].Current["proof"])
				review := v.attempt(v.Teams[0].Current["review"])
				if proof.ParentAttempt != first.ID || review.ReviewOf != proof.ID || v.entity(proof.Target).Status == "accepted" {
					t.Fatal("repair changed provenance or accepted the claim")
				}
			}
		})
	}
}

func TestFormatRepairBDD_UnknownCancelledAndTransportFailuresCannotBeReplayed(t *testing.T) {
	good := execution.Result{Status: "failed", PrecheckStatus: "candidate", FailureCode: "skill_result_contract", RemoteOutcome: "response_received", SkillChecks: []execution.SkillResultCheck{{Check: "lean_source", Passed: false}}}
	if !repairableSkillFailure(good) {
		t.Fatal("known structural error rejected")
	}
	for _, change := range []func(*execution.Result){func(r *execution.Result) { r.RemoteOutcome = "unknown" }, func(r *execution.Result) { r.Status = "cancelled" }, func(r *execution.Result) { r.FailureCode = "http_error" }, func(r *execution.Result) { r.PrecheckStatus = "failed" }, func(r *execution.Result) { r.SkillChecks = nil }} {
		r := good
		change(&r)
		if repairableSkillFailure(r) {
			t.Fatal("unsafe retry", r)
		}
	}
}
