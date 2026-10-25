package leancheck

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLibraryBDD_CandidateImportsPrecedeInlinedDefinitions(t *testing.T) {
	g := Goal{Source: "def Statement : Prop := True\n", Declaration: "Statement", Candidate: "candidate"}
	source := "import Goal\nimport Mathlib\n\ntheorem candidate : Statement := by trivial\n"
	combined, err := ModuleSource(g, source)
	if err != nil || !strings.HasPrefix(combined, "import Mathlib\n\ndef Statement") || strings.Count(combined, "import Mathlib") != 1 || strings.Contains(combined, "import Goal") {
		t.Fatal(combined, err)
	}
	plain := "import Goal\n\ntheorem candidate : Statement := by trivial\n"
	got, err := ModuleSource(g, plain)
	if err != nil || got != g.Source+"\n"+strings.TrimPrefix(plain, "import Goal\n") {
		t.Fatal("existing module source changed", got, err)
	}
}

func TestLibraryBDD_RealModuleImportAndTampering(t *testing.T) {
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
	goal := Goal{Source: "namespace Shared\ndef Statement : Prop := forall n : Nat, 0 + n = n\nend Shared\n", Declaration: "Shared.Statement", Candidate: "Shared.candidate"}
	source := "import Goal\nimport Mathlib\nnamespace Shared\ntheorem candidate : Statement := by\n  intro n\n  exact Nat.zero_add n\nend Shared\n"
	dir := filepath.Join(t.TempDir(), "module")
	if err = os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	module := "ResearchLemma_11111111111111111111111111111111"
	r, err := (DockerChecker{Config: cfg}).BuildModule(context.Background(), goal, source, module, dir)
	if err != nil || r.Status != "ready" {
		t.Fatal("module failed", r, err)
	}
	if err = VerifyModuleArtifacts(filepath.Join(dir, "checked"), r); err != nil {
		t.Fatal(err)
	}
	signature, err := (DockerChecker{Config: cfg}).InspectModule(context.Background(), goal, filepath.Join(dir, "checked"), r, filepath.Join(t.TempDir(), "inspect"))
	if err != nil || signature.Status != "verified" || len(signature.Binders) != 1 || signature.Binders[0].Type != "Nat" || signature.InspectorSHA256 != InspectorDigest() {
		t.Fatal("native signature failed", signature, err)
	}
	cfg.Libraries = append(cfg.Libraries, filepath.Join(dir, "checked"))
	reuse := Goal{Source: "import " + module + "\nnamespace Reuse\ndef Statement : Prop := Shared.Statement\nend Reuse\n", Declaration: "Reuse.Statement", Candidate: "Reuse.candidate"}
	checkDir := filepath.Join(t.TempDir(), "reuse")
	if err = os.Mkdir(checkDir, 0700); err != nil {
		t.Fatal(err)
	}
	verified, err := (DockerChecker{Config: cfg}).Check(context.Background(), reuse, "import Goal\ntheorem Reuse.candidate : Reuse.Statement := Shared.candidate\n", checkDir)
	if err != nil || verified.Status != "verified" {
		t.Fatal("import did not verify", verified, err)
	}
	application := Goal{Source: "import " + module + "\nnamespace Application\ndef Statement : Prop := 0 + (2 : Nat) = 2\nend Application\n", Declaration: "Application.Statement", Candidate: "Application.candidate"}
	for _, tc := range []struct {
		term  string
		valid bool
	}{{"2", true}, {"True", false}, {"3", false}} {
		candidate, err := ApplicationSource(application, goal.Candidate, signature.Binders, []Argument{{Name: signature.Binders[0].Name, Term: tc.term}})
		if err != nil {
			t.Fatal(err)
		}
		r, _ := (DockerChecker{Config: cfg}).Check(context.Background(), application, candidate, t.TempDir())
		if (r.Status == "verified") != tc.valid {
			t.Fatalf("unverified substitution accepted: %s %+v", tc.term, r)
		}
	}
	if err = os.WriteFile(filepath.Join(dir, "checked", module+".olean"), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if VerifyModuleArtifacts(filepath.Join(dir, "checked"), r) == nil {
		t.Fatal("tampered artifact accepted")
	}
}
