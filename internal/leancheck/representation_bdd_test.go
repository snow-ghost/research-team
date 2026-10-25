package leancheck

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func representationFixture() (Goal, Goal, Goal, string) {
	source := "import Mathlib\nnamespace Representation\ndef Direct : Prop := forall (f : Bool -> Nat) (g : Nat -> Bool), Function.LeftInverse g f -> forall x y : Bool, f x = f y -> x = y\ndef Original : Prop := forall (f : Bool -> Nat) (g : Nat -> Bool), Function.LeftInverse g f -> forall x y : Bool, x ≠ y -> f x ≠ f y\ndef Bridge : Prop := Direct ↔ Original\nend Representation\n"
	original := Goal{Source: source, Declaration: "Representation.Original", Candidate: "Representation.originalProof"}
	direct := Goal{Source: source, Declaration: "Representation.Direct", Candidate: "Representation.directProof"}
	bridge := Goal{Source: source, Declaration: "Representation.Bridge", Candidate: "Representation.bridgeProof"}
	proof := "import Goal\ntheorem Representation.bridgeProof : Representation.Bridge := by\n  constructor\n  · intro h f g hi x y hne heq\n    exact hne (h f g hi x y heq)\n  · intro h f g hi x y heq\n    by_contra hne\n    exact (h f g hi x y hne) heq\n"
	return original, direct, bridge, proof
}

func TestRepresentationBDD_TransferNeedsVerifiedBridgeAndExactOriginalGoal(t *testing.T) {
	o, d, b, source := representationFixture()
	calls := 0
	checker := portfolioChecker(func(_ context.Context, g Goal, s, _ string) (Report, error) {
		calls++
		return Report{Status: "verified", AuditSHA256: AuditDigest(), GoalSHA256: Digest(g), SourceSHA256: Digest(s)}, nil
	})
	r, err := CompareRepresentation(context.Background(), checker, o, d, b, source, "Codex", "acceptedLeftInverse", 6, 90, t.TempDir())
	if err != nil || calls != 4 || r.Transferred == nil || r.Transferred.Status != "verified" || Digest(r.Transferred.Goal) != Digest(o) || r.ModelCalls != 0 || r.Accepted || !strings.Contains(r.Transferred.Source, "bridgeProof).mp") {
		t.Fatal(r, err, calls)
	}
	bad := portfolioChecker(func(_ context.Context, g Goal, s, _ string) (Report, error) {
		status := "verified"
		if g.Declaration == b.Declaration {
			status = "failed"
		}
		return Report{Status: status, Phase: "compile", AuditSHA256: AuditDigest(), GoalSHA256: Digest(g), SourceSHA256: Digest(s)}, nil
	})
	r, err = CompareRepresentation(context.Background(), bad, o, d, b, source, "Codex", "acceptedLeftInverse", 6, 90, t.TempDir())
	if err != nil || r.Transferred != nil {
		t.Fatal("failed bridge transferred", r, err)
	}
}

func TestRepresentationBDD_RealLeanProvesEquivalentRouteWithoutModel(t *testing.T) {
	file := os.Getenv("RESEARCH_TEST_LEAN_CONFIG")
	if file == "" {
		t.Skip("local Lean configuration required")
	}
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatal(err)
	}
	o, d, b, source := representationFixture()
	// Use the established Mathlib theorem in this native regression; the live
	// system trial separately pins its accepted, audited local module.
	r, err := CompareRepresentation(context.Background(), DockerChecker{Config: cfg}, o, d, b, source, "Codex", "Function.LeftInverse.injective", 6, 90, t.TempDir())
	if err != nil || r.Direct.Status != "verified" || r.Bridge.Status != "verified" || r.Transferred == nil || r.Transferred.Status != "verified" {
		t.Fatal(r, err)
	}
	t.Logf("original=%s direct=%s bridge=%s transferred=%s model_calls=0", r.Original.Status, r.Direct.Status, r.Bridge.Status, r.Transferred.Status)
}
