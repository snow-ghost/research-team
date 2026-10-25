package researchcompare

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
	"github.com/snow-ghost/research-team/internal/proofreview"
)

func ReadReport(path string) (Report, error) {
	var r Report
	f, err := os.Open(path)
	if err != nil {
		return r, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return r, fmt.Errorf("comparison report exceeds bound or is not regular")
	}
	b, err := io.ReadAll(io.LimitReader(f, (8<<20)+1))
	if err != nil || len(b) > 8<<20 {
		return r, fmt.Errorf("comparison report exceeds bound")
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return r, err
	}
	return r, nil
}

type Breakdown struct {
	All    map[string]ModeSummary `json:"all"`
	Paired map[string]ModeSummary `json:"paired"`
}
type TaskEffect struct {
	Case                      string  `json:"case"`
	Class                     string  `json:"class"`
	Baseline                  string  `json:"baseline"`
	Mode                      string  `json:"mode"`
	Pairs                     int     `json:"pairs"`
	ReviewedSuccessDifference float64 `json:"reviewed_success_difference"`
	RequestDifference         float64 `json:"request_difference"`
	SecondsDifference         float64 `json:"seconds_difference"`
}
type ReviewGap struct {
	Case           string                `json:"case"`
	Mode           string                `json:"mode"`
	Repetition     int                   `json:"repetition"`
	ReviewStatus   string                `json:"review_status"`
	FailureCode    string                `json:"failure_code,omitempty"`
	ArtifactStatus string                `json:"artifact_status"`
	Findings       []proofreview.Finding `json:"findings,omitempty"`
}
type Analysis struct {
	Summary            ComparisonSummary    `json:"summary"`
	Preliminary        bool                 `json:"preliminary"`
	Classes            map[string]Breakdown `json:"classes"`
	Cases              map[string]Breakdown `json:"cases"`
	Effects            []TaskEffect         `json:"task_effects"`
	ReviewGaps         []ReviewGap          `json:"review_gaps"`
	IndependentTasks   int                  `json:"independent_tasks"`
	RepeatObservations int                  `json:"repeat_observations"`
	Inference          string               `json:"inference"`
}

