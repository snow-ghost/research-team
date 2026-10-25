package leancheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const chainProcedure = "lean-solve-by-elim-only@1;subsets=lexicographic;depth=1..3;trials=1..32;heartbeats=20000"
const chainTemplate = "import Goal\nimport Mathlib\nset_option maxHeartbeats 20000 in\ntheorem %s : %s := by\n  unfold %s\n  intros\n  solve_by_elim (maxDepth := %d) (constructor := false) (intro := false) only [*, %s]\n"

func ChainDigest() string { return Digest([]string{chainProcedure, chainTemplate}) }

type ChainTrial struct {
	Candidates   []string `json:"candidates"`
	Status       string   `json:"status"`
	SourceSHA256 string   `json:"source_sha256"`
	Diagnostics  string   `json:"diagnostics,omitempty"`
}

type ChainReport struct {
	Status          string       `json:"status"`
	Depth           int          `json:"depth"`
	MaxTrials       int          `json:"max_trials"`
	ProcedureSHA256 string       `json:"procedure_sha256"`
	Trials          []ChainTrial `json:"trials"`
	Source          string       `json:"source,omitempty"`
	Verification    *Report      `json:"verification,omitempty"`
}

// Trials count candidate subsets, not internal solver states. Lean also enforces
// a depth bound and a heartbeat limit, then audits the resulting proof.
func SearchChains(ctx context.Context, checker Checker, goal Goal, candidates []string, depth, maxTrials int, dir string) (ChainReport, error) {
	r := ChainReport{Status: "no_match", Depth: depth, MaxTrials: maxTrials, ProcedureSHA256: ChainDigest(), Trials: []ChainTrial{}}
	if checker == nil || depth < 1 || depth > 3 || maxTrials < 1 || maxTrials > 32 || len(candidates) < 1 || len(candidates) > 8 {
		return r, errors.New("invalid chain search bounds")
	}
	if err := goal.Validate(); err != nil {
		return r, err
	}
	seen := map[string]bool{}
	for _, name := range candidates {
		if !declaration.MatchString(name) || seen[name] {
			return r, errors.New("invalid or duplicate chain candidate")
		}
		seen[name] = true
	}
	var visit func(int, []string, int) (bool, error)
	visit = func(start int, selected []string, count int) (bool, error) {
		if len(selected) < count {
			for i := start; i < len(candidates); i++ {
				stop, err := visit(i+1, append(append([]string{}, selected...), candidates[i]), count)
				if stop || err != nil {
					return stop, err
				}
			}
			return false, nil
		}
		if ctx.Err() != nil {
			r.Status = "bounded"
			return true, ctx.Err()
		}
		if len(r.Trials) >= maxTrials {
			r.Status = "bounded"
			return true, nil
		}
		// Lean consumes one additional recursion unit to recognize an empty goal list.
		source := fmt.Sprintf(chainTemplate, goal.Candidate, goal.Declaration, goal.Declaration, depth+1, strings.Join(selected, ", "))
		checkDir := filepath.Join(dir, fmt.Sprintf("trial-%02d", len(r.Trials)+1))
		if err := os.MkdirAll(checkDir, 0700); err != nil {
			r.Status = "failed"
			return true, err
		}
		check, err := checker.Check(ctx, goal, source, checkDir)
		body, _ := json.Marshal(check)
		if saveErr := os.WriteFile(filepath.Join(checkDir, "report.json"), body, 0600); saveErr != nil {
			r.Status = "failed"
			return true, saveErr
		}
		r.Trials = append(r.Trials, ChainTrial{Candidates: append([]string{}, selected...), Status: check.Status, SourceSHA256: Digest(source), Diagnostics: preview(check.Diagnostics, 2000)})
		if err != nil && check.Phase != "compile" {
			r.Status = "failed"
			return true, err
		}
		if check.Status == "verified" {
			if check.AuditSHA256 != AuditDigest() || check.GoalSHA256 != Digest(goal) || check.SourceSHA256 != Digest(source) {
				r.Status = "failed"
				return true, errors.New("chain verification binding differs")
			}
			r.Status, r.Source, r.Verification = "verified", source, &check
			return true, nil
		}
		return false, nil
	}
	for size := 1; size <= min(depth, len(candidates)); size++ {
		stop, err := visit(0, nil, size)
		if stop || err != nil {
			return r, err
		}
	}
	return r, nil
}
