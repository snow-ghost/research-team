package researchcompare

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
)

type Rehearsal struct {
	Simulation               bool              `json:"simulation"`
	ModelCalls               int               `json:"model_calls"`
	MathematicalVerification bool              `json:"mathematical_verification"`
	SimulatedRequests        int               `json:"simulated_requests"`
	Summary                  ComparisonSummary `json:"summary"`
}

type rehearsalExecutor struct{ reviewer bool }

func (e rehearsalExecutor) Run(_ context.Context, t execution.Task) (execution.Result, error) {
	answer := `{"outcome":"proof","source":"import Goal\ntheorem Candidate : Statement := by sorry\n","evidence":"SIMULATION ONLY"}`
	if e.reviewer {
		answer = `{"rubric":"proof-review@2","summary":"SIMULATED REVIEW, NOT EVIDENCE","findings":[]}`
	}
	return execution.Result{Status: "candidate", Candidate: answer, Events: []execution.Event{{Type: "model_requested"}, {Type: "model_finish", Status: "stop"}}, Usage: &execution.Usage{Source: "simulation", InputTokens: 1, OutputTokens: 1}}, nil
}

type rehearsalChecker struct{}

func (rehearsalChecker) Check(_ context.Context, g leancheck.Goal, source, _ string) (leancheck.Report, error) {
	return leancheck.Report{Status: "verified", Phase: "complete", GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(source), AuditSHA256: leancheck.AuditDigest(), Diagnostics: "SIMULATED CHECK, NO LEAN INVOCATION"}, nil
}

// RehearseEvaluation runs the same scheduling, persistence and resume path with
// explicit substitutes. Its report cannot be imported as a live comparison.
func RehearseEvaluation(ctx context.Context, set EvaluationSet, dir string, resume bool) (Rehearsal, error) {
	out := Rehearsal{Simulation: true}
	if _, err := PrepareEvaluation(set); err != nil {
		return out, err
	}
	profile := func(id string, steps int) execution.Profile {
		return execution.Profile{ID: id, Kind: "model", Model: &execution.ModelConfig{Protocol: "chat_completions", BaseURL: "http://127.0.0.1:1/v1", Model: "simulation", AllowLoopbackHTTP: true}, Limits: execution.Limits{TimeoutSeconds: 900, MaxSteps: steps, MaxOutputTokens: 32768, MaxOutputBytes: 16 << 20}}
	}
	c := Config{Version: 4, Cases: set.Cases, Universal: profile("universal", 5), Proof: profile("proof", 1), Counter: profile("counter", 1), Formalize: profile("formalize", 3), Reviewer: profile("review", 1), Lean: leancheck.Config{Runtime: "/disabled", Toolchain: os.TempDir(), Image: "sha256:" + strings.Repeat("0", 64), ExpectedVersion: "simulation", TimeoutSeconds: 1, MemoryMB: 256}}
	calls := 0
	runner := Runner{Simulation: true, Resume: resume, Lookup: func(string) (string, bool) { return "", false }, Checker: rehearsalChecker{}, Build: func(p execution.Profile, _ func(string) (string, bool)) (execution.Executor, error) {
		return rehearsalCount{next: rehearsalExecutor{reviewer: p.ID == "review"}, count: &calls}, nil
	}}
	report, err := RunComparison(ctx, c, dir, runner)
	if err != nil {
		return out, err
	}
	out.SimulatedRequests = calls
	out.Summary, err = Summarize(report)
	if err != nil {
		return out, err
	}
	body, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return out, err
	}
	return out, os.WriteFile(dir+string(os.PathSeparator)+"rehearsal.json", body, 0600)
}

type rehearsalCount struct {
	next  execution.Executor
	count *int
}

func (r rehearsalCount) Run(ctx context.Context, t execution.Task) (execution.Result, error) {
	*r.count++
	return r.next.Run(ctx, t)
}
