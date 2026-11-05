package researchweb

import (
	"context"
	"fmt"
	"sort"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
)

type PlanningCostEstimate struct {
	Basis            string `json:"basis"`
	EstimatedChecks  int    `json:"estimated_checks"`
	HistorySamples   int    `json:"history_samples"`
	ExcludedAttempts int    `json:"excluded_attempts,omitempty"`
}

type PlanningCostEstimates struct {
	NewProof         PlanningCostEstimate `json:"new_proof"`
	LemmaApplication PlanningCostEstimate `json:"lemma_application"`
	BFS              PlanningCostEstimate `json:"bfs"`
	FormatRepair     PlanningCostEstimate `json:"format_repair"`
}

type PlanningLibrarySearch struct {
	Status                  string                 `json:"status"`
	Revision                int                    `json:"revision"`
	Candidates              int                    `json:"candidates"`
	NativeChecks            int                    `json:"native_checks"`
	Limit                   int                    `json:"limit"`
	TimeoutSeconds          int                    `json:"timeout_seconds"`
	NewProofEstimatedChecks int                    `json:"new_proof_estimated_checks"`
	HistorySamples          int                    `json:"history_samples"`
	Costs                   *PlanningCostEstimates `json:"costs,omitempty"`
	Matches                 []string               `json:"matches,omitempty"`
}

type PlanningLibraryMatch struct {
	Bindings                 []leancheck.Argument        `json:"bindings,omitempty"`
	Binders                  []leancheck.Binder          `json:"binders,omitempty"`
	Missing                  []leancheck.MissingArgument `json:"missing,omitempty"`
	ID                       string                      `json:"id"`
	Library                  string                      `json:"library"`
	Theorem                  string                      `json:"theorem"`
	Declaration              string                      `json:"declaration"`
	Module                   string                      `json:"module"`
	Source                   string                      `json:"source,omitempty"`
	LibraryGoalSHA256        string                      `json:"library_goal_sha256"`
	Status                   string                      `json:"status"`
	Application              string                      `json:"application,omitempty"`
	CostComparison           string                      `json:"cost_comparison,omitempty"`
	EstimatedRemainingChecks int                         `json:"estimated_remaining_checks"`
	OmittedPremises          int                         `json:"omitted_premises,omitempty"`
}

type PlanningLibraryMaterial struct {
	Search                  *PlanningLibrarySearch `json:"search"`
	Matches                 []PlanningLibraryMatch `json:"matches"`
	CostModel               string                 `json:"cost_model"`
	Costs                   PlanningCostEstimates  `json:"costs"`
	NewProofEstimatedChecks int                    `json:"new_proof_estimated_checks"`
	HistorySamples          int                    `json:"history_samples"`
	Accepted                bool                   `json:"accepted"`
}

func (s *Service) preparePlanningLibrary(ctx context.Context, r PlanningRequest) (int, *PlanningLibrarySearch, bool, error) {
	limit, timeout := r.LibraryLimit, r.LibraryTimeoutSeconds
	if limit == 0 {
		limit = 4
	}
	if timeout == 0 {
		timeout = 60
	}
	if limit < 1 || limit > 8 || timeout < 1 || timeout > 120 {
		return 0, nil, false, RuleError("Поиск лемм перед планированием ограничен 1..8 кандидатами и 1..120 секундами.")
	}
	search := &PlanningLibrarySearch{Revision: r.ExpectedRevision, Limit: limit, TimeoutSeconds: timeout, Matches: []string{}}
	if s.Options.Lean == nil {
		search.Status = "native_unavailable"
		return r.ExpectedRevision, search, false, nil
	}
	v, err := s.Store.Read()
	if err != nil {
		return 0, nil, false, err
	}
	costs := s.planningCostEstimates(&v.Data, r.Target)
	search.Costs = &costs
	search.NewProofEstimatedChecks, search.HistorySamples = costs.NewProof.EstimatedChecks, costs.NewProof.HistorySamples
	for _, application := range v.Applications {
		if application.Target == r.Target && application.State == "candidate" {
			search.Status = "application_pending"
			return v.Revision, search, true, nil
		}
	}
	if v.Revision != r.ExpectedRevision {
		return 0, nil, false, ErrConflict
	}
	if v.Paused {
		return 0, nil, false, RuleError("Новые запуски приостановлены.")
	}
	result, err := s.SearchCatalog(ctx, CatalogSearchRequest{ExpectedRevision: r.ExpectedRevision, RequestID: r.RequestID + "-library", Target: r.Target, Limit: limit, TimeoutSeconds: timeout, Confirm: true})
	if err != nil {
		return 0, nil, false, fmt.Errorf("planning library search: %w", err)
	}
	search.Status, search.Revision, search.Candidates, search.NativeChecks = result.Status, result.Revision, result.Candidates, len(result.Matches)
	for _, match := range result.Matches {
		search.Matches = append(search.Matches, match.ID)
	}
	selected, bestCost := "", costs.NewProof.EstimatedChecks+1
	for _, match := range result.Matches {
		cost := 0
		switch match.Report.Status {
		case "verified":
			cost = 1
		case "obligations":
			cost = 1 + len(match.Report.Missing)
			for _, missing := range match.Report.Missing {
				if missing.Kind != "premise" {
					cost = costs.NewProof.EstimatedChecks + 1
					break
				}
			}
		default:
			continue
		}
		if cost < bestCost {
			selected, bestCost = match.ID, cost
		}
	}
	if selected != "" {
		for _, match := range result.Matches {
			if match.ID != selected {
				continue
			}
			if match.Application == "" {
				if err := s.ApplyLemmaMatch(match.ID, ApplyMatchRequest{ExpectedRevision: result.Revision, RequestID: r.RequestID + "-apply", Confirm: true}); err != nil {
					return 0, nil, false, fmt.Errorf("planning library application: %w", err)
				}
				search.Status = "application_created"
			} else {
				search.Status = "application_pending"
			}
			return result.Revision, search, true, nil
		}
	}
	return result.Revision, search, false, nil
}

