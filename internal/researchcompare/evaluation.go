package researchcompare

import (
	"fmt"
	"regexp"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

type EvaluationSet struct {
	Version     int    `json:"version"`
	Repetitions int    `json:"repetitions"`
	Cases       []Case `json:"cases"`
}

type EvaluationPlan struct {
	Status                   string `json:"status"`
	CaseSetSHA256            string `json:"case_set_sha256"`
	PolicySHA256             string `json:"policy_sha256"`
	Cases                    int    `json:"cases"`
	Runs                     int    `json:"runs"`
	MaxRequests              int    `json:"max_requests"`
	ModelCalls               int    `json:"model_calls"`
	MathematicalVerification bool   `json:"mathematical_verification"`
}

const evaluationPolicy = "research-evaluation-plan@1;single,team,demand,lean;paired-goals;2-repetitions;6-requests-per-arm;32768-output;900-seconds-per-arm;common-reviewer;no-cross-arm-context;no-interrupted-replay;operator-acceptance-separate;provider,format,native,review,operator,usage-separate"

func EvaluationPolicyDigest() string {
	return leancheck.Digest([]string{evaluationPolicy, DemandPolicyDigest(), leancheck.DependencyDigest()})
}

func PrepareEvaluation(set EvaluationSet) (EvaluationPlan, error) {
	p := EvaluationPlan{Status: "prepared_not_authorized", CaseSetSHA256: leancheck.Digest(set), PolicySHA256: EvaluationPolicyDigest()}
	if set.Version != 1 || set.Repetitions != 2 || len(set.Cases) != 12 {
		return p, fmt.Errorf("expected 12 cases and two repetitions")
	}
	ids, goals, classes := map[string]bool{}, map[string]bool{}, map[string]int{}
	for _, c := range set.Cases {
		if !regexp.MustCompile(`^task-[0-9]{3}$`).MatchString(c.ID) || ids[c.ID] || c.Class == "" {
			return p, fmt.Errorf("neutral distinct task ids and classes required")
		}
		if err := c.Goal.Validate(); err != nil {
			return p, err
		}
		digest := leancheck.Digest(c.Goal)
		if goals[digest] {
			return p, fmt.Errorf("duplicate goal")
		}
		ids[c.ID], goals[digest] = true, true
		classes[c.Class]++
	}
	if len(classes) != 6 {
		return p, fmt.Errorf("six classes required")
	}
	for _, n := range classes {
		if n != 2 {
			return p, fmt.Errorf("two cases per class required")
		}
	}
	p.Cases, p.Runs, p.MaxRequests = 12, 12*set.Repetitions*4, 12*set.Repetitions*3*6
	return p, nil
}
