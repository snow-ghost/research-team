package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

type nativeCheckerFunc func(context.Context, leancheck.Goal, string, string) (leancheck.Report, error)

func (f nativeCheckerFunc) Check(ctx context.Context, goal leancheck.Goal, source, dir string) (leancheck.Report, error) {
	return f(ctx, goal, source, dir)
}

func TestRunNativeUsesNoModelCalls(t *testing.T) {
	config := nativeConfig{
		Goal:   leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"},
		Source: "import Goal\ntheorem Candidate : Statement := by trivial\n", Outcome: "proof", Evidence: "native",
		TaskID: "task", AttemptID: "attempt", Snapshot: "snapshot", LeaseEpoch: 1, ProfileID: "native-lean",
	}
	for _, status := range []string{"verified", "failed"} {
		t.Run(status, func(t *testing.T) {
			calls := 0
			checker := nativeCheckerFunc(func(_ context.Context, goal leancheck.Goal, source, _ string) (leancheck.Report, error) {
				calls++
				return leancheck.Report{Status: status, AuditSHA256: leancheck.AuditDigest(), GoalSHA256: leancheck.Digest(goal), SourceSHA256: leancheck.Digest(source)}, nil
			})
			result, err := runNative(context.Background(), checker, config, t.TempDir())
			if calls != 1 {
				t.Fatalf("unexpected native check count: %d", calls)
			}
			if status == "verified" && (err != nil || result.Status != "candidate") {
				t.Fatalf("valid native candidate rejected: %+v %v", result, err)
			}
			if status == "failed" && (err == nil || result.Status != "failed") {
				t.Fatalf("invalid native candidate accepted: %+v %v", result, err)
			}
			if result.Usage == nil || result.Usage.InputTokens != 0 || result.Usage.OutputTokens != 0 || result.RemoteOutcome != "not_started" {
				t.Fatalf("native usage is not zero and local: %+v", result.Usage)
			}
			for _, event := range result.Events {
				if event.Type == "model_requested" {
					t.Fatal("native verification called a model")
				}
			}
			var answer map[string]string
			if json.Unmarshal([]byte(result.Candidate), &answer) != nil || answer["source"] != config.Source {
				t.Fatalf("candidate source was not preserved: %s", result.Candidate)
			}
		})
	}
}

func TestRunNativePortfolioTriesFreshTactics(t *testing.T) {
	config := nativeConfig{
		Goal:      leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"},
		Portfolio: true, Outcome: "proof", Evidence: "native portfolio",
		TaskID: "task", AttemptID: "attempt", Snapshot: "snapshot", LeaseEpoch: 1, ProfileID: "native-lean",
	}
	calls := 0
	checker := nativeCheckerFunc(func(_ context.Context, goal leancheck.Goal, source, _ string) (leancheck.Report, error) {
		calls++
		status := "failed"
		if strings.Contains(source, "omega") {
			status = "verified"
		}
		return leancheck.Report{Status: status, AuditSHA256: leancheck.AuditDigest(), GoalSHA256: leancheck.Digest(goal), SourceSHA256: leancheck.Digest(source)}, nil
	})
	result, err := runNative(context.Background(), checker, config, t.TempDir())
	if err != nil || result.Status != "candidate" || calls != 2 || !strings.Contains(result.Candidate, "omega") {
		t.Fatalf("native portfolio did not try fresh tactics: calls=%d result=%+v err=%v", calls, result, err)
	}
}
