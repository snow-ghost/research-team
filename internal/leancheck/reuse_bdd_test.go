package leancheck

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReuseBDD_BothArmsBoundedAndExactReportsRequired(t *testing.T) {
	g := Goal{Source: "def Target : Prop := True", Declaration: "Target", Candidate: "candidate"}
	calls := 0
	checker := portfolioChecker(func(ctx context.Context, g Goal, s, _ string) (Report, error) {
		calls++
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("arm has no independent timeout")
		}
		status := "failed"
		if strings.Contains(s, "only [*, acceptedProof]") {
			status = "verified"
		}
		return Report{Status: status, Phase: "compile", AuditSHA256: AuditDigest(), GoalSHA256: Digest(g), SourceSHA256: Digest(s)}, nil
	})
	r, err := CompareReuse(context.Background(), checker, g, "ResearchLemma_11111111111111111111111111111111", "acceptedProof", 3, 5, t.TempDir())
	if err != nil || calls != 2 || r.Without.Status != "not_proved" || r.With.Status != "verified" || r.ModelCalls != 0 || r.Accepted {
		t.Fatal(r, err, calls)
	}
	bad := portfolioChecker(func(context.Context, Goal, string, string) (Report, error) {
		return Report{Status: "verified", AuditSHA256: AuditDigest(), SourceSHA256: "wrong"}, nil
	})
	if _, err := CompareReuse(context.Background(), bad, g, "ResearchLemma_11111111111111111111111111111111", "acceptedProof", 3, 5, t.TempDir()); err == nil {
		t.Fatal("unbound report accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CompareReuse(ctx, checker, g, "ResearchLemma_11111111111111111111111111111111", "acceptedProof", 3, 5, t.TempDir()); err == nil || calls != 2 {
		t.Fatal("cancelled trial invoked checker")
	}
}

func TestReuseBDD_RealLeanTransfersLeftInverseToUniquePreimage(t *testing.T) {
	file := os.Getenv("RESEARCH_TEST_LEAN_CONFIG")
	if file == "" {
		t.Skip("set RESEARCH_TEST_LEAN_CONFIG; local Lean only")
	}
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatal(err)
	}
	module := "ResearchLemma_55555555555555555555555555555555"
	g := Goal{Source: "import Mathlib\ndef AcceptedLeftInverse : Prop := forall (f : Bool -> Nat) (g : Nat -> Bool), Function.LeftInverse g f -> Function.Injective f\n", Declaration: "AcceptedLeftInverse", Candidate: "acceptedLeftInverse"}
	dir := t.TempDir()
	r, err := (DockerChecker{Config: cfg}).BuildModule(context.Background(), g, "import Goal\ntheorem acceptedLeftInverse : AcceptedLeftInverse := by\n  intro f g h x y heq\n  calc\n    x = g (f x) := (h x).symm\n    _ = g (f y) := congrArg g heq\n    _ = y := h y\n", module, dir)
	if err != nil || r.Status != "ready" {
		t.Fatal(r, err)
	}
	cfg.Libraries = append(cfg.Libraries, filepath.Join(dir, "checked"))
	goal := Goal{Source: "import Mathlib\ndef UniquePreimage : Prop := forall (f : Bool -> Nat) (g : Nat -> Bool), Function.LeftInverse g f -> forall x : Bool, f x = f true -> x = true\n", Declaration: "UniquePreimage", Candidate: "uniquePreimage"}
	trial, err := CompareReuse(context.Background(), DockerChecker{Config: cfg}, goal, module, "acceptedLeftInverse", 6, 90, t.TempDir())
	if err != nil || trial.With.Status != "verified" {
		t.Fatal(trial, err)
	}
	if trial.Without.Status != "not_proved" {
		t.Fatal("bounded baseline unexpectedly proved task; choose another discriminating example", trial)
	}
	t.Logf("without=%s with=%s model_calls=%d accepted=%v", trial.Without.Status, trial.With.Status, trial.ModelCalls, trial.Accepted)
}
