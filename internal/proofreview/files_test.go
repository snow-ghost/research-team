package proofreview

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestReviewFilesBDD_SeparatesDefinitionsAndProofWithoutChangingGoal(t *testing.T) {
	g := leancheck.Goal{Source: "def Statement : Prop := True\n", Declaration: "Statement", Candidate: "candidate"}
	source := "import Goal\ntheorem candidate : Statement := by trivial\n"
	r := leancheck.Report{Status: "verified", Phase: "complete", AuditSHA256: leancheck.AuditDigest(), GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(source)}
	i, err := NewInput(g, g, source, r, "proof", "True", "author")
	if err != nil {
		t.Fatal(err)
	}
	legacy, _ := json.Marshal(i)
	if strings.Contains(string(legacy), "checked_files") {
		t.Fatal("old inputs changed")
	}
	b, err := WithCheckedFiles(i, g.Source, source)
	if err != nil || b.CheckedFiles.GoalModule != "Goal" || !b.CheckedFiles.GoalOriginChecked || b.CheckedFiles.Candidate.RawSHA256 != RawSHA256(source) || b.CheckedFiles.Definitions.Source != g.Source || b.GoalSHA256 != i.GoalSHA256 {
		t.Fatal("file roles or goal binding changed", err)
	}
	if b.CheckedFiles.Candidate.RawSHA256 == b.SourceSHA256 {
		t.Fatal("raw and JSON digests confused")
	}
	for _, pair := range [][2]string{{g.Source + "\n", source}, {g.Source, source + "\n"}} {
		if _, err := WithCheckedFiles(i, pair[0], pair[1]); err == nil {
			t.Fatal("changed compiler input accepted")
		}
	}
	i.Report.AuditSHA256 = "untrusted"
	if _, err := WithCheckedFiles(i, g.Source, source); err == nil {
		t.Fatal("unaudited origin attested")
	}
}
