package researchcompare

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestSummaryBDD_IncompleteMatrixAndCancelledRequestsDoNotBecomeEqualComparison(t *testing.T) {
	r := Report{Version: 3, MaxRequests: 216}
	for _, mode := range []string{"single", "team", "demand", "lean"} {
		requests := 1
		if mode == "lean" {
			requests = 0
		}
		r.Runs = append(r.Runs, Run{Case: "one", Mode: mode, Repetition: 1, GoalSHA256: "same", Status: "failed", RequestUpperBound: requests, MeasuredRequests: requests, UsageIncomplete: true, Verification: &leancheck.Report{Status: "verified"}})
	}
	r.Runs = append(r.Runs, Run{Case: "two", Mode: "single", Repetition: 1, GoalSHA256: "other", Status: "failed", Stages: []StageObservation{{Status: "cancelled"}}})
	s, err := Summarize(r)
	if err != nil || s.Complete || s.PairedGroups != 1 || len(s.ExcludedGroups) != 1 || s.Paired["single"].IncompleteUsage != 1 || s.Paired["single"].NativeVerified != 1 || s.OperatorAcceptance {
		t.Fatal(s, err)
	}
	r.Runs[0].GoalSHA256 = "changed"
	if _, err := Summarize(r); err == nil {
		t.Fatal("different goals compared")
	}
}

func TestSummaryBDD_RepeatedAnalysisHasStableFloatingPointTotals(t *testing.T) {
	r := Report{Version: 3, MaxRequests: 216}
	for i, seconds := range []float64{1e16, 1, 1} {
		for _, mode := range []string{"single", "team", "demand", "lean"} {
			r.Runs = append(r.Runs, Run{Case: fmt.Sprint(i), Mode: mode, Repetition: 1, GoalSHA256: "same", Status: "failed", Seconds: seconds})
		}
	}
	var first string
	for i := 0; i < 40; i++ {
		summary, err := Summarize(r)
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(summary)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = string(data)
		} else if string(data) != first {
			t.Fatal("identical report produced different totals")
		}
	}
}
