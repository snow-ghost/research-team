package researchweb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/snow-ghost/research-team/internal/researchcompare"
)

type EvaluationWatch struct {
	ID                  string `json:"id"`
	Label               string `json:"label"`
	File                string `json:"file"`
	ConfigurationSHA256 string `json:"configuration_sha256"`
}

type EvaluationStatus struct {
	ID                string                             `json:"id"`
	Label             string                             `json:"label"`
	Status            string                             `json:"status"`
	UpdatedAt         *time.Time                         `json:"updated_at,omitempty"`
	Summary           *researchcompare.ComparisonSummary `json:"summary,omitempty"`
	Runs              []EvaluationRunStatus              `json:"runs"`
	RequestUpperBound int                                `json:"request_upper_bound"`
	MeasuredRequests  int                                `json:"measured_requests"`
	MaxRequests       int                                `json:"max_requests"`
}
type EvaluationRunStatus struct {
	Case               string  `json:"case"`
	Class              string  `json:"class"`
	Mode               string  `json:"mode"`
	Repetition         int     `json:"repetition"`
	Status             string  `json:"status"`
	MathematicalStatus string  `json:"mathematical_status,omitempty"`
	UsageIncomplete    bool    `json:"usage_incomplete"`
	RequestUpperBound  int     `json:"request_upper_bound"`
	MeasuredRequests   int     `json:"measured_requests"`
	Seconds            float64 `json:"seconds"`
}

func resolveEvaluationWatches(c *Config, base string) error {
	if len(c.Evaluations) > 10 {
		return ErrLimit
	}
	seen := map[string]bool{}
	for i := range c.Evaluations {
		w := &c.Evaluations[i]
		if !requestPattern.MatchString(w.ID) || seen[w.ID] || !textOK(w.Label, 200) || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(w.ConfigurationSHA256) || w.File == "" {
			return RuleError("Неверные настройки наблюдения сравнения.")
		}
		seen[w.ID] = true
		if !filepath.IsAbs(w.File) {
			w.File = filepath.Join(base, w.File)
		}
		w.File = filepath.Clean(w.File)
		rel, err := filepath.Rel(c.WebDir, w.File)
		if err != nil || rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return RuleError("Отчет сравнения должен находиться вне веб-каталога.")
		}
	}
	return nil
}

func (s *Service) EvaluationStatuses() []EvaluationStatus {
	items := []EvaluationStatus{}
	for _, w := range s.Options.Config.Evaluations {
		x := EvaluationStatus{ID: w.ID, Label: w.Label, Status: "not_started", Runs: []EvaluationRunStatus{}}
		body, err := readBoundedFile(w.File, 8<<20)
		if err != nil {
			if !os.IsNotExist(err) {
				x.Status = "unavailable"
			}
			items = append(items, x)
			continue
		}
		var report researchcompare.Report
		if json.Unmarshal(body, &report) != nil || report.Version != 4 || report.Simulation || report.ConfigurationSHA256 != w.ConfigurationSHA256 {
			x.Status = "invalid"
			items = append(items, x)
			continue
		}
		summary, err := researchcompare.Summarize(report)
		if err != nil {
			x.Status = "invalid"
			items = append(items, x)
			continue
		}
		x.Status = "running"
		if summary.Complete {
			x.Status = "completed"
		}
		x.Summary = &summary
		x.MaxRequests = report.MaxRequests
		if info, err := os.Stat(w.File); err == nil {
			at := info.ModTime().UTC()
			x.UpdatedAt = &at
		}
		for _, r := range report.Runs {
			x.Runs = append(x.Runs, EvaluationRunStatus{Case: r.Case, Class: r.Class, Mode: r.Mode, Repetition: r.Repetition, Status: r.Status, MathematicalStatus: r.MathematicalStatus, UsageIncomplete: r.UsageIncomplete, RequestUpperBound: r.RequestUpperBound, MeasuredRequests: r.MeasuredRequests, Seconds: r.Seconds})
			x.RequestUpperBound += r.RequestUpperBound
			x.MeasuredRequests += r.MeasuredRequests
		}
		items = append(items, x)
	}
	return items
}
