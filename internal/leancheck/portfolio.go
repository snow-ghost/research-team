package leancheck

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const PortfolioVersion = "lean-tactics@1"

type TacticTrial struct {
	ID     string `json:"id"`
	Source string `json:"source"`
}

type TacticResult struct {
	Trial  TacticTrial `json:"trial"`
	Report Report      `json:"report"`
}

func TacticTrials(g Goal) ([]TacticTrial, error) {
	template, err := CandidateTemplate(g)
	if err != nil {
		return nil, err
	}
	out := []TacticTrial{}
	for _, tactic := range []string{"simp_all", "omega", "ring", "norm_num", "aesop"} {
		// Each tactic has a fresh proof state. Partial simplification cannot mask later trials.
		body := "  unfold " + g.Declaration + "\n  intros\n  " + tactic + "\n"
		out = append(out, TacticTrial{ID: tactic, Source: template + body})
	}
	return out, nil
}

func TryTactics(ctx context.Context, checker Checker, g Goal, dir string) ([]TacticResult, error) {
	trials, err := TacticTrials(g)
	if err != nil {
		return nil, err
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		return nil, err
	}
	results := []TacticResult{}
	for _, trial := range trials {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		work := filepath.Join(dir, trial.ID)
		if err := os.Mkdir(work, 0700); err != nil {
			return results, err
		}
		limited, cancel := context.WithTimeout(ctx, 60*time.Second)
		report, checkErr := checker.Check(limited, g, trial.Source, work)
		cancel()
		if report.Status == "verified" && (report.GoalSHA256 != Digest(g) || report.SourceSHA256 != Digest(trial.Source) || report.AuditSHA256 != AuditDigest()) {
			report.Status, report.Phase = "failed", "report_integrity"
		}
		result := TacticResult{Trial: trial, Report: report}
		body, _ := json.Marshal(struct {
			Version string       `json:"procedure"`
			Result  TacticResult `json:"result"`
		}{PortfolioVersion, result})
		if err := os.WriteFile(filepath.Join(work, "trial.json"), body, 0600); err != nil {
			return results, err
		}
		results = append(results, result)
		if checkErr == nil && report.Status == "verified" {
			return results, nil
		}
	}
	return results, fmt.Errorf("no tactic closed the exact goal")
}
