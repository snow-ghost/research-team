package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
	"github.com/snow-ghost/research-team/internal/researchweb"
)

type nativeConfig struct {
	Goal       leancheck.Goal
	Portfolio  bool
	Source     string
	Outcome    string
	Evidence   string
	TaskID     string
	AttemptID  string
	Snapshot   string
	LeaseEpoch uint64
	ProfileID  string
}

type nativeOutput struct {
	execution.Result
	Verification leancheck.Report `json:"verification"`
}

type tolerantNativeChecker struct{ checker leancheck.Checker }

func (c tolerantNativeChecker) Check(ctx context.Context, goal leancheck.Goal, source, dir string) (leancheck.Report, error) {
	report, _ := c.checker.Check(ctx, goal, source, dir)
	return report, nil
}

func runNative(ctx context.Context, checker leancheck.Checker, config nativeConfig, dir string) (nativeOutput, error) {
	if config.Portfolio {
		trials, err := leancheck.TacticTrials(config.Goal)
		if err != nil {
			return nativeOutput{}, err
		}
		var result nativeOutput
		for i, trial := range trials {
			work := filepath.Join(dir, fmt.Sprintf("trial-%d", i))
			if err := os.Mkdir(work, 0700); err != nil {
				return result, err
			}
			trialConfig := config
			trialConfig.Portfolio = false
			trialConfig.Source = trial.Source
			result, err = runNative(ctx, tolerantNativeChecker{checker}, trialConfig, work)
			if err == nil {
				return result, nil
			}
		}
		return result, errors.New("no native tactic closed the exact goal")
	}
	result := nativeOutput{Result: execution.Result{
		TaskID: config.TaskID, AttemptID: config.AttemptID, Snapshot: config.Snapshot,
		LeaseEpoch: config.LeaseEpoch, ProfileID: config.ProfileID, Status: "failed",
		RemoteOutcome: "not_started", Usage: &execution.Usage{Source: "native"}, Events: []execution.Event{},
	}}
	candidate, err := json.Marshal(map[string]string{
		"outcome": config.Outcome, "source": config.Source, "evidence": config.Evidence,
	})
	if err != nil {
		return result, err
	}
	result.Candidate = string(candidate)
	result.Verification, err = checker.Check(ctx, config.Goal, config.Source, dir)
	result.Events = append(result.Events, execution.Event{Type: "native_check", Status: result.Verification.Status})
	if err != nil {
		return result, err
	}
	if result.Verification.Status != "verified" || result.Verification.GoalSHA256 != leancheck.Digest(config.Goal) || result.Verification.SourceSHA256 != leancheck.Digest(config.Source) || result.Verification.AuditSHA256 != leancheck.AuditDigest() {
		result.Diagnostics = append(result.Diagnostics, execution.Diagnostic{Source: "native", Stage: "verification", Kind: "native_candidate_rejected", Message: "candidate did not pass the pinned Lean verification"})
		return result, errors.New("native candidate was not verified")
	}
	result.Status = "candidate"
	return result, nil
}

func readSource(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil || len(body) == 0 || len(body) > 65536 {
		return "", errors.New("Lean source is unavailable or exceeds 65536 bytes")
	}
	return string(body), nil
}

func main() {
	goalPath := flag.String("goal", "", "Goal.lean")
	candidatePath := flag.String("candidate", "", "Candidate.lean")
	portfolio := flag.Bool("portfolio", false, "Try the native tactic portfolio instead of a fixed candidate")
	leanConfig := flag.String("lean-config", "", "Pinned Lean checker configuration")
	declaration := flag.String("declaration", "", "Goal declaration name")
	candidateName := flag.String("candidate-name", "", "Candidate declaration name")
	outcome := flag.String("outcome", "proof", "proof or refutation")
	evidence := flag.String("evidence", "Native Lean verification without a model call", "Short evidence text")
	taskID := flag.String("task-id", "native-task", "Result task identifier")
	attemptID := flag.String("attempt-id", "native-attempt", "Result attempt identifier")
	flag.Parse()
	if *goalPath == "" || *leanConfig == "" || *declaration == "" || *candidateName == "" || (*outcome != "proof" && *outcome != "refutation") || (*portfolio && *candidatePath != "") || (!*portfolio && *candidatePath == "") {
		fmt.Fprintln(os.Stderr, "Pass -goal, -lean-config, -declaration, -candidate-name, a valid -outcome and either -candidate or -portfolio.")
		os.Exit(2)
	}
	goalSource, err := readSource(*goalPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	candidateSource := ""
	if !*portfolio {
		candidateSource, err = readSource(*candidatePath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	var lean leancheck.Config
	if err := researchweb.ReadJSON(*leanConfig, &lean); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	work, err := os.MkdirTemp("", "research-native-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(work)
	config := nativeConfig{
		Goal:      leancheck.Goal{Source: goalSource, Declaration: *declaration, Candidate: *candidateName},
		Portfolio: *portfolio,
		Source:    candidateSource, Outcome: *outcome, Evidence: *evidence,
		TaskID: *taskID, AttemptID: *attemptID, Snapshot: filepath.Base(*goalPath) + "-snapshot",
		LeaseEpoch: 1, ProfileID: "native-lean",
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	result, runErr := runNative(ctx, leancheck.DockerChecker{Config: lean}, config, work)
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, runErr)
		os.Exit(1)
	}
}
