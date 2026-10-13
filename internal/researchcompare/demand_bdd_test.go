package researchcompare

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestComparisonV3BDD_DemandSkipsFormalizerUsesNewReviewAndFreshTacticBaseline(t *testing.T) {
	c := comparisonFixture(t)
	c.Version = 3
	for i := range c.Cases {
		c.Cases[i].Class = fmt.Sprintf("class-%d", i)
	}
	calls, formal := 0, 0
	r := Runner{Build: func(p execution.Profile, _ func(string) (string, bool)) (execution.Executor, error) {
		return executorFunc(func(_ context.Context, task execution.Task) (execution.Result, error) {
			calls++
			answer := `{"outcome":"proof","source":"import Goal\ntheorem Candidate : Statement := by trivial","evidence":"Direct proof"}`
			if p.ID == "counter" {
				answer = `{"outcome":"inconclusive","source":"","evidence":"Boundary"}`
			}
			if p.ID == "review" {
				answer = `{"rubric":"proof-review@2","summary":"Reviewed exact claim","findings":[]}`
			}
			if p.ID == "formalize" {
				formal++
			}
			return execution.Result{Status: "candidate", Candidate: answer, Events: []execution.Event{{Type: "model_requested"}, {Type: "model_finish", Status: "stop"}}, Usage: &execution.Usage{InputTokens: 5, OutputTokens: 3}}, nil
		}), nil
	}, Checker: checkerFunc(func(_ context.Context, g leancheck.Goal, source, _ string) (leancheck.Report, error) {
		return leancheck.Report{Status: "verified", GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(source), AuditSHA256: leancheck.AuditDigest()}, nil
	})}
	dir := filepath.Join(t.TempDir(), "v3")
	report, err := RunComparison(context.Background(), c, dir, r)
	if err != nil || report.Version != 3 || report.PolicySHA256 != DemandPolicyDigest() || len(report.Runs) != 48 || calls != 108 || formal != 12 {
		t.Fatal("incorrect routing or budgets", err, len(report.Runs), calls, formal)
	}
	for _, run := range report.Runs {
		if run.RequestUpperBound > 6 || run.Status == "running" {
			t.Fatal(run)
		}
		if run.Mode == "demand" && (len(run.Stages) != 3 || run.Profiles["formalize"] != "" || run.MathematicalStatus != "proof_verified" || !run.ReviewAccepted) {
			t.Fatal("specialist invoked without need", run)
		}
	}
	r.Resume = true
	if _, err := RunComparison(context.Background(), c, dir, r); err != nil || calls != 108 {
		t.Fatal("paid requests replayed", err, calls)
	}
	if _, err := AuditComparison(dir); err != nil {
		t.Fatal(err)
	}
}
