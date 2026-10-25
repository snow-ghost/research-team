package leancheck

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChainsBDD_BoundedSubsetsDoNotAcceptUncheckedCandidates(t *testing.T) {
	g := Goal{Source: "def Target : Prop := True", Declaration: "Target", Candidate: "candidate"}
	calls := 0
	checker := portfolioChecker(func(_ context.Context, g Goal, s, _ string) (Report, error) {
		calls++
		status := "failed"
		if strings.Contains(s, "only [*, First, Second]") {
			status = "verified"
		}
		return Report{Status: status, Phase: "compile", AuditSHA256: AuditDigest(), GoalSHA256: Digest(g), SourceSHA256: Digest(s)}, nil
	})
	r, err := SearchChains(context.Background(), checker, g, []string{"First", "Second"}, 2, 2, t.TempDir())
	if err != nil || r.Status != "bounded" || len(r.Trials) != 2 || r.Verification != nil || calls != 2 {
		t.Fatal(r, err, calls)
	}
	r, err = SearchChains(context.Background(), checker, g, []string{"First", "Second"}, 2, 3, t.TempDir())
	if err != nil || r.Status != "verified" || len(r.Trials) != 3 || r.Verification == nil {
		t.Fatal(r, err)
	}
	bad := portfolioChecker(func(_ context.Context, g Goal, s, _ string) (Report, error) {
		return Report{Status: "verified", AuditSHA256: AuditDigest(), GoalSHA256: "wrong", SourceSHA256: Digest(s)}, nil
	})
	if r, err := SearchChains(context.Background(), bad, g, []string{"First"}, 2, 3, t.TempDir()); err == nil || r.Status == "verified" {
		t.Fatal("unbound check accepted", r, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := calls
	if _, err := SearchChains(ctx, checker, g, []string{"First"}, 2, 3, t.TempDir()); !errors.Is(err, context.Canceled) || calls != before {
		t.Fatal("cancelled search invoked checker", err)
	}
}

func TestChainsBDD_RealLeanCombinesTwoPremisesWithoutModel(t *testing.T) {
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
	modules := []struct{ name, goal, candidate, proof string }{
		{"ResearchLemma_11111111111111111111111111111111", "forall (P Q : Prop), P /\\ Q -> P", "firstProof", "intro P Q h; exact h.1"},
		{"ResearchLemma_33333333333333333333333333333333", "forall (P Q : Prop), P -> P \\/ Q", "secondProof", "intro P Q h; exact Or.inl h"},
	}
	imports := "import Mathlib\n"
	for _, m := range modules {
		dir := t.TempDir()
		decl := "Accepted_" + m.candidate
		g := Goal{Source: "import Mathlib\ndef " + decl + " : Prop := " + m.goal + "\n", Declaration: decl, Candidate: m.candidate}
		r, err := (DockerChecker{Config: cfg}).BuildModule(context.Background(), g, "import Goal\ntheorem "+m.candidate+" : "+decl+" := by "+m.proof+"\n", m.name, dir)
		if err != nil || r.Status != "ready" {
			t.Fatal(r, err)
		}
		cfg.Libraries = append(cfg.Libraries, filepath.Join(dir, "checked"))
		imports = "import " + m.name + "\n" + imports
	}
	g := Goal{Source: imports + "def Target : Prop := forall (P Q R : Prop), P /\\ Q -> P \\/ R\n", Declaration: "Target", Candidate: "candidate"}
	r, err := SearchChains(context.Background(), DockerChecker{Config: cfg}, g, []string{"firstProof", "secondProof"}, 3, 3, t.TempDir())
	if err != nil || r.Status != "verified" || r.Verification == nil || len(r.Trials) != 3 {
		t.Fatal(r, err)
	}
}
