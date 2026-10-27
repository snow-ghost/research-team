//go:build linux

package researchweb

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
	"github.com/snow-ghost/research-team/internal/proofreview"
)

func TestReviewFilesBDD_ActualCompilerInputsAreRequiredForFileAttestation(t *testing.T) {
	s := serviceFor(t, optionsFor(t, "http://127.0.0.1:1/v1"))
	g := leancheck.Goal{Source: "def Statement : Prop := True\n", Declaration: "Statement", Candidate: "Candidate"}
	r := leancheck.Report{Status: "verified", Phase: "complete", AuditSHA256: leancheck.AuditDigest(), GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(submittedProof)}
	i, err := proofreview.NewInput(g, g, submittedProof, r, "proof", "True", "author")
	if err != nil {
		t.Fatal(err)
	}
	i.Assumptions = "Definitions are self-contained"
	legacy, err := s.attachReviewFiles(i, "check-fixture")
	if err != nil || legacy.CheckedFiles != nil {
		t.Fatal("missing files attested", err)
	}
	dir := filepath.Join(s.Options.Config.DataDir, "verifications", "check-fixture", "input")
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = atomicFile(filepath.Join(dir, "Goal.lean"), []byte(g.Source)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.attachReviewFiles(i, "check-fixture"); err == nil {
		t.Fatal("partial file pair accepted")
	}
	if err = atomicFile(filepath.Join(dir, "Candidate.lean"), []byte(submittedProof)); err != nil {
		t.Fatal(err)
	}
	bound, err := s.attachReviewFiles(i, "check-fixture")
	if err != nil || bound.CheckedFiles == nil || bound.Assumptions != i.Assumptions || hash(bound.OriginalGoal) != hash(i.OriginalGoal) {
		t.Fatal("binding failed or assumptions changed", err)
	}
	if err = atomicFile(filepath.Join(dir, "Goal.lean"), []byte(g.Source+"\n")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.attachReviewFiles(i, "check-fixture"); err == nil {
		t.Fatal("modified input accepted")
	}
}
