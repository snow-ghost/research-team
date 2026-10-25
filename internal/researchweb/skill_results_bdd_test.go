package researchweb

import (
	"testing"

	"github.com/snow-ghost/research-team/internal/execution"
)

func TestSkillResultsBDD_InvalidResultKeepsOriginalAnswerAndUsage(t *testing.T) {
	p := execution.Profile{Skills: []execution.Skill{{ID: "formal", Version: "1", Contract: &execution.SkillContract{ResultChecks: []string{"lean_source"}}}}}
	r := execution.Result{Status: "candidate", Candidate: "No Lean file", Usage: &execution.Usage{OutputTokens: 91}, Events: []execution.Event{{Type: "model_requested"}}}
	enforceSkillResults(p, &r)
	if r.Status != "failed" || r.PrecheckStatus != "candidate" || r.Candidate != "No Lean file" || r.Usage.OutputTokens != 91 || r.FailureCode != "skill_result_contract" || len(r.SkillChecks) != 1 || r.SkillChecks[0].Passed {
		t.Fatal(r)
	}
	r = execution.Result{Status: "candidate", Candidate: "```lean\nimport Goal\ntheorem Candidate : Statement := by trivial\n```"}
	enforceSkillResults(p, &r)
	if r.Status != "candidate" || !r.SkillChecks[0].Passed {
		t.Fatal(r)
	}
}

func TestSkillResultsBDD_ReviewAndCounterexampleRequireTypedReports(t *testing.T) {
	for _, check := range []string{"review_report", "counterexample_report"} {
		p := execution.Profile{Skills: []execution.Skill{{ID: "check", Version: "1", Contract: &execution.SkillContract{ResultChecks: []string{check}}}}}
		if checkSkillResult(p, "Looks correct")[0].Passed {
			t.Fatal("unstructured result passed")
		}
		good := `{"rubric":"proof-review@2","summary":"Exact statement checked","findings":[]}`
		if check == "counterexample_report" {
			good = `{"outcome":"none_found","evidence":"Finite boundaries only; not a proof"}`
		}
		if !checkSkillResult(p, good)[0].Passed {
			t.Fatal(check)
		}
	}
	if validateSkillContract(execution.SkillContract{ResultChecks: []string{"shell"}}) == nil {
		t.Fatal("arbitrary validator accepted")
	}
}
