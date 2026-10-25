package researchcompare

import (
	"fmt"
	"slices"
	"strings"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

type ModeSummary struct {
	Runs              int     `json:"runs"`
	NativeVerified    int     `json:"native_verified"`
	Reviewed          int     `json:"reviewed"`
	Failed            int     `json:"failed"`
	Requests          int     `json:"requests"`
	ChargedRequests   int     `json:"charged_requests"`
	KnownOutputTokens int64   `json:"known_output_tokens"`
	IncompleteUsage   int     `json:"incomplete_usage"`
	Seconds           float64 `json:"seconds"`
}
type ComparisonSummary struct {
	Simulation         bool                   `json:"simulation,omitempty"`
	ReportSHA256       string                 `json:"report_object_sha256"`
	RecordedRuns       int                    `json:"recorded_runs"`
	TerminalRuns       int                    `json:"terminal_runs"`
	ExpectedRuns       int                    `json:"expected_runs"`
	Complete           bool                   `json:"complete"`
	PairedGroups       int                    `json:"paired_groups"`
	ExcludedGroups     []string               `json:"excluded_groups"`
	All                map[string]ModeSummary `json:"all_recorded"`
	Paired             map[string]ModeSummary `json:"paired"`
	OperatorAcceptance bool                   `json:"operator_acceptance"`
}

// Pair by the same goal and repetition, excluding missing or externally interrupted arms.
func Summarize(r Report) (ComparisonSummary, error) {
	s := ComparisonSummary{ReportSHA256: leancheck.Digest(r), RecordedRuns: len(r.Runs), ExpectedRuns: 48, All: map[string]ModeSummary{}, Paired: map[string]ModeSummary{}, ExcludedGroups: []string{}}
	s.Simulation = r.Simulation
	maxRequests := 216
	if r.Version == 4 {
		maxRequests, s.ExpectedRuns = 432, 96
		if r.PolicySHA256 != EvaluationPolicyDigest() || r.EvaluationSet == nil || r.CaseSetSHA256 != leancheck.Digest(*r.EvaluationSet) {
			return s, fmt.Errorf("evaluation policy or case set missing")
		}
		if _, err := PrepareEvaluation(*r.EvaluationSet); err != nil {
			return s, err
		}
	}
	if (r.Version != 2 && r.Version != 3 && r.Version != 4) || r.MaxRequests != maxRequests {
		return s, fmt.Errorf("summary requires controlled comparison")
	}
	third := "adaptive"
	if r.Version >= 3 {
		third = "demand"
	}
	modes := []string{"single", "team", third, "lean"}
	groups := map[string]map[string]Run{}
	add := func(out map[string]ModeSummary, run Run) {
		m := out[run.Mode]
		m.Runs++
		m.Requests += run.MeasuredRequests
		m.ChargedRequests += run.RequestUpperBound
		m.Seconds += run.Seconds
		if run.Verification != nil && run.Verification.Status == "verified" {
			m.NativeVerified++
		}
		if run.ReviewAccepted {
			m.Reviewed++
		}
		if run.Status == "failed" {
			m.Failed++
		}
		if run.UsageIncomplete {
			m.IncompleteUsage++
		} else {
			m.KnownOutputTokens += run.OutputTokens
		}
		out[run.Mode] = m
	}
	requests := 0
	for _, run := range r.Runs {
		if r.Version == 4 {
			valid := false
			for _, c := range r.EvaluationSet.Cases {
				valid = valid || c.ID == run.Case && c.Class == run.Class && leancheck.Digest(c.Goal) == run.GoalSHA256
			}
			if !valid {
				return s, fmt.Errorf("run does not match evaluation manifest")
			}
		}
		if !slices.Contains(modes, run.Mode) || run.Repetition < 1 || run.Repetition > 2 || run.RequestUpperBound < 0 || run.RequestUpperBound > 6 || run.MeasuredRequests < 0 || run.MeasuredRequests > run.RequestUpperBound || run.Seconds < 0 || run.InputTokens < 0 || run.OutputTokens < 0 || (run.Mode == "lean" && run.RequestUpperBound != 0) {
			return s, fmt.Errorf("invalid comparison measurements")
		}
		key := fmt.Sprintf("%s:%d", run.Case, run.Repetition)
		if groups[key] == nil {
			groups[key] = map[string]Run{}
		}
		if _, exists := groups[key][run.Mode]; exists {
			return s, fmt.Errorf("duplicate comparison arm")
		}
		for _, other := range groups[key] {
			if other.GoalSHA256 != run.GoalSHA256 {
				return s, fmt.Errorf("paired goals differ")
			}
		}
		groups[key][run.Mode] = run
		requests += run.RequestUpperBound
		if run.Status != "running" {
			s.TerminalRuns++
			add(s.All, run)
		}
	}
	if requests > r.MaxRequests {
		return s, fmt.Errorf("comparison budget exceeded")
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		group := groups[key]
		valid := len(group) == 4
		for _, run := range group {
			if run.Status == "running" || run.TimeIncomplete || strings.Contains(run.Error, "interrupted_request") {
				valid = false
			}
			for _, stage := range run.Stages {
				if stage.Status == "cancelled" {
					valid = false
				}
			}
		}
		if !valid {
			s.ExcludedGroups = append(s.ExcludedGroups, key)
			continue
		}
		s.PairedGroups++
		for _, mode := range modes {
			add(s.Paired, group[mode])
		}
	}
	slices.Sort(s.ExcludedGroups)
	s.Complete = len(r.Runs) == s.ExpectedRuns && s.TerminalRuns == s.ExpectedRuns && len(groups) == s.ExpectedRuns/4
	return s, nil
}
