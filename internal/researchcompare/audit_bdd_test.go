package researchcompare

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snow-ghost/research-team/internal/execution"
)

func TestAuditBDD_ReadOnlyFailuresRemainSeparateFromNativeChecks(t *testing.T) {
	dir := t.TempDir()
	r := Report{Version: 2, AuditSHA256: "pinned", Runs: []Run{{Case: "first", Mode: "team", Repetition: 1, Status: "failed", Error: "executor failed; see private result diagnostics", UsageIncomplete: true}}}
	write := func(path string, v any) {
		t.Helper()
		b, _ := json.Marshal(v)
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "report.json"), r)
	before, _ := os.ReadFile(filepath.Join(dir, "report.json"))
	work := filepath.Join(dir, "00-first-team-1", "review")
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(work, "result.json"), execution.Result{Status: "timed_out", FailureCode: "deadline_exceeded", Partial: "private response"})
	a, err := AuditComparison(dir)
	after, _ := os.ReadFile(filepath.Join(dir, "report.json"))
	if err != nil || a.Counts["timed_out:deadline_exceeded"] != 1 || a.Counts["usage_unknown"] != 1 || string(before) != string(after) {
		t.Fatal(a, err)
	}
	encoded, _ := json.Marshal(a)
	if strings.Contains(string(encoded), "private response") {
		t.Fatal("raw response leaked")
	}
	r.Runs[0].Case = "../escape"
	write(filepath.Join(dir, "report.json"), r)
	if _, err := AuditComparison(dir); err == nil {
		t.Fatal("path traversal accepted")
	}
}
