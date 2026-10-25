package researchcompare

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestEvaluationBDD_PreparationIsNotExecutionOrAuthorization(t *testing.T) {
	body, err := os.ReadFile("../../examples/evaluation-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var set EvaluationSet
	if err := json.Unmarshal(body, &set); err != nil {
		t.Fatal(err)
	}
	p, err := PrepareEvaluation(set)
	if err != nil || p.Cases != 12 || p.Runs != 96 || p.MaxRequests != 432 || p.ModelCalls != 0 || p.MathematicalVerification || p.Status != "prepared_not_authorized" {
		t.Fatal(p, err)
	}
	set.Cases[0].ID = "false-case"
	if _, err := PrepareEvaluation(set); err == nil {
		t.Fatal("label hint accepted")
	}
}

func TestEvaluationBDD_RealLeanCompilesEveryGoalWithoutAcceptingSyntheticProof(t *testing.T) {
	file := os.Getenv("RESEARCH_TEST_LEAN_CONFIG")
	if file == "" {
		t.Skip("set RESEARCH_TEST_LEAN_CONFIG; local containers only")
	}
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var cfg leancheck.Config
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile("../../examples/evaluation-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var set EvaluationSet
	if err := json.Unmarshal(body, &set); err != nil {
		t.Fatal(err)
	}
	for _, c := range set.Cases {
		t.Run(c.ID, func(t *testing.T) {
			source := "import Goal\ntheorem " + c.Goal.Candidate + " : " + c.Goal.Declaration + " := by sorry\n"
			r, err := (leancheck.DockerChecker{Config: cfg}).Check(context.Background(), c.Goal, source, t.TempDir())
			if r.Phase != "audit" || r.Status != "rejected" || r.GoalSHA256 != leancheck.Digest(c.Goal) {
				t.Fatal("goal did not compile or synthetic proof was accepted", r, err)
			}
		})
	}
}
