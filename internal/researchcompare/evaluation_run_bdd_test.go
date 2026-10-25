package researchcompare

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func evaluationFixture(t *testing.T) EvaluationSet {
	t.Helper()
	b, err := os.ReadFile("../../examples/evaluation-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var set EvaluationSet
	if err := json.Unmarshal(b, &set); err != nil {
		t.Fatal(err)
	}
	return set
}

func TestEvaluationV4BDD_FullScheduleAndResumeWithoutModelOrLean(t *testing.T) {
	set := evaluationFixture(t)
	dir := filepath.Join(t.TempDir(), "rehearsal")
	r, err := RehearseEvaluation(context.Background(), set, dir, false)
	if err != nil || !r.Simulation || r.ModelCalls != 0 || r.MathematicalVerification || r.Summary.RecordedRuns != 96 || !r.Summary.Complete || r.Summary.PairedGroups != 24 || r.SimulatedRequests != 216 || !r.Summary.Simulation {
		t.Fatal(r, err)
	}
	if _, err := AuditComparison(dir); err != nil {
		t.Fatal(err)
	}
	r, err = RehearseEvaluation(context.Background(), set, dir, true)
	if err != nil || r.SimulatedRequests != 0 || r.Summary.RecordedRuns != 96 {
		t.Fatal("replay on resume", r, err)
	}
	set.Cases[0].Goal.Source += "\n-- changed"
	if _, err := RehearseEvaluation(context.Background(), set, dir, true); err == nil {
		t.Fatal("changed goals resumed")
	}
}

func TestEvaluationV4BDD_MissingAuditAndPolicyCannotBecomeComparable(t *testing.T) {
	if _, err := Summarize(Report{Version: 4, MaxRequests: 432}); err == nil {
		t.Fatal("unbound evaluation summarized")
	}
}
