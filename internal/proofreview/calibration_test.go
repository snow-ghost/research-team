package proofreview

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestCalibrationBDD_ScoresNeedCompleteSourceBoundAnnotations(t *testing.T) {
	c := CalibrationCases()[2]
	r := Report{Rubric: Rubric, Summary: "Review", Findings: []Finding{{Severity: "major", Category: "statement", Text: "Goal changed", Evidence: "Different natural and formal bounds", Location: "statement"}, {Severity: "question", Category: "dependency", Text: "Unsupported missing dependency", Evidence: "Proposed check", Location: "source"}, {Severity: "editorial", Category: "verification", Text: "No independent rerun"}}}
	a := ReviewAnnotation{Case: c.ID, ReportSHA256: leancheck.Digest(r), Annotator: "fixture annotator", Findings: []FindingAnnotation{{Index: 0, Issue: c.Expected[0].ID}, {Index: 1}, {Index: 2}}}
	s, err := ScoreCalibration(c, r, a)
	if err != nil || s.Detected != 1 || s.Missed != 0 || s.FalseAlarms != 1 || s.Editorial != 1 || s.Precision == nil || *s.Precision != 0.5 || s.Recall == nil || *s.Recall != 1 {
		t.Fatal(s, err)
	}
	a.ReportSHA256 = "changed"
	if _, err := ScoreCalibration(c, r, a); err == nil {
		t.Fatal("annotation applied to different report")
	}
	a.ReportSHA256 = leancheck.Digest(r)
	a.Findings = a.Findings[:2]
	if _, err := ScoreCalibration(c, r, a); err == nil {
		t.Fatal("partial annotation scored")
	}
}
func TestCalibrationBDD_EditorialOnlyDoesNotDetectMathematicalIssue(t *testing.T) {
	c := CalibrationCases()[3]
	r := Report{Rubric: Rubric, Summary: "Only editorial", Findings: []Finding{{Severity: "editorial", Category: "premise", Text: "An extra inverse"}}}
	a := ReviewAnnotation{Case: c.ID, ReportSHA256: leancheck.Digest(r), Annotator: "fixture", Findings: []FindingAnnotation{{Index: 0, Issue: c.Expected[0].ID}}}
	s, err := ScoreCalibration(c, r, a)
	if err != nil || s.Detected != 0 || s.Missed != 1 || s.Editorial != 1 {
		t.Fatal(s, err)
	}
}
func TestCalibrationBDD_RealLeanChecksAllFixtureProofs(t *testing.T) {
	file := os.Getenv("RESEARCH_TEST_LEAN_CONFIG")
	if file == "" {
		t.Skip("set RESEARCH_TEST_LEAN_CONFIG")
	}
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var cfg leancheck.Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	for _, c := range CalibrationCases() {
		t.Run(c.ID, func(t *testing.T) {
			claim := CalibrationClaim(c)
			r, err := (leancheck.DockerChecker{Config: cfg}).Check(context.Background(), claim, c.Source, t.TempDir())
			if err != nil || r.Status != "verified" {
				t.Fatal("fixture source did not verify", r, err)
			}
			input, err := NewInput(c.Goal, claim, c.Source, r, c.Kind, c.Statement, "fixture")
			if err != nil || input.ClaimKind != c.Kind {
				t.Fatal(err)
			}
		})
	}
}

func TestCalibrationBDD_ExpandedCasesSeparateExpositionFromCheckedProof(t *testing.T) {
	cases := ExpandedCalibrationCases()
	if len(cases) != 12 {
		t.Fatal("expanded experiment requires 48 calls")
	}
	seen := map[string]bool{}
	for _, c := range cases {
		if seen[c.ID] {
			t.Fatal("duplicate case")
		}
		seen[c.ID] = true
	}
	for _, c := range cases[6:] {
		if c.Exposition == "" {
			t.Fatal("missing exposition")
		}
		for _, issue := range c.Expected {
			if issue.ID == "" || issue.Description == "" {
				t.Fatal("missing reference annotation")
			}
		}
	}
	file := os.Getenv("RESEARCH_TEST_LEAN_CONFIG")
	if file == "" {
		t.Skip("set RESEARCH_TEST_LEAN_CONFIG")
	}
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var cfg leancheck.Config
	if err = json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases[6:] {
		t.Run(c.ID, func(t *testing.T) {
			r, err := (leancheck.DockerChecker{Config: cfg}).Check(context.Background(), c.Goal, c.Source, t.TempDir())
			if err != nil || r.Status != "verified" {
				t.Fatal(r, err)
			}
		})
	}
}
