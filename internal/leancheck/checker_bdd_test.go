package leancheck

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestLeanBDD_RealKernelAndGoalPolicy(t *testing.T) {
	path := os.Getenv("RESEARCH_TEST_LEAN_CONFIG")
	if path == "" {
		t.Skip("set RESEARCH_TEST_LEAN_CONFIG; only local Lean containers are used")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config Config
	if json.Unmarshal(body, &config) != nil {
		t.Fatal("invalid Lean test configuration")
	}
	goal := Goal{Source: "def target : Prop := forall n : Nat, 0 + n = n\n", Declaration: "target", Candidate: "candidate"}
	for _, tc := range []struct {
		name, source string
		verified     bool
		phase        string
	}{
		{"valid", "import Goal\ntheorem candidate : target := by intro n; exact Nat.zero_add n\n", true, "complete"},
		{"wrong_goal", "import Goal\ntheorem candidate : True := by trivial\n", false, "audit"},
		{"sorry", "import Goal\ntheorem candidate : target := by sorry\n", false, "audit"},
		{"extra_axiom", "import Goal\naxiom fake : target\ntheorem candidate : target := fake\n", false, "audit"},
		{"goal_overwrite", "import Goal\ndef target : Prop := True\ntheorem candidate : target := by trivial\n", false, "compile"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := (DockerChecker{Config: config}).Check(context.Background(), goal, tc.source, t.TempDir())
			if (result.Status == "verified") != tc.verified || result.Phase != tc.phase {
				t.Fatalf("%+v %v", result, err)
			}
		})
	}
}
func TestLeanBDD_OnlyOneCompleteSourceBlock(t *testing.T) {
	for _, text := range []string{"none", "```lean\nby sorry", "```lean\nx\n```\n```lean\ny\n```"} {
		if _, err := ExtractSource(text); err == nil {
			t.Fatal("ambiguous source accepted")
		}
	}
	source, err := ExtractSource("Proof\n```lean\nimport Goal\n```\n")
	if err != nil || source != "import Goal\n" {
		t.Fatal("valid source block rejected")
	}
}
func TestLeanBDD_MathlibWideKernelRegression(t *testing.T) {
	path := os.Getenv("RESEARCH_TEST_LEAN_CONFIG")
	if path == "" {
		t.Skip("Lean configuration is unset")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config Config
	if json.Unmarshal(body, &config) != nil {
		t.Fatal("invalid configuration")
	}
	if len(config.Libraries) == 0 {
		t.Skip("mathlib is not configured")
	}
	goalSource, err := os.ReadFile("../../examples/research/wide-kernel/Goal.lean")
	if err != nil {
		t.Fatal(err)
	}
	goal := Goal{Source: string(goalSource), Declaration: "ResearchWideKernel.Statement", Candidate: "ResearchWideKernel.candidate"}
	source := `import Goal
namespace ResearchWideKernel
universe u
theorem candidate : Statement.{u} := by
  intro F _ m n hmn A
  classical
  let f : (Fin n → F) →ₗ[F] (Fin m → F) := A.mulVecLin
  have hdim : Module.finrank F (Fin m → F) < Module.finrank F (Fin n → F) := by
    simpa using hmn
  have hker : LinearMap.ker f ≠ ⊥ := LinearMap.ker_ne_bot_of_finrank_lt hdim
  obtain ⟨v, hv, hne⟩ := (LinearMap.ker f).ne_bot_iff.mp hker
  exact ⟨v, hne, hv⟩
end ResearchWideKernel
`
	report, err := (DockerChecker{Config: config}).Check(context.Background(), goal, source, t.TempDir())
	if err != nil || report.Status != "verified" {
		t.Fatalf("%+v %v", report, err)
	}
}
