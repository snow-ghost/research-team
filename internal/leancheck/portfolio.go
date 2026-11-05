package leancheck

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

var definitionPattern = regexp.MustCompile(`(?m)^def\s+([A-Za-z_][A-Za-z0-9_.]*)`)

func decisionDefinitions(g Goal) string {
	names := []string{g.Declaration}
	seen := map[string]bool{g.Declaration: true}
	for _, match := range definitionPattern.FindAllStringSubmatch(g.Source, -1) {
		name := match[1]
		if seen[name] || strings.HasSuffix(g.Declaration, "."+name) {
			continue
		}
		seen[name] = true
		names = append(names, name)
		if len(names) == 16 {
			break
		}
	}
	return strings.Join(names, " ")
}

func TacticTrials(g Goal) ([]TacticTrial, error) {
	template, err := CandidateTemplate(g)
	if err != nil {
		return nil, err
	}
	out := []TacticTrial{}
	for _, tactic := range []string{"simp_all", "omega", "ring", "norm_num", "decide", "aesop"} {
		// Each tactic has a fresh proof state. Partial simplification cannot mask later trials.
		body := "  unfold " + g.Declaration + "\n  intros\n  " + tactic + "\n"
		if tactic == "decide" {
			body = "  unfold " + decisionDefinitions(g) + "\n  decide\n"
		}
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
