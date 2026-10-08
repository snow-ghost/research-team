package researchweb

import (
	"fmt"
	"regexp"
	"time"

	"github.com/snow-ghost/research-team/internal/researchcompare"
)

type ComparisonRecord struct {
	ID           string                 `json:"id"`
	ReportSHA256 string                 `json:"report_sha256"`
	Report       researchcompare.Report `json:"report"`
	CreatedAt    time.Time              `json:"created_at"`
}
type ComparisonRequest struct {
	ExpectedRevision int                    `json:"expected_revision"`
	RequestID        string                 `json:"request_id"`
	Report           researchcompare.Report `json:"report"`
	Confirm          bool                   `json:"confirm"`
}

func (s *Service) ImportComparison(r ComparisonRequest) error {
	version2 := r.Report.Version == 2
	maxRequests, count, perRun := 96, 12, 8
	if version2 {
		maxRequests, count, perRun = 216, 48, 6
	}
	if !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || (r.Report.Version != 1 && !version2) || r.Report.MaxRequests != maxRequests || len(r.Report.Runs) != count || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(r.Report.ConfigurationSHA256) {
		return RuleError("Нужен завершенный отчет сравнения согласованного набора.")
	}
	seen := map[string]bool{}
	cases := map[string]string{}
	requests := 0
	for _, run := range r.Report.Runs {
		key := run.Case + ":" + run.Mode
		modeOK := run.Mode == "single" || run.Mode == "team"
		if version2 {
			key += fmt.Sprint(":", run.Repetition)
			modeOK = modeOK || run.Mode == "adaptive" || run.Mode == "lean"
			if run.Repetition < 1 || run.Repetition > 2 || !textOK(run.Class, 100) || (run.Mode == "lean" && (run.RequestUpperBound != 0 || run.MeasuredRequests != 0)) || run.MeasuredRequests < 0 || run.MeasuredRequests > run.RequestUpperBound || r.Report.AuditSHA256 == "" {
				return RuleError("Некорректные повторения или контрольная группа.")
			}
		}
		if !textOK(run.Case, 100) || !modeOK || seen[key] || run.RequestUpperBound < 0 || run.RequestUpperBound > perRun || run.Seconds < 0 || run.InputTokens < 0 || run.OutputTokens < 0 || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(run.GoalSHA256) {
			return RuleError("Некорректные измерения сравнения.")
		}
		if run.Status != "failed" && run.Status != "inconclusive" && run.Status != "requires_review" && run.Status != "verified_not_accepted" && !(version2 && run.Mode == "lean" && run.Status == "verified_baseline_not_accepted") {
			return RuleError("Опыт еще не завершен.")
		}
		if previous := cases[run.Case]; previous != "" && previous != run.GoalSHA256 {
			return RuleError("Режимы сравнивали разные цели.")
		}
		seen[key] = true
		cases[run.Case] = run.GoalSHA256
		requests += run.RequestUpperBound
	}
	if len(cases) != 6 || requests > maxRequests {
		return RuleError("Нарушен состав или бюджет сравнения.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Записан отчет сравнения", "", "operator", func(d *Data) error {
		digest := hash(r.Report)
		for _, old := range d.Comparisons {
			if old.ReportSHA256 == digest {
				return RuleError("Этот отчет уже записан.")
			}
		}
		d.Comparisons = append(d.Comparisons, ComparisonRecord{ID: identifier("comparison"), ReportSHA256: digest, Report: r.Report, CreatedAt: time.Now().UTC()})
		return nil
	})
}
