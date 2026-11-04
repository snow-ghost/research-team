package execution

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

type CandidateRule struct {
	Candidate   string
	Declaration string
	Theorem     string
	Outcome     string
}

var tacticPattern = regexp.MustCompile(`\b(?:intro|exact|omega|simp|norm_num|unfold|decide|apply|refine|rw|simpa|aesop|linarith)\b`)

func ValidateCandidate(raw string, rule CandidateRule) error {
	var answer map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &answer); err != nil {
		return errors.New("candidate must be one JSON object")
	}
	if len(answer) != 3 || answer["outcome"] == nil || answer["source"] == nil || answer["evidence"] == nil {
		return errors.New("candidate requires exactly outcome, source and evidence")
	}
	var outcome, source, evidence string
	if json.Unmarshal(answer["outcome"], &outcome) != nil || json.Unmarshal(answer["source"], &source) != nil || json.Unmarshal(answer["evidence"], &evidence) != nil {
		return errors.New("candidate fields must be strings")
	}
	expectedOutcome := rule.Outcome
	if expectedOutcome == "" {
		expectedOutcome = "proof"
	}
	if outcome != expectedOutcome {
		return errors.New("unexpected candidate outcome")
	}
	if !strings.HasPrefix(source, "import Goal\n") {
		return errors.New("source must start with import Goal")
	}
	for _, token := range []string{"Mathlib", "Proof.", "Qed.", "sorry", "native_decide", "admit", "axiom", "```"} {
		if strings.Contains(source, token) {
			return errors.New("forbidden source token")
		}
	}
	signature := "theorem " + rule.Candidate + " : " + rule.Declaration + " := by"
	if rule.Candidate == "" || rule.Declaration == "" || rule.Theorem == "" || !strings.Contains(source, signature) {
		return errors.New("missing exact theorem signature or library theorem")
	}
	if !strings.Contains(source, rule.Theorem) {
		return errors.New("library theorem is not used")
	}
	if expectedOutcome == "proof" && !strings.Contains(source, "intro ") {
		return errors.New("variables must be introduced before exact")
	}
	allowed := map[string]bool{"intro": true, "exact": true, "omega": true}
	if expectedOutcome == "refutation" {
		allowed = map[string]bool{"unfold": true, "decide": true}
	}
	for _, tactic := range tacticPattern.FindAllString(source, -1) {
		if !allowed[tactic] {
			return errors.New("tactic is not allowed")
		}
	}
	if len(strings.Fields(evidence)) > 40 {
		return errors.New("evidence exceeds 40 words")
	}
	return nil
}