// Effects are averaged within each task; repetitions are not independent tasks.
// No confidence interval or superiority claim is inferred from this small set.
func Analyze(r Report, artifacts string) (Analysis, error) {
	s, err := Summarize(r)
	if err != nil {
		return Analysis{}, err
	}
	a := Analysis{Summary: s, Preliminary: !s.Complete, Classes: map[string]Breakdown{}, Cases: map[string]Breakdown{}, Effects: []TaskEffect{}, ReviewGaps: []ReviewGap{}, Inference: "descriptive_only;repetitions_clustered_by_task;no_superiority_claim"}
	byCase, byClass := map[string][]Run{}, map[string][]Run{}
	for _, run := range r.Runs {
		byCase[run.Case] = append(byCase[run.Case], run)
		byClass[run.Class] = append(byClass[run.Class], run)
	}
	for _, entry := range []struct {
		groups map[string][]Run
		out    map[string]Breakdown
	}{{byCase, a.Cases}, {byClass, a.Classes}} {
		for name, runs := range entry.groups {
			sub := r
			sub.Runs = runs
			summary, err := Summarize(sub)
			if err != nil {
				return a, err
			}
			entry.out[name] = Breakdown{All: summary.All, Paired: summary.Paired}
		}
	}
	excluded := map[string]bool{}
	for _, key := range s.ExcludedGroups {
		excluded[key] = true
	}
	caseIDs := []string{}
	for id := range byCase {
		caseIDs = append(caseIDs, id)
	}
	slices.Sort(caseIDs)
	third := "demand"
	if r.Version == 2 {
		third = "adaptive"
	}
	for _, id := range caseIDs {
		groups := map[int]map[string]Run{}
		for _, run := range byCase[id] {
			if groups[run.Repetition] == nil {
				groups[run.Repetition] = map[string]Run{}
			}
			groups[run.Repetition][run.Mode] = run
		}
		counted := false
		for _, mode := range []string{"team", third} {
			effect := TaskEffect{Case: id, Class: byCase[id][0].Class, Baseline: "single", Mode: mode}
			for _, rep := range []int{1, 2} {
				group := groups[rep]
				if excluded[fmt.Sprintf("%s:%d", id, rep)] || len(group) != 4 {
					continue
				}
				x, y := group[mode], group["single"]
				effect.Pairs++
				a.RepeatObservations++
				if mode == "team" {
					counted = true
				}
				effect.ReviewedSuccessDifference += float64(success(x) - success(y))
				effect.RequestDifference += float64(x.RequestUpperBound - y.RequestUpperBound)
				effect.SecondsDifference += x.Seconds - y.Seconds
			}
			if effect.Pairs > 0 {
				n := float64(effect.Pairs)
				effect.ReviewedSuccessDifference /= n
				effect.RequestDifference /= n
				effect.SecondsDifference /= n
				a.Effects = append(a.Effects, effect)
			}
		}
		if counted {
			a.IndependentTasks++
		}
	}
	a.RepeatObservations /= 2
	var root *os.Root
	if artifacts != "" {
		root, err = os.OpenRoot(artifacts)
		if err != nil {
			return a, err
		}
		defer root.Close()
	}
	for _, run := range r.Runs {
		if run.Mode == "lean" || run.Status == "running" || run.Verification == nil || run.Verification.Status != "verified" || run.ReviewAccepted {
			continue
		}
		gap := ReviewGap{Case: run.Case, Mode: run.Mode, Repetition: run.Repetition, ReviewStatus: "not_recorded", ArtifactStatus: "not_requested"}
		for _, stage := range run.Stages {
			if stage.Role == "review" {
				gap.ReviewStatus, gap.FailureCode = stage.Status, stage.FailureCode
				if root != nil {
					gap.ArtifactStatus = "unavailable"
					if review, status := boundReview(root, r, run, stage); status != "unavailable" {
						gap.ArtifactStatus = status
						if status == "verified_binding" {
							gap.Findings = review.Findings
						} else {
							gap.ReviewStatus = "invalid_report"
						}
					}
				}
			}
		}
		a.ReviewGaps = append(a.ReviewGaps, gap)
	}
	return a, nil
}

func success(r Run) int {
	if r.Verification != nil && r.Verification.Status == "verified" && r.ReviewAccepted {
		return 1
	}
	return 0
}

func boundReview(root *os.Root, r Report, run Run, stage StageObservation) (proofreview.Report, string) {
	if !safeAuditName(run.Case) || !safeAuditName(run.Mode) {
		return proofreview.Report{}, "unavailable"
	}
	index := -1
	if r.EvaluationSet != nil {
		for i, c := range r.EvaluationSet.Cases {
			if c.ID == run.Case {
				index = i
			}
		}
	} else {
		seen := map[string]bool{}
		for _, x := range r.Runs {
			if !seen[x.Case] {
				index++
				seen[x.Case] = true
			}
			if x.Case == run.Case {
				break
			}
		}
	}
	if index < 0 {
		return proofreview.Report{}, "unavailable"
	}
	f, err := root.Open(fmt.Sprintf("%02d-%s-%s-%d/review/result.json", index, run.Case, run.Mode, run.Repetition))
	if err != nil {
		return proofreview.Report{}, "unavailable"
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (2<<20)+1))
	if err != nil || len(b) > 2<<20 {
		return proofreview.Report{}, "unavailable"
	}
	var result execution.Result
	var review proofreview.Report
	if json.Unmarshal(b, &result) != nil || leancheck.Digest(result) != stage.ResultSHA256 {
		return review, "unavailable"
	}
	if result.Status != "candidate" || execution.DecodeAgentReport(result.Candidate, &review) != nil || proofreview.Validate(review) != nil {
		return review, "bound_invalid_contract"
	}
	return review, "verified_binding"
}
