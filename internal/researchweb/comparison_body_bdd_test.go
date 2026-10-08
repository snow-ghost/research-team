package researchweb

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/snow-ghost/research-team/internal/leancheck"
	"github.com/snow-ghost/research-team/internal/researchcompare"
)

func TestComparisonBDD_ExpandedBodyDoesNotWidenOtherCommands(t *testing.T) {
	body, _ := json.Marshal(map[string]string{"report": strings.Repeat("x", 300<<10)})
	request := func() *httptest.ResponseRecorder { return httptest.NewRecorder() }
	makeBody := func(limit int64) error {
		r := httptest.NewRequest("POST", "/api/comparisons", strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/json")
		var result map[string]string
		if limit == 0 {
			return bodyJSON(request(), r, &result)
		}
		return bodyJSONLimit(request(), r, &result, limit)
	}
	if !errors.Is(makeBody(0), ErrLimit) {
		t.Fatal("ordinary command size bound widened")
	}
	if err := makeBody(2 << 20); err != nil {
		t.Fatal("completed comparison body rejected", err)
	}
}

func TestComparisonBDD_ControlledReportImportsWithoutAcceptingClaims(t *testing.T) {
	s := serviceFor(t, optionsFor(t, "http://127.0.0.1:1/v1"))
	report := researchcompare.Report{Version: 2, MaxRequests: 216, ConfigurationSHA256: strings.Repeat("b", 64), AuditSHA256: leancheck.AuditDigest(), StartedAt: time.Now().UTC()}
	for item := 0; item < 6; item++ {
		for repetition := 1; repetition <= 2; repetition++ {
			for _, mode := range []string{"single", "team", "adaptive", "lean"} {
				calls := 1
				if mode == "lean" {
					calls = 0
				}
				report.Runs = append(report.Runs, researchcompare.Run{Case: fmt.Sprintf("case-%d", item), Class: fmt.Sprintf("class-%d", item), Repetition: repetition, Mode: mode, GoalSHA256: strings.Repeat("a", 64), Status: "inconclusive", RequestUpperBound: calls, MeasuredRequests: calls, Seconds: 1})
			}
		}
	}
	v := stateOf(t, s)
	if err := s.ImportComparison(ComparisonRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Confirm: true, Report: report}); err != nil {
		t.Fatal(err)
	}
	after := stateOf(t, s)
	if len(after.Comparisons) != 1 || len(after.Entities) != len(v.Entities) || len(after.Verifications) != len(v.Verifications) {
		t.Fatal("comparison altered proof records")
	}
}
