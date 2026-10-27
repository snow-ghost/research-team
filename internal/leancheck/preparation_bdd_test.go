package leancheck

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func preparationConfig(t *testing.T) Config {
	t.Helper()
	file := os.Getenv("RESEARCH_TEST_LEAN_CONFIG")
	if file == "" {
		t.Skip("set RESEARCH_TEST_LEAN_CONFIG for native preparation")
	}
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}
func TestPreparationBDD_RealDefinitionsDoNotProveFalseAndCanBeReused(t *testing.T) {
	cfg := preparationConfig(t)
	checker := DockerChecker{Config: cfg}
	goals := []Goal{{Source: "namespace PrepA\ndef Statement : Prop := True\nend PrepA", Declaration: "PrepA.Statement", Candidate: "PrepA.candidate"}, {Source: "namespace PrepB\ndef Statement : Prop := False\nend PrepB", Declaration: "PrepB.Statement", Candidate: "PrepB.candidate"}, {Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "GlobalCandidate"}}
	dir := t.TempDir()
	r, err := checker.PrepareGoals(context.Background(), goals, dir)
	if err != nil || r.Status != "ready" {
		t.Fatal(r, err)
	}
	if err := VerifyPreparedDefinitions(dir, r, goals); err != nil {
		t.Fatal(err)
	}
	tampered := r
	joint := *r.Joint
	joint.GoalSHA256 = Digest("different goals")
	tampered.Joint = &joint
	if VerifyPreparedDefinitions(dir, tampered, goals) == nil {
		t.Fatal("joint report for different goals accepted")
	}
	failed, _ := checker.Check(context.Background(), goals[1], "import Goal\ntheorem PrepB.candidate : PrepB.Statement := by trivial\n", t.TempDir())
	if failed.Status == "verified" {
		t.Fatal("preparation proved False")
	}
	cfg.Libraries = append(cfg.Libraries, filepath.Join(dir, "checked"))
	g := goals[0]
	module := "ResearchLemma_11111111111111111111111111111111"
	built, err := (DockerChecker{Config: cfg}).BuildSharedModule(context.Background(), g, "import Goal\ntheorem PrepA.candidate : PrepA.Statement := by trivial\n", module, t.TempDir(), r.Definitions[0])
	if err != nil || built.Status != "ready" {
		t.Fatal("shared definitions cannot be used by a proof module", built, err)
	}
	file := filepath.Join(dir, "checked", r.Definitions[0].Module+".olean")
	if err := os.WriteFile(file, []byte("corrupted"), 0600); err != nil {
		t.Fatal(err)
	}
	if VerifyPreparedDefinitions(dir, r, goals) == nil {
		t.Fatal("tampered definitions accepted")
	}
}
func TestPreparationBDD_RealSelfImportAndNameCollisionAreRejected(t *testing.T) {
	cfg := preparationConfig(t)
	for name, goals := range map[string][]Goal{
		"self_import": {{Source: "import Goal\ndef Target : Prop := True", Declaration: "Target", Candidate: "candidate"}},
		"collision":   {{Source: "def Target : Prop := True", Declaration: "Target", Candidate: "candidateOne"}, {Source: "def Target : Prop := False", Declaration: "Target", Candidate: "candidateTwo"}},
	} {
		t.Run(name, func(t *testing.T) {
			r, _ := (DockerChecker{Config: cfg}).PrepareGoals(context.Background(), goals, t.TempDir())
			if r.Status == "ready" {
				t.Fatal("invalid plan accepted", r)
			}
		})
	}
}
