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
	ProcessUnit         string `json:"process_unit,omitempty"`
	ProgressFile        string `json:"progress_file,omitempty"`
	ProcessFile         string `json:"process_file,omitempty"`
	StaleSeconds        int    `json:"stale_seconds,omitempty"`
	ID                  string `json:"id"`
	Label               string `json:"label"`
	File                string `json:"file"`
	ConfigurationSHA256 string `json:"configuration_sha256"`
}

type EvaluationStatus struct {
	CanStop           bool                               `json:"can_stop"`
	Health            *EvaluationHealth                  `json:"health,omitempty"`
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
	LastRecordedRole   string  `json:"last_recorded_role,omitempty"`
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
		if w.ProcessUnit != "" && !evaluationUnit.MatchString(w.ProcessUnit) {
			return RuleError("Остановка разрешена только отдельной службе исследовательского сравнения.")
		}
		if !filepath.IsAbs(w.File) {
			w.File = filepath.Join(base, w.File)
		}
		w.File = filepath.Clean(w.File)
		rel, err := filepath.Rel(c.WebDir, w.File)
		if err != nil || rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return RuleError("Отчет сравнения должен находиться вне веб-каталога.")
		}
		if w.StaleSeconds != 0 && (w.StaleSeconds < 60 || w.StaleSeconds > 3600) {
			return RuleError("Предел отсутствия записей должен быть от 60 до 3600 секунд.")
		}
		for _, path := range []*string{&w.ProgressFile, &w.ProcessFile} {
			if *path == "" {
				continue
			}
			if !filepath.IsAbs(*path) {
				*path = filepath.Join(base, *path)
			}
			*path = filepath.Clean(*path)
			rel, err := filepath.Rel(c.WebDir, *path)
			if err != nil || rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return RuleError("Сведения наблюдения должны быть вне веб-каталога.")
			}
		}
	}
	return nil
}

func (s *Service) EvaluationStatuses() []EvaluationStatus {
	items := []EvaluationStatus{}
	for _, w := range s.Options.Config.Evaluations {
		x := EvaluationStatus{ID: w.ID, Label: w.Label, Status: "not_started", Runs: []EvaluationRunStatus{}, CanStop: w.ProcessUnit != ""}
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
		x.Health = evaluationHealth(w, x.UpdatedAt, summary.Complete, time.Now().UTC())
		for _, r := range report.Runs {
			last := ""
			if len(r.Stages) > 0 {
				last = r.Stages[len(r.Stages)-1].Role
			}
			x.Runs = append(x.Runs, EvaluationRunStatus{LastRecordedRole: last, Case: r.Case, Class: r.Class, Mode: r.Mode, Repetition: r.Repetition, Status: r.Status, MathematicalStatus: r.MathematicalStatus, UsageIncomplete: r.UsageIncomplete, RequestUpperBound: r.RequestUpperBound, MeasuredRequests: r.MeasuredRequests, Seconds: r.Seconds})
			x.RequestUpperBound += r.RequestUpperBound
			x.MeasuredRequests += r.MeasuredRequests
		}
		items = append(items, x)
	}
	return items
}
