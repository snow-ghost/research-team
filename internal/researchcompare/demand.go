package researchcompare

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
	"github.com/snow-ghost/research-team/internal/proofreview"
)

func DemandPolicyDigest() string {
	return leancheck.Digest(struct {
		Version                         int
		Modes                           []string
		Rubric, Instructions, Portfolio string
		Requests, Seconds               int
	}{3, []string{"single", "team", "demand", "lean"}, proofreview.Rubric, proofreview.Instructions, leancheck.PortfolioVersion, 6, 900})
}

type StageObservation struct {
	ProviderHTTPStatus int              `json:"provider_http_status,omitempty"`
	Role               string           `json:"role"`
	Status             string           `json:"status"`
	FailureCode        string           `json:"failure_code,omitempty"`
	FinishReason       string           `json:"finish_reason,omitempty"`
	LimitReason        string           `json:"limit_reason,omitempty"`
	ProfileSHA256      string           `json:"profile_sha256"`
	ResultSHA256       string           `json:"result_sha256"`
	Requests           int              `json:"requests"`
	Seconds            float64          `json:"seconds"`
	Usage              *execution.Usage `json:"usage,omitempty"`
}

func observeStage(role string, r execution.Result, started time.Time, p execution.Profile) StageObservation {
	s := StageObservation{Role: role, Status: r.Status, FailureCode: r.FailureCode, ProfileSHA256: leancheck.Digest(p), ResultSHA256: leancheck.Digest(r), Seconds: time.Since(started).Seconds(), Usage: r.Usage}
	for _, e := range r.Events {
		switch e.Type {
		case "model_requested":
			s.Requests++
		case "model_finish":
			s.FinishReason = e.Status
		case "limit_reason":
			s.LimitReason = e.Status
		}
	}
	for _, d := range r.Diagnostics {
		if d.Source == "model" && d.Kind == "http_error" {
			var data struct {
				Status int `json:"http_status"`
			}
			if json.Unmarshal(d.Data, &data) == nil {
				s.ProviderHTTPStatus = data.Status
			}
		}
	}
	return s
}

func (r Runner) portfolioBaseline(ctx context.Context, item Case, dir string, run *Run) error {
	start := time.Now()
	defer func() { run.Seconds = time.Since(start).Seconds() }()
	if err := os.Mkdir(dir, 0700); err != nil {
		return err
	}
	for _, outcome := range []string{"proof", "refutation"} {
		goal := item.Goal
		if outcome == "refutation" {
			goal = negative(goal)
		}
		results, err := leancheck.TryTactics(ctx, r.Checker, goal, filepath.Join(dir, outcome))
		for _, result := range results {
			if result.Report.Status == "verified" && result.Report.AuditSHA256 == leancheck.AuditDigest() {
				report := result.Report
				run.Verification = &report
				run.Status = "verified_baseline_not_accepted"
				run.Outcome = outcome
				run.MathematicalStatus = outcome + "_verified"
				return nil
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			continue
		}
	}
	run.Status = "inconclusive"
	run.MathematicalStatus = "inconclusive"
	return nil
}
