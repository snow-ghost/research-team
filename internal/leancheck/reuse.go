package leancheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const reuseTemplate = "import Goal\nimport Mathlib\nset_option maxHeartbeats 20000 in\ntheorem %s : %s := by\n  unfold %s\n  intros\n  solve_by_elim (maxDepth := %d) (constructor := false) (intro := false) only [%s]\n"

func ReuseDigest() string {
	return Digest([]string{"paired-native-reuse@1;two-arms;depth=1..6;independent-timeouts;no-model;no-acceptance", reuseTemplate, AuditDigest(), DependencyDigest()})
}

type ReuseArm struct {
	Status  string  `json:"status"`
	Seconds float64 `json:"seconds"`
	Source  string  `json:"source"`
	Goal    Goal    `json:"goal"`
	Report  Report  `json:"report"`
}
type ReuseReport struct {
	ProcedureSHA256    string   `json:"procedure_sha256"`
	OriginalGoalSHA256 string   `json:"original_goal_sha256"`
	Depth              int      `json:"depth"`
	TimeoutSeconds     int      `json:"timeout_seconds"`
	Without            ReuseArm `json:"without"`
	With               ReuseArm `json:"with"`
	ModelCalls         int      `json:"model_calls"`
	Accepted           bool     `json:"accepted"`
}

// Both arms run even if one fails. The comparison concerns this fixed bounded
// procedure, not the existence of alternative proofs or agent performance.
func CompareReuse(ctx context.Context, checker Checker, goal Goal, module, candidate string, depth, seconds int, dir string) (ReuseReport, error) {
	r := ReuseReport{ProcedureSHA256: ReuseDigest(), OriginalGoalSHA256: Digest(goal), Depth: depth, TimeoutSeconds: seconds}
	if checker == nil || goal.Validate() != nil || !declaration.MatchString(module) || !strings.HasPrefix(module, "ResearchLemma_") || !declaration.MatchString(candidate) || depth < 1 || depth > 6 || seconds < 1 || seconds > 120 || strings.Contains(goal.Source, "ResearchLemma_") {
		return r, errors.New("reuse comparison needs an independent goal, one module and bounded search")
	}
	for _, arm := range []struct {
		name, hints string
		dest        *ReuseArm
	}{{"without", "*", &r.Without}, {"with", "*, " + candidate, &r.With}} {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		g := goal
		if arm.name == "with" {
			g.Source = "import " + module + "\n" + g.Source
		}
		source := fmt.Sprintf(reuseTemplate, g.Candidate, g.Declaration, g.Declaration, depth+1, arm.hints)
		work := filepath.Join(dir, arm.name)
		if err := os.Mkdir(work, 0700); err != nil {
			return r, err
		}
		limited, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
		start := time.Now()
		report, err := checker.Check(limited, g, source, work)
		cancel()
		x := ReuseArm{Status: "not_proved", Seconds: time.Since(start).Seconds(), Goal: g, Source: source, Report: report}
		if report.Status == "verified" {
			if report.AuditSHA256 != AuditDigest() || report.GoalSHA256 != Digest(g) || report.SourceSHA256 != Digest(source) {
				return r, errors.New("reuse verification binding differs")
			}
			x.Status = "verified"
		} else if err != nil && report.Phase != "compile" {
			x.Status = "failed"
		}
		*arm.dest = x
		body, _ := json.MarshalIndent(x, "", "  ")
		if err := os.WriteFile(filepath.Join(work, "arm.json"), body, 0600); err != nil {
			return r, err
		}
	}
	return r, nil
}
