package researchcompare

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestControlledBDD_ThreeModesTwoRepetitionsAndNoModelBaseline(t *testing.T) {
	c := comparisonFixture(t)
	c.Version = 2
	for i := range c.Cases {
		c.Cases[i].Class = fmt.Sprintf("class-%d", i)
	}
	calls := 0
	runner := Runner{Build: func(p execution.Profile, _ func(string) (string, bool)) (execution.Executor, error) {
		return executorFunc(func(_ context.Context, task execution.Task) (execution.Result, error) {
			calls++
			if !strings.Contains(task.Context, "candidate_templates") {
				t.Error("trusted templates missing")
			}
			answer := `{"outcome":"proof","source":"import Goal\ntheorem Candidate : Statement := by trivial","evidence":"Direct proof"}`
			if p.ID == "proof" {
				answer = "Mathematical proof"
			}
			if p.ID == "counter" {
				answer = `{"outcome":"inconclusive","source":"","evidence":"Checked boundary"}`
			}
			if p.ID == "review" {
				answer = `{"summary":"Reviewed","findings":[]}`
			}
			return execution.Result{Candidate: answer, Events: []execution.Event{{Type: "model_requested"}}, Usage: &execution.Usage{InputTokens: 5, OutputTokens: 3}}, nil
		}), nil
	}, Checker: checkerFunc(func(_ context.Context, g leancheck.Goal, source, _ string) (leancheck.Report, error) {
		return leancheck.Report{Status: "verified", GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(source), AuditSHA256: leancheck.AuditDigest()}, nil
	})}
	dir := filepath.Join(t.TempDir(), "controlled")
	report, err := RunComparison(context.Background(), c, dir, runner)
	if err != nil || len(report.Runs) != 48 || calls != 120 {
		t.Fatal(err, len(report.Runs), calls)
	}
	for _, run := range report.Runs {
		if run.RequestUpperBound > 6 || run.Seconds < 0 || run.Repetition < 1 || run.Repetition > 2 || (run.Mode == "lean" && run.RequestUpperBound != 0) {
			t.Fatal(run)
		}
	}
	runner.Resume = true
	if _, err := RunComparison(context.Background(), c, dir, runner); err != nil || calls != 120 {
		t.Fatal("completed requests replayed", err, calls)
	}
}
