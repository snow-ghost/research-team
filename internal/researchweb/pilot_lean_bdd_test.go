//go:build linux

package researchweb

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestPilotBDD_RealLeanChecksStatementsCoverageAndNegativeControl(t *testing.T) {
	file := os.Getenv("RESEARCH_TEST_LEAN_CONFIG")
	if file == "" {
		t.Skip("RESEARCH_TEST_LEAN_CONFIG is unset")
	}
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var cfg leancheck.Config
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatal(err)
	}
	s := serviceFor(t, optionsFor(t, "http://127.0.0.1:1/v1"))
	if err := s.CreatePilot(PilotRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Recipe: PilotID, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	proofs := map[string]string{
		"ResearchIterations.Target":           "intro a f g h n; exact h.injective.iterate n",
		"ResearchIterations.LeftInverseClaim": "intro a b f g h; exact h.injective",
		"ResearchIterations.CompositionClaim": "intro a b c f g hf hg; exact hg.comp hf",
		"ResearchIterations.IterationClaim":   "intro composition a f hf n; induction n with\n  | zero => exact fun x y h => h\n  | succ n ih =>\n    intro x y h\n    apply composition a a a (f^[n]) f ih hf\n    simpa only [Function.iterate_succ_apply'] using h",
		"ResearchIterations.Coverage":         "intro inverse iterations composition a f g h n; exact iterations composition a f (inverse a a f g h) n",
	}
	for _, e := range stateOf(t, s).Entities {
		g := *e.FormalGoal
		t.Run(g.Declaration, func(t *testing.T) {
			template, _ := leancheck.CandidateTemplate(g)
			source := template + "  unfold " + g.Declaration + "\n  " + proofs[g.Declaration] + "\n"
			r, err := (leancheck.DockerChecker{Config: cfg}).Check(context.Background(), g, source, t.TempDir())
			if err != nil || r.Status != "verified" {
				t.Fatal(r, err)
			}
		})
	}
	g := pilotGoal("FalseControl", "forall (a : Type) (f g : a -> a), Function.RightInverse g f -> Function.Injective f")
	template, _ := leancheck.CandidateTemplate(g)
	r, _ := (leancheck.DockerChecker{Config: cfg}).Check(context.Background(), g, template+"  intro a f g h; exact h.injective\n", t.TempDir())
	if r.Status == "verified" {
		t.Fatal("right inverse silently substituted for left inverse")
	}
	negative := refutationGoal(g)
	template, _ = leancheck.CandidateTemplate(negative)
	source := template + "  unfold ResearchRefutation.Statement ResearchIterations.FalseControl\n  intro h\n  have inverse : Function.RightInverse (fun n : Nat => 2*n) (fun n : Nat => n/2) := by\n    intro n\n    change (2*n)/2 = n\n    omega\n  have bad : (0 : Nat) = 1 := h Nat (fun n => n/2) (fun n => 2*n) inverse (by decide)\n  exact Nat.zero_ne_one bad\n"
	r, err = (leancheck.DockerChecker{Config: cfg}).Check(context.Background(), negative, source, t.TempDir())
	if err != nil || r.Status != "verified" {
		t.Fatal("negative boundary control failed", r, err)
	}
}
