package leancheck

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

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
	source := "import Goal\nnamespace Shared\ntheorem candidate : Statement := by\n  intro n\n  exact Nat.zero_add n\nend Shared\n"
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
	if err = os.WriteFile(filepath.Join(dir, "checked", module+".olean"), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if VerifyModuleArtifacts(filepath.Join(dir, "checked"), r) == nil {
		t.Fatal("tampered artifact accepted")
	}
}
