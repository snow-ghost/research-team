package execution

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

type nativeFirstChecker func(context.Context, leancheck.Goal, string, string) (leancheck.Report, error)

func (f nativeFirstChecker) Check(ctx context.Context, goal leancheck.Goal, source, dir string) (leancheck.Report, error) {
	return f(ctx, goal, source, dir)
}

type nativeFirstExecutorFunc func(context.Context, Task) (Result, error)

func (f nativeFirstExecutorFunc) Run(ctx context.Context, task Task) (Result, error) {
	return f(ctx, task)
}

func nativeFirstTask(t *testing.T, role string) Task {
	t.Helper()
	goal := leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}
	contextData, _ := json.Marshal(map[string]any{
		"tool_goals":     map[string]leancheck.Goal{"check_lean": goal, "check_refutation": goal},
		"team_materials": map[string]any{"role": role, "refuting": false},
	})
	return Task{ID: "task", AttemptID: "attempt", Snapshot: "snapshot", LeaseEpoch: 1, Workspace: t.TempDir(), Objective: "prove", Context: string(contextData)}
}

func TestNativeFirstSkipsModelOnlyAfterVerifiedNativeCandidate(t *testing.T) {
	fallbackCalls, nativeCalls := 0, 0
	fallback := nativeFirstExecutorFunc(func(_ context.Context, _ Task) (Result, error) {
		fallbackCalls++
		return Result{Status: "candidate", Candidate: "model", Usage: &Usage{InputTokens: 10}, Events: []Event{{Type: "model_requested"}}}, nil
	})
	checker := nativeFirstChecker(func(_ context.Context, goal leancheck.Goal, source, _ string) (leancheck.Report, error) {
		nativeCalls++
		status := "failed"
		if strings.Contains(source, "omega") {
			status = "verified"
		}
		return leancheck.Report{Status: status, AuditSHA256: leancheck.AuditDigest(), GoalSHA256: leancheck.Digest(goal), SourceSHA256: leancheck.Digest(source)}, nil
	})
	result, err := NewNativeFirstExecutor(Profile{ID: "test"}, checker, fallback).Run(context.Background(), nativeFirstTask(t, "formalize"))
	if err != nil || result.Status != "candidate" || fallbackCalls != 0 || nativeCalls != 2 {
		t.Fatalf("native candidate did not skip fallback: calls=%d/%d result=%+v err=%v", fallbackCalls, nativeCalls, result, err)
	}
	if result.Native == nil || result.Native.ModelFallback || result.Native.Trials != 2 || result.Native.Verification.Status != "verified" || result.Usage.InputTokens != 0 || !strings.Contains(result.Candidate, "omega") {
		t.Fatalf("invalid native report: %+v", result)
	}
}

func TestNativeFirstFallsBackWithoutHidingNativeFailure(t *testing.T) {
	fallbackCalls, nativeCalls := 0, 0
	fallback := nativeFirstExecutorFunc(func(_ context.Context, _ Task) (Result, error) {
		fallbackCalls++
		return Result{Status: "candidate", Candidate: "model", Usage: &Usage{InputTokens: 10}, Events: []Event{{Type: "model_requested"}}}, nil
	})
	checker := nativeFirstChecker(func(_ context.Context, goal leancheck.Goal, source, _ string) (leancheck.Report, error) {
		nativeCalls++
		return leancheck.Report{Status: "failed", AuditSHA256: leancheck.AuditDigest(), GoalSHA256: leancheck.Digest(goal), SourceSHA256: leancheck.Digest(source)}, nil
	})
	result, err := NewNativeFirstExecutor(Profile{ID: "test"}, checker, fallback).Run(context.Background(), nativeFirstTask(t, "formalize"))
	if err != nil || fallbackCalls != 1 || nativeCalls != 6 || result.Candidate != "model" {
		t.Fatalf("fallback did not run exactly once: calls=%d/%d result=%+v err=%v", fallbackCalls, nativeCalls, result, err)
	}
	if result.Native == nil || !result.Native.ModelFallback || result.Native.Trials != 6 || result.Events[0].Type != "native_check" || result.Events[len(result.Events)-1].Type != "model_requested" {
		t.Fatalf("native failure was hidden: %+v", result)
	}
}

func TestNativeFirstDoesNotProveDuringReview(t *testing.T) {
	nativeCalls := 0
	checker := nativeFirstChecker(func(_ context.Context, goal leancheck.Goal, source, _ string) (leancheck.Report, error) {
		nativeCalls++
		return leancheck.Report{Status: "verified", AuditSHA256: leancheck.AuditDigest(), GoalSHA256: leancheck.Digest(goal), SourceSHA256: leancheck.Digest(source)}, nil
	})
	fallback := nativeFirstExecutorFunc(func(_ context.Context, _ Task) (Result, error) {
		return Result{Status: "candidate", Candidate: "review"}, nil
	})
	result, err := NewNativeFirstExecutor(Profile{ID: "test"}, checker, fallback).Run(context.Background(), nativeFirstTask(t, "review"))
	if err != nil || result.Candidate != "review" || nativeCalls != 0 {
		t.Fatalf("review triggered native proof: calls=%d result=%+v err=%v", nativeCalls, result, err)
	}
}

func TestNativeFirstBuildsVerifiedLibraryApplication(t *testing.T) {
	task := nativeFirstTask(t, "formalize")
	var packet map[string]any
	if err := json.Unmarshal([]byte(task.Context), &packet); err != nil {
		t.Fatal(err)
	}
	packet["library_search"] = map[string]any{"matches": []map[string]any{{
		"status": "verified", "theorem": "Wrapped4.sixUniqueFarOps_correct",
		"binders": []leancheck.Binder{}, "bindings": []leancheck.Argument{},
	}}}
	encoded, _ := json.Marshal(packet)
	task.Context = string(encoded)
	fallbackCalls, nativeCalls := 0, 0
	fallback := nativeFirstExecutorFunc(func(_ context.Context, _ Task) (Result, error) {
		fallbackCalls++
		return Result{Status: "candidate", Candidate: "model"}, nil
	})
	checker := nativeFirstChecker(func(_ context.Context, goal leancheck.Goal, source, _ string) (leancheck.Report, error) {
		nativeCalls++
		return leancheck.Report{Status: "verified", AuditSHA256: leancheck.AuditDigest(), GoalSHA256: leancheck.Digest(goal), SourceSHA256: leancheck.Digest(source)}, nil
	})
	result, err := NewNativeFirstExecutor(Profile{ID: "test"}, checker, fallback).Run(context.Background(), task)
	if err != nil || fallbackCalls != 0 || nativeCalls != 1 || result.Native.LibraryApplications != 1 || !strings.Contains(result.Candidate, "Wrapped4.sixUniqueFarOps_correct") {
		t.Fatalf("library application was not built natively: calls=%d/%d result=%+v err=%v", fallbackCalls, nativeCalls, result, err)
	}
}
