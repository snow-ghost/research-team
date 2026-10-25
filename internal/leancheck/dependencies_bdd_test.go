package leancheck

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDependenciesBDD_UnknownProcedureCannotEstablishUse(t *testing.T) {
	d := &ProofDependencies{ProcedureSHA256: "old", LibraryReferences: []DeclarationReference{{Module: "ResearchLemma_x", Declaration: "proof"}}}
	if d.Uses("ResearchLemma_x", "proof") {
		t.Fatal("unrecognized procedure trusted")
	}
	d.ProcedureSHA256 = DependencyDigest()
	if !d.Uses("ResearchLemma_x", "proof") || d.Uses("ResearchLemma_x", "other") {
		t.Fatal("exact declaration not checked")
	}
}

func TestDependenciesBDD_RealLeanDistinguishesImportsAndHelperReferences(t *testing.T) {
	file := os.Getenv("RESEARCH_TEST_LEAN_CONFIG")
	if file == "" {
		t.Skip("local Lean configuration required")
	}
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err = json.Unmarshal(body, &cfg); err != nil {
		t.Fatal(err)
	}
	module := "ResearchLemma_55555555555555555555555555555555"
	g := Goal{Source: "def AcceptedIdentity : Prop := forall n : Nat, n = n\n", Declaration: "AcceptedIdentity", Candidate: "identityProof"}
	dir := t.TempDir()
	r, err := (DockerChecker{Config: cfg}).BuildModule(context.Background(), g, "import Goal\ntheorem identityProof : AcceptedIdentity := by intro n; rfl\n", module, dir)
	if err != nil || r.Status != "ready" {
		t.Fatal(r, err)
	}
	cfg.Libraries = append(cfg.Libraries, filepath.Join(dir, "checked"))
	goal := Goal{Source: "import " + module + "\ndef Target : Prop := forall n : Nat, n = n\n", Declaration: "Target", Candidate: "candidate"}
	for _, tc := range []struct {
		name, source string
		used         bool
	}{
		{"import_only", "theorem candidate : Target := by intro n; rfl\n", false},
		{"direct", "theorem candidate : Target := identityProof\n", true},
		{"helper", "theorem localHelper : Target := identityProof\ntheorem candidate : Target := localHelper\n", true},
		{"goal_helper", "theorem candidate : Target := inputHelper\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := goal
			if tc.name == "goal_helper" {
				g.Source += "theorem inputHelper : Target := identityProof\n"
			}
			r, err := (DockerChecker{Config: cfg}).Check(context.Background(), g, "import Goal\n"+tc.source, t.TempDir())
			if err != nil || r.Status != "verified" || r.Dependencies == nil || r.Dependencies.Uses(module, "identityProof") != tc.used {
				t.Fatal(r, err)
			}
			if tc.name == "helper" {
				for _, ref := range r.Dependencies.LibraryReferences {
					if ref.Declaration == "identityProof" && ref.Direct {
						t.Fatal("indirect dependency labeled direct")
					}
				}
			}
		})
	}
}
