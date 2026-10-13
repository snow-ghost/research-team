package leancheck

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTypedMatchBDD_InfersParametersExposesPremisesAndChecksKernel(t *testing.T) {
	file := os.Getenv("RESEARCH_TEST_LEAN_CONFIG")
	if file == "" {
		t.Skip("RESEARCH_TEST_LEAN_CONFIG is unset")
	}
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err = json.Unmarshal(body, &cfg); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"plain", "conditional", "later_premise_infers_functions"} {
		conditional := scenario == "conditional"
		const moduleName = "ResearchLemma_22222222222222222222222222222222"
		statement, proof := "forall n : Nat, 0 + n = n", "intro n; exact Nat.zero_add n"
		if conditional {
			statement, proof = "forall n : Nat, 0 < n -> 0 + n = n", "intro n _; exact Nat.zero_add n"
		}
		imports := ""
		if scenario == "later_premise_infers_functions" {
			imports = "import Mathlib\n"
			statement = "forall (a b : Type) (f : a -> b) (g : b -> a), Function.LeftInverse g f -> Function.Injective f"
			proof = "intro a b f g h; exact h.injective"
		}
		g := Goal{Source: imports + "def Accepted : Prop := " + statement + "\n", Declaration: "Accepted", Candidate: "acceptedProof"}
		dir := filepath.Join(t.TempDir(), "library")
		if err = os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		module, err := (DockerChecker{Config: cfg}).BuildModule(context.Background(), g, "import Goal\ntheorem acceptedProof : Accepted := by "+proof+"\n", moduleName, dir)
		if err != nil || module.Status != "ready" {
			t.Fatal(module, err)
		}
		target := Goal{Source: "import " + moduleName + "\ndef Target : Prop := 0 + (2 : Nat) = 2\n", Declaration: "Target", Candidate: "targetProof"}
		if scenario == "later_premise_infers_functions" {
			target.Source = "import " + moduleName + "\ndef Target : Prop := forall (f g : Bool -> Bool), Function.LeftInverse g f -> Function.Injective f\n"
		}
		r, err := (DockerChecker{Config: cfg}).MatchModule(context.Background(), target, filepath.Join(dir, "checked"), module, g.Candidate, t.TempDir())
		if err != nil {
			t.Fatal(err, r.Diagnostics)
		}
		if conditional {
			if r.Status != "obligations" || len(r.Missing) != 1 || r.Missing[0].Kind != "premise" || r.Missing[0].ClosedType == "" || r.Verification != nil {
				t.Fatal("missing premise hidden", r)
			}
		} else if r.Status != "verified" || len(r.Missing) != 0 || r.Verification == nil || r.Verification.AuditSHA256 != AuditDigest() || r.Source == "" {
			t.Fatal("unification promoted without kernel", r)
		}
		target.Source = "import " + moduleName + "\ndef Target : Prop := (0 : Nat) = 1\n"
		if scenario == "later_premise_infers_functions" {
			target.Source = "import " + moduleName + "\ndef Target : Prop := forall (f g : Bool -> Bool), Function.LeftInverse g f -> False\n"
		}
		r, err = (DockerChecker{Config: cfg}).MatchModule(context.Background(), target, filepath.Join(dir, "checked"), module, g.Candidate, t.TempDir())
		if err != nil || r.Status != "no_match" {
			t.Fatal("false target matched", r, err)
		}
	}
}
