package proofreview

import "testing"

func TestReviewBDD_LimitationsAreNotMathematicalObligations(t *testing.T) {
	valid := Report{Rubric: Rubric, Summary: "Exact statement checked; no independent tool run.", Findings: []Finding{{Severity: "editorial", Category: "verification", Text: "No tools run by this reviewer."}}}
	if err := Validate(valid); err != nil {
		t.Fatal(err)
	}
	valid.Findings[0].Severity = "question"
	if Validate(valid) == nil {
		t.Fatal("unlocated blocking finding accepted")
	}
	valid.Findings[0].Evidence = "The natural-language goal quantifies over Int, but Lean quantifies over Nat."
	valid.Findings[0].Location = "goal.Statement"
	valid.Findings[0].Category = "statement"
	if err := Validate(valid); err != nil {
		t.Fatal(err)
	}
}

func TestReviewBDD_LegacyIsReadableWithoutReclassifyingFindings(t *testing.T) {
	r := Report{Summary: "Old review", Findings: []Finding{{Severity: "question", Text: "Unresolved obligation"}}}
	if err := Validate(r); err != nil || r.Findings[0].Severity != "question" {
		t.Fatal(err)
	}
	r.Rubric = "unknown"
	if Validate(r) == nil {
		t.Fatal("unknown rubric accepted")
	}
}
