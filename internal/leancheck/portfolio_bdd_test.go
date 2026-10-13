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

type portfolioChecker func(context.Context, Goal, string, string) (Report, error)

func (f portfolioChecker) Check(c context.Context, g Goal, s, d string) (Report, error) {
	return f(c, g, s, d)
}

func TestPortfolioBDD_PartialSimpDoesNotMaskRingAndReportsRemainSeparate(t *testing.T) {
	g := Goal{Source: "def Target : Prop := True", Declaration: "Target", Candidate: "candidate"}
	visited := []string{}
	c := portfolioChecker(func(_ context.Context, goal Goal, source, dir string) (Report, error) {
		visited = append(visited, filepath.Base(dir))
		r := Report{Status: "failed", GoalSHA256: Digest(goal), SourceSHA256: Digest(source), AuditSHA256: AuditDigest()}
		if strings.HasSuffix(source, "  ring\n") {
			r.Status = "verified"
			return r, nil
		}
		return r, errors.New("unsolved goals")
	})
	results, err := TryTactics(context.Background(), c, g, filepath.Join(t.TempDir(), "trials"))
	if err != nil || len(results) != 3 || strings.Join(visited, ",") != "simp_all,omega,ring" {
		t.Fatal(results, visited, err)
	}
}

func TestApplicationBDD_InvalidParameterNamesAreNotLeanCommands(t *testing.T) {
	g := Goal{Source: "def Target : Prop := True", Declaration: "Target", Candidate: "candidate"}
	for _, a := range []Argument{{Name: "n); axiom fake", Term: "2"}, {Name: "m", Term: "2"}, {Name: "n", Term: "2\naxiom fake"}} {
		if _, err := ApplicationSource(g, "Shared.candidate", []Binder{{Name: "n"}}, []Argument{a}); err == nil {
			t.Fatal("invalid argument accepted", a)
		}
	}
}

func TestPortfolioBDD_RealLeanFindsPolynomialProofWithoutModel(t *testing.T) {
	file := os.Getenv("RESEARCH_TEST_LEAN_CONFIG")
	if file == "" {
		t.Skip("RESEARCH_TEST_LEAN_CONFIG is unset")
	}
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatal(err)
	}
	g := Goal{Source: "import Mathlib\ndef Target : Prop := forall n : Nat, (n + 1)^2 = n^2 + 2*n + 1\n", Declaration: "Target", Candidate: "candidate"}
	r, err := TryTactics(context.Background(), DockerChecker{Config: cfg}, g, filepath.Join(t.TempDir(), "portfolio"))
	if err != nil || len(r) == 0 || r[len(r)-1].Report.Status != "verified" {
		t.Fatal(r, err)
	}
}
