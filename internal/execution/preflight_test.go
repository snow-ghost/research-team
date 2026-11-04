package execution

import (
	"encoding/json"
	"testing"
)

func TestCandidatePreflight(t *testing.T) {
	rule := CandidateRule{Candidate: "Candidate202", Declaration: "Research202", Theorem: "Wrapped4.sixUniqueFarOps_correct", Outcome: "proof"}
	valid := `{"outcome":"proof","source":"import Goal\ntheorem Candidate202 : Research202 := by\n  intro n hn a b c d e f x\n  exact Wrapped4.sixUniqueFarOps_correct hn a b c d e f x","evidence":"library application"}`
	if err := ValidateCandidate(valid, rule); err != nil {
		t.Fatal(err)
	}
	for name, source := range map[string]string{
		"mathlib":       "import Mathlib\ntheorem Candidate202 : Research202 := by\n  intro n hn a b c d e f x\n  exact Wrapped4.sixUniqueFarOps_length hn",
		"missing_intro": "import Goal\ntheorem Candidate202 : Research202 := by\n  exact Wrapped4.sixUniqueFarOps_correct hn a b c d e f x",
		"coq":           "import Goal\ntheorem Candidate202 : Research202 := by\n  intro n hn a b c d e f x\n  exact Wrapped4.sixUniqueFarOps_correct hn a b c d e f x\nQed.",
		"wrong_theorem": "import Goal\ntheorem Candidate202 : Research202 := by\n  intro n hn a b c d e f x\n  exact Wrapped4.sixUniqueFarOps_length hn",
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateCandidate(`{"outcome":"proof","source":`+strconvQuote(source)+`,"evidence":"x"}`, rule); err == nil {
				t.Fatal("invalid candidate accepted")
			}
		})
	}
	if err := ValidateCandidate(`{"outcome":"proof","source":"import Goal\n","evidence":"x","extra":1}`, rule); err == nil {
		t.Fatal("extra field accepted")
	}
}

func strconvQuote(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