func (s *Service) finishPlanningLibrary(r PlanningRequest) error {
	v, err := s.Store.Read()
	if err != nil {
		return err
	}
	e := v.entity(r.Target)
	if e == nil {
		return RuleError("Цель библиотечного применения не найдена.")
	}
	if w := eWorkflow(v.Data, e); w != nil && w.Status == "prepared" {
		s.workflowState(e.Study, "needs_operator", "Проверенная библиотечная лемма выбрана вместо нового плана; проверьте обязательства применения.", nil)
	}
	return nil
}

func eWorkflow(d Data, e *Entity) *ResearchWorkflow {
	if e == nil {
		return nil
	}
	if study := d.study(e.Study); study != nil {
		return study.Workflow
	}
	return nil
}

func librarySearchForAttempt(d Data, a Attempt) *PlanningLibrarySearch {
	if a.LibrarySearch != nil {
		return a.LibrarySearch
	}
	if a.Role != "proof" && a.Role != "formalize" {
		return nil
	}
	for i := len(d.Attempts) - 1; i >= 0; i-- {
		candidate := d.Attempts[i]
		if candidate.Target == a.Target && candidate.TargetRevision == a.TargetRevision && candidate.LibrarySearch != nil {
			return candidate.LibrarySearch
		}
	}
	return nil
}

func (s *Service) planningLibraryMaterial(d Data, a Attempt) *PlanningLibraryMaterial {
	search := librarySearchForAttempt(d, a)
	if search == nil {
		return nil
	}
	costs := s.planningCostEstimates(&d, a.Target)
	if search.Costs != nil {
		costs = *search.Costs
	} else if search.NewProofEstimatedChecks > 0 {
		costs.NewProof.EstimatedChecks = search.NewProofEstimatedChecks
		costs.NewProof.HistorySamples = search.HistorySamples
	}
	out := &PlanningLibraryMaterial{Search: search, Matches: []PlanningLibraryMatch{}, CostModel: "New proof and format repair use only Lean-verified attempts and actual model_requested events, with provider-specific ACP fallback to one Kimi prompt or Coddy usage events. New proof excludes repairs, uses lower median with fallback two and cap 1..4; repairs use fallback one and cap 1..2. Lemma application is one native check plus one check per unproved premise. BFS is one bounded computation. Matches are applied automatically only when their estimated cost does not exceed the snapshotted new-proof estimate; more expensive obligations remain an operator/planner decision. Failed, limited, stale, unverified and unreadable attempts are excluded. These are heuristic costs, not probabilities or guaranteed savings.", Costs: costs, NewProofEstimatedChecks: costs.NewProof.EstimatedChecks, HistorySamples: costs.NewProof.HistorySamples, Accepted: false}
	for _, id := range search.Matches {
		for _, match := range d.LemmaMatches {
			if match.ID != id || !matchCurrent(&d, match) {
				continue
			}
			item := PlanningLibraryMatch{ID: match.ID, Library: match.Library, Status: match.Report.Status, Application: match.Application, Source: match.Report.Source, Bindings: []leancheck.Argument{}, Missing: []leancheck.MissingArgument{}}
			for _, library := range d.Library {
				if library.ID == match.Library {
					item.Theorem = library.Goal.Candidate
					item.Declaration = library.Goal.Declaration
					item.Module = library.Module
					item.LibraryGoalSHA256 = leancheck.Digest(library.Goal)
					break
				}
			}
			for _, signature := range d.LemmaSignatures {
				if signature.Library == match.Library && signature.GoalSHA256 == item.LibraryGoalSHA256 && signature.Native != nil && signature.Native.Status == "verified" {
					item.Binders = append([]leancheck.Binder(nil), signature.Native.Binders...)
					break
				}
			}
			for i, binding := range match.Report.Bindings {
				if i >= 16 {
					break
				}
				binding.Term = execution.Preview(binding.Term, 4000)
				item.Bindings = append(item.Bindings, binding)
			}
			if match.Report.Status == "verified" || match.Report.Status == "obligations" {
				item.EstimatedRemainingChecks = 1 + len(match.Report.Missing)
				switch {
				case item.EstimatedRemainingChecks < costs.NewProof.EstimatedChecks:
					item.CostComparison = "cheaper"
				case item.EstimatedRemainingChecks == costs.NewProof.EstimatedChecks:
					item.CostComparison = "equal"
				default:
					item.CostComparison = "more_expensive"
				}
			}
			for i, missing := range match.Report.Missing {
				if i >= 4 {
					item.OmittedPremises = len(match.Report.Missing) - 4
					break
				}
				missing.Type = execution.Preview(missing.Type, 4000)
				missing.ClosedType = execution.Preview(missing.ClosedType, 4000)
				item.Missing = append(item.Missing, missing)
			}
			out.Matches = append(out.Matches, item)
		}
	}
	return out
}

