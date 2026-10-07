package researchcompare

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
)

type executorFunc func(context.Context, execution.Task) (execution.Result, error)

func (f executorFunc) Run(ctx context.Context, t execution.Task) (execution.Result, error) {
	return f(ctx, t)
}

type checkerFunc func(context.Context, leancheck.Goal, string, string) (leancheck.Report, error)

func (f checkerFunc) Check(ctx context.Context, g leancheck.Goal, s, dir string) (leancheck.Report, error) {
	return f(ctx, g, s, dir)
}

func comparisonFixture(t *testing.T) Config {
	t.Helper()
	makeProfile := func(id string, steps int) execution.Profile {
		return execution.Profile{ID: id, Kind: "model", Limits: execution.Limits{TimeoutSeconds: 900, MaxSteps: steps, MaxOutputTokens: 32768, MaxOutputBytes: 16 << 20}, Model: &execution.ModelConfig{Protocol: "chat_completions", BaseURL: "https://example.com/v1", Model: "same-model"}}
	}
	c := Config{Universal: makeProfile("universal", 5), Proof: makeProfile("proof", 1), Counter: makeProfile("counter", 1), Formalize: makeProfile("formalize", 3), Reviewer: makeProfile("review", 1), Lean: leancheck.Config{Runtime: "/usr/bin/false", Image: "sha256:" + strings.Repeat("a", 64), Toolchain: t.TempDir(), ExpectedVersion: "fixture", TimeoutSeconds: 60, MemoryMB: 512}}
	for _, id := range []string{"one", "two", "three", "four", "five", "six"} {
		c.Cases = append(c.Cases, Case{ID: id, Goal: leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}})
	}
	return c
}

func TestComparisonBDD_EqualBudgetsNoReplayAndNativeAcceptance(t *testing.T) {
	c := comparisonFixture(t)
	directory := filepath.Join(t.TempDir(), "run")
	requests := 0
	runner := Runner{Build: func(p execution.Profile, _ func(string) (string, bool)) (execution.Executor, error) {
		return executorFunc(func(_ context.Context, task execution.Task) (execution.Result, error) {
			var stored Report
			body, err := os.ReadFile(filepath.Join(directory, "report.json"))
			if err != nil || json.Unmarshal(body, &stored) != nil || stored.Runs[len(stored.Runs)-1].RequestUpperBound < 1 {
				t.Fatal("request started before reservation")
			}
			requests++
			candidate := `{"outcome":"proof","source":"import Goal\ntheorem Candidate : Statement := by trivial","evidence":"Exact proposition"}`
			if p.ID == "proof" {
				candidate = "Direct proof"
			}
			if p.ID == "counter" {
				candidate = `{"outcome":"inconclusive","source":"","evidence":"No finite counterexample"}`
			}
			if p.ID == "review" {
				candidate = `{"summary":"Source and goal match","findings":[]}`
			}
			return execution.Result{TaskID: task.ID, AttemptID: task.AttemptID, ProfileID: p.ID, Snapshot: task.Snapshot, LeaseEpoch: 1, Status: "candidate", Candidate: candidate, Usage: &execution.Usage{InputTokens: 5, OutputTokens: 3}, Events: []execution.Event{{Type: "model_requested"}}}, nil
		}), nil
	}, Checker: checkerFunc(func(_ context.Context, g leancheck.Goal, s, _ string) (leancheck.Report, error) {
		return leancheck.Report{Status: "verified", GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(s)}, nil
	})}
	report, err := RunComparison(context.Background(), c, directory, runner)
	if err != nil || len(report.Runs) != 12 || requests != 36 {
		t.Fatal(report, err, requests)
	}
	for _, run := range report.Runs {
		if run.Status != "verified_not_accepted" || run.RequestUpperBound != 6 || run.UnknownRequestCount || run.UsageIncomplete {
			t.Fatal(run)
		}
	}
	if _, err := RunComparison(context.Background(), c, directory, runner); err == nil || requests != 36 {
		t.Fatal("prior calls replayed")
	}
}

func TestComparisonBDD_MismatchedModelOrBudgetRejected(t *testing.T) {
	for _, change := range []func(*Config){func(c *Config) { c.Proof.Model.Model = "other" }, func(c *Config) { c.Universal.Limits.MaxSteps = 7 }, func(c *Config) { c.Cases[1].ID = c.Cases[0].ID }} {
		c := comparisonFixture(t)
		change(&c)
		if c.Validate() == nil {
			t.Fatal("unfair comparison accepted")
		}
	}
}

func TestComparisonBDD_ResumeNeverReplaysInterruptedRun(t *testing.T) {
	c := comparisonFixture(t)
	directory := t.TempDir()
	report := Report{Version: 1, ConfigurationSHA256: leancheck.Digest(c), MaxRequests: 96, Runs: []Run{{Case: c.Cases[0].ID, Mode: "single", GoalSHA256: leancheck.Digest(c.Cases[0].Goal), Status: "verified_not_accepted", RequestUpperBound: 6}, {Case: c.Cases[0].ID, Mode: "team", GoalSHA256: leancheck.Digest(c.Cases[0].Goal), Status: "running", RequestUpperBound: 2}}}
	body, _ := json.Marshal(report)
	if err := os.WriteFile(filepath.Join(directory, "report.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	called := 0
	r := Runner{Resume: true, Build: func(p execution.Profile, _ func(string) (string, bool)) (execution.Executor, error) {
		return executorFunc(func(_ context.Context, task execution.Task) (execution.Result, error) {
			if strings.HasPrefix(task.ID, c.Cases[0].ID+"-") {
				t.Fatal("interrupted request replayed")
			}
			called++
			candidate := `{"outcome":"inconclusive","source":"","evidence":"No conclusion"}`
			return execution.Result{Status: "candidate", Candidate: candidate, Events: []execution.Event{{Type: "model_requested"}}}, nil
		}), nil
	}}
	actual, err := RunComparison(context.Background(), c, directory, r)
	if err != nil || len(actual.Runs) != 12 || called != 20 {
		t.Fatal(actual, err, called)
	}
	if actual.Runs[1].Status != "failed" || actual.Runs[1].RequestUpperBound != 2 || !actual.Runs[1].UsageIncomplete || !actual.Runs[1].TimeIncomplete || len(actual.ResumedAt) != 1 {
		t.Fatal("unknown reserve lost", actual.Runs[1])
	}
	c.Universal.Model.Model = "different"
	if _, err := RunComparison(context.Background(), c, directory, r); err == nil {
		t.Fatal("changed configuration resumed")
	}
}
