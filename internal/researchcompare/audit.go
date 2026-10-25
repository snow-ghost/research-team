package researchcompare

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
	"github.com/snow-ghost/research-team/internal/proofreview"
)

type AuditRow struct {
	Case          string         `json:"case"`
	Mode          string         `json:"mode"`
	Repetition    int            `json:"repetition"`
	Status        string         `json:"status"`
	NativeChecked bool           `json:"native_checked"`
	UsageUnknown  bool           `json:"usage_unknown"`
	Failure       string         `json:"failure,omitempty"`
	RoleFailures  map[string]int `json:"role_failures,omitempty"`
	Review        *ReviewAudit   `json:"review,omitempty"`
}

type ReviewAudit struct {
	Rubric     string         `json:"rubric,omitempty"`
	Findings   map[string]int `json:"findings"`
	Blocking   int            `json:"blocking"`
	Unlocated  int            `json:"unlocated"`
	ValidShape bool           `json:"valid_shape"`
}

type AuditReport struct {
	Version      int            `json:"version"`
	ReportSHA256 string         `json:"report_sha256"`
	AuditSHA256  string         `json:"audit_sha256"`
	Counts       map[string]int `json:"counts"`
	Rows         []AuditRow     `json:"rows"`
}

// AuditComparison is read-only. Raw responses remain private and are not copied to the report.
func AuditComparison(dir string) (AuditReport, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return AuditReport{}, err
	}
	defer root.Close()
	read := func(path string, out any) error {
		f, err := root.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, (2<<20)+1))
		if err != nil || len(b) > 2<<20 {
			return fmt.Errorf("audit input exceeds bound")
		}
		return json.Unmarshal(b, out)
	}
	var report Report
	if err := read("report.json", &report); err != nil {
		return AuditReport{}, err
	}
	if report.Version != 2 && report.Version != 3 && report.Version != 4 {
		return AuditReport{}, fmt.Errorf("artifact audit requires comparison version 2 or 3")
	}
	if len(report.Runs) > 96 {
		return AuditReport{}, fmt.Errorf("too many runs")
	}
	result := AuditReport{Version: 1, ReportSHA256: leancheck.Digest(report), AuditSHA256: report.AuditSHA256, Counts: map[string]int{}, Rows: []AuditRow{}}
	cases := map[string]int{}
	for _, run := range report.Runs {
		if !safeAuditName(run.Case) || !safeAuditName(run.Mode) {
			return AuditReport{}, fmt.Errorf("invalid run path")
		}
		if _, ok := cases[run.Case]; !ok {
			cases[run.Case] = len(cases)
		}
		row := AuditRow{Case: run.Case, Mode: run.Mode, Repetition: run.Repetition, Status: run.Status, UsageUnknown: run.UsageIncomplete || run.UnknownRequestCount, RoleFailures: map[string]int{}}
		row.NativeChecked = run.Verification != nil && run.Verification.Status == "verified" && run.Verification.AuditSHA256 == report.AuditSHA256
		work := fmt.Sprintf("%02d-%s-%s-%d", cases[run.Case], run.Case, run.Mode, run.Repetition)
		for _, role := range []string{"proof", "counter", "formalize", "review", "universal", "repair-0", "repair-1", "repair-2", "repair-3", "repair-4", "repair-5"} {
			var r execution.Result
			err := read(filepath.Join(work, role, "result.json"), &r)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				row.RoleFailures["artifact_unreadable"]++
				continue
			}
			if r.Status != "candidate" {
				key := r.Status + ":" + r.FailureCode
				for _, diagnostic := range r.Diagnostics {
					if diagnostic.Source == "model" && diagnostic.Kind == "http_error" {
						var code struct {
							Status int `json:"http_status"`
						}
						if json.Unmarshal(diagnostic.Data, &code) == nil && code.Status > 0 {
							key = fmt.Sprintf("provider_http_%d", code.Status)
						}
					}
				}
				if r.Status == "limit_reached" {
					// The historical record does not distinguish output and tool-step limits.
					key = "limit_unspecified"
					for _, event := range r.Events {
						if event.Type == "model_finish" && event.Status == "length" {
							key = "output_limit"
						}
						if event.Type == "limit_reason" && event.Status == "tool_or_step_limit" {
							key = "tool_or_step_limit"
						}
					}
				}
				row.RoleFailures[key]++
			}
			if role == "review" && r.Status == "candidate" {
				var review proofreview.Report
				valid := execution.DecodeAgentReport(r.Candidate, &review) == nil && proofreview.Validate(review) == nil
				row.Review = &ReviewAudit{Rubric: review.Rubric, ValidShape: valid, Findings: map[string]int{}}
				for _, f := range review.Findings {
					row.Review.Findings[f.Severity]++
					if f.Severity != "editorial" {
						row.Review.Blocking++
						if f.Evidence == "" || f.Location == "" {
							row.Review.Unlocated++
						}
					}
				}
			}
		}
		if run.Error != "" {
			row.Failure = "run_failure"
			switch run.Error {
			case "invalid typed comparison outcome":
				row.Failure = "outcome_protocol"
			case "interrupted_request_not_replayed":
				row.Failure = "interrupted_not_replayed"
			}
			result.Counts[row.Failure]++
		}
		for key, count := range row.RoleFailures {
			result.Counts[key] += count
		}
		if row.NativeChecked {
			result.Counts["native_checked"]++
		}
		if row.UsageUnknown {
			result.Counts["usage_unknown"]++
		}
		result.Rows = append(result.Rows, row)
	}
	return result, nil
}

func safeAuditName(s string) bool {
	if s == "" || len(s) > 100 {
		return false
	}
	for _, r := range s {
		if r != '-' && r != '_' && !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}