func (s *Service) planningCostEstimates(d *Data, target string) PlanningCostEstimates {
	return PlanningCostEstimates{
		NewProof:         s.modelRequestEstimate(d, target, false, 2, 4, "verified_proof_model_requests"),
		LemmaApplication: PlanningCostEstimate{EstimatedChecks: 1, Basis: "native_application_protocol"},
		BFS:              PlanningCostEstimate{EstimatedChecks: 1, HistorySamples: s.completedComputationSamples(d, target), Basis: "bounded_computation_protocol"},
		FormatRepair:     s.modelRequestEstimate(d, target, true, 1, 2, "verified_format_repair_model_requests"),
	}
}

func (s *Service) modelRequestEstimate(d *Data, target string, repair bool, fallback, cap int, basis string) PlanningCostEstimate {
	out := PlanningCostEstimate{EstimatedChecks: fallback, Basis: basis}
	costs := []int{}
	for _, attempt := range d.Attempts {
		if attempt.Target != target || (attempt.Role != "proof" && attempt.Role != "formalize") || (attempt.ParentAttempt != "") != repair {
			continue
		}
		verified := false
		if attempt.Status == "candidate" {
			for _, check := range d.Verifications {
				if check.Attempt == attempt.ID && check.Target == target && check.TargetRevision == attempt.TargetRevision && verifiedReportMatches(check) {
					verified = true
					break
				}
			}
		}
		if !verified {
			out.ExcludedAttempts++
			continue
		}
		result, err := s.readResult(attempt)
		if err != nil || !s.safeResultContent(attempt, result) {
			out.ExcludedAttempts++
			continue
		}
		calls := actualModelCalls(attempt, result)
		if calls == 0 {
			out.ExcludedAttempts++
			continue
		}
		costs = append(costs, calls)
	}
	out.HistorySamples = len(costs)
	if len(costs) == 0 {
		return out
	}
	sort.Ints(costs)
	out.EstimatedChecks = costs[(len(costs)-1)/2]
	if out.EstimatedChecks < 1 {
		out.EstimatedChecks = 1
	}
	if out.EstimatedChecks > cap {
		out.EstimatedChecks = cap
	}
	return out
}

func actualModelCalls(a Attempt, result execution.Result) int {
	requested, usage, prompts := 0, 0, 0
	for _, event := range result.Events {
		switch event.Type {
		case "model_requested":
			requested++
		case "usage":
			usage++
		case "acp_prompt_started":
			prompts++
		}
	}
	if requested > 0 {
		return requested
	}
	if a.ProfileConfiguration != nil && a.ProfileConfiguration.External != nil && a.ProfileConfiguration.External.Provider == "kimi-agent" {
		if prompts == 1 {
			return 1
		}
		return 0
	}
	return usage
}

func (s *Service) completedComputationSamples(d *Data, target string) int {
	samples := 0
	for _, attempt := range d.Attempts {
		if attempt.Target != target {
			continue
		}
		for _, summary := range s.computationSummaries(attempt.ID) {
			if summary.Status == "completed" && summary.LimitReason == "" && summary.CertificateSHA256 != "" {
				samples++
			}
		}
	}
	return samples
}

func (s *Service) newProofEstimatedChecks(d *Data, target string) (int, int) {
	estimate := s.modelRequestEstimate(d, target, false, 2, 4, "verified_proof_model_requests")
	return estimate.EstimatedChecks, estimate.HistorySamples
}
