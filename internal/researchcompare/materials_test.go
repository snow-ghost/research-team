package researchcompare

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
)

func materialConfig(t *testing.T) Config {
	t.Helper()
	profile := func(id string, steps int) execution.Profile {
		return execution.Profile{ID: id, Kind: "model", Limits: execution.Limits{TimeoutSeconds: 900, MaxSteps: steps, MaxOutputTokens: 32768, MaxOutputBytes: 16 << 20}, Model: &execution.ModelConfig{Protocol: "chat_completions", BaseURL: "https://example.com/v1", Model: "same-model"}}
	}
	classes := []string{"a", "b", "c", "d", "e", "f"}
	cases := make([]Case, 12)
	for i := range cases {
		cases[i] = Case{ID: fmt.Sprintf("task-%03d", i+1), Class: classes[i%6], Goal: leancheck.Goal{Source: fmt.Sprintf("def Research%03d : Prop := True", i+1), Declaration: fmt.Sprintf("Research%03d", i+1), Candidate: fmt.Sprintf("Candidate%03d", i+1)}}
	}
	toolchain, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return Config{Version: 4, Cases: cases, Universal: profile("universal", 5), Proof: profile("proof", 1), Counter: profile("counter", 1), Formalize: profile("formalize", 3), Reviewer: profile("reviewer", 1), Lean: leancheck.Config{Runtime: "/usr/bin/false", Image: "sha256:" + strings.Repeat("a", 64), Toolchain: toolchain, ExpectedVersion: "fixture", TimeoutSeconds: 60, MemoryMB: 512}}
}

func TestComparisonMaterialsValidation(t *testing.T) {
	config := materialConfig(t)
	config.Materials = []Material{{Name: "Core.lean", Content: "def operation := 1"}}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	config.Materials[0].Name = "../Core.lean"
	if err := config.Validate(); err == nil {
		t.Fatal("material path accepted")
	}
	config.Materials[0].Name = "Core.lean"
	config.Materials[0].Content = ""
	if err := config.Validate(); err == nil {
		t.Fatal("empty material accepted")
	}
}

type materialExecutorFunc func(context.Context, execution.Task) (execution.Result, error)

func (f materialExecutorFunc) Run(ctx context.Context, task execution.Task) (execution.Result, error) {
	return f(ctx, task)
}

func TestComparisonMaterialsReachExecutorContext(t *testing.T) {
	config := materialConfig(t)
	config.Materials = []Material{{Name: "Core.lean", Content: "def operation := 1"}}
	contextSeen := ""
	runner := Runner{Build: func(execution.Profile, func(string) (string, bool)) (execution.Executor, error) {
		return materialExecutorFunc(func(_ context.Context, task execution.Task) (execution.Result, error) {
			contextSeen = task.Context
			return execution.Result{Status: "candidate", Candidate: `{"outcome":"inconclusive","source":"","evidence":"fixture"}`, Events: []execution.Event{{Type: "model_requested"}}}, nil
		}), nil
	}}
	run := Run{Profiles: map[string]string{}}
	if err := runner.run(context.Background(), config, config.Cases[0], "single", filepath.Join(t.TempDir(), "run"), &run, func(Run) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(contextSeen, `"materials"`) || !strings.Contains(contextSeen, "def operation") {
		t.Fatal("comparison material did not reach the executor context")
	}
}
