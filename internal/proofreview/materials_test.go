package proofreview

import (
	"github.com/snow-ghost/research-team/internal/leancheck"
	"testing"
)

func TestReviewInputBDD_SeparatesOriginalClaimFromCheckedNegation(t *testing.T) {
	o := leancheck.Goal{Source: "def Original : Prop := False\n", Declaration: "Original", Candidate: "originalProof"}
	n := leancheck.Goal{Source: o.Source + "\nnamespace ResearchRefutation\ndef Statement : Prop := Not (Original)\nend ResearchRefutation\n", Declaration: "ResearchRefutation.Statement", Candidate: "ResearchRefutation.candidate"}
	source := "import Goal\ntheorem ResearchRefutation.candidate : ResearchRefutation.Statement := by simp [ResearchRefutation.Statement, Original]\n"
	r := leancheck.Report{Status: "verified", GoalSHA256: leancheck.Digest(n), SourceSHA256: leancheck.Digest(source)}
	i, err := NewInput(o, n, source, r, "refutation", "Original claim", "author")
	if err != nil || i.ClaimKind != "refutation" || i.ClaimUnderReview.Declaration == i.OriginalGoal.Declaration || i.Schema != InputSchema {
		t.Fatal(i, err)
	}
	if _, err := NewInput(o, o, source, r, "refutation", "Original claim", "author"); err == nil {
		t.Fatal("positive goal accepted as its own negation")
	}
	r.SourceSHA256 = "changed"
	if _, err := NewInput(o, n, source, r, "refutation", "Original claim", "author"); err == nil {
		t.Fatal("substituted source accepted")
	}
}
