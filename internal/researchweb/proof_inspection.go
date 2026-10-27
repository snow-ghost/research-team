package researchweb

import (
	"net/http"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/proofreview"
)

type InspectionGate struct {
	Code   string `json:"code"`
	Label  string `json:"label"`
	Status string `json:"status"`
}
type InspectionDependency struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Revision     int    `json:"revision"`
	Status       string `json:"status"`
	LibraryReady bool   `json:"library_ready"`
}
type InspectionReview struct {
	Attempt string        `json:"attempt"`
	Status  string        `json:"status"`
	Report  *ReviewReport `json:"report,omitempty"`
}
type InspectionAttempt struct {
	ID           string                 `json:"id"`
	Role         string                 `json:"role"`
	Status       string                 `json:"status"`
	Outcome      string                 `json:"remote_outcome"`
	FailureClass string                 `json:"failure_class,omitempty"`
	Diagnostics  []execution.Diagnostic `json:"diagnostics,omitempty"`
}
type ProofInspection struct {
	Snapshot     int                       `json:"snapshot"`
	Target       Entity                    `json:"target"`
	ClaimKind    string                    `json:"claim_kind"`
	Verification *Verification             `json:"verification,omitempty"`
	Files        *proofreview.CheckedFiles `json:"checked_files,omitempty"`
	FileError    string                    `json:"file_error,omitempty"`
	Result       *ResearchResult           `json:"result,omitempty"`
	Gates        []InspectionGate          `json:"gates"`
	Findings     []Finding                 `json:"findings"`
	Reviews      []InspectionReview        `json:"reviews"`
	Dependencies []InspectionDependency    `json:"dependencies"`
	Attempts     []InspectionAttempt       `json:"attempts"`
	Budget       BudgetObservation         `json:"budget"`
	Execution    ExecutionHealth           `json:"execution"`
}

func (s *Service) InspectProof(target string) (ProofInspection, error) {
	v, err := s.Store.Read()
	if err != nil {
		return ProofInspection{}, err
	}
	e := v.entity(target)
	if e == nil {
		return ProofInspection{}, RuleError("Утверждение не найдено.")
	}
	out := ProofInspection{Snapshot: v.Revision, Target: *e, ClaimKind: "proof", Gates: []InspectionGate{}, Findings: []Finding{}, Reviews: []InspectionReview{}, Dependencies: []InspectionDependency{}, Attempts: []InspectionAttempt{}, Budget: s.observeBudget(&v.Data, e.Study), Execution: s.ExecutionHealth()}
	check := proofVerification(&v.Data, e)
	if check == nil {
		for n := len(v.Verifications) - 1; n >= 0; n-- {
			if v.Verifications[n].Target == target {
				check = &v.Verifications[n]
				break
			}
		}
	}
	nativeStatus, fileStatus := "pending", "not_recorded"
	if check != nil {
		out.Verification = check
		nativeStatus = check.Status
		if !verificationMatches(&v.Data, *check) {
			nativeStatus = "stale"
		}
		if check.Purpose == "refutation" {
			out.ClaimKind = "refutation"
		}
		if nativeStatus == "verified" && e.FormalGoal != nil && check.Report != nil {
			input, inputErr := proofreview.NewInput(*e.FormalGoal, check.Goal, check.Source, *check.Report, out.ClaimKind, e.Statement, check.Author)
			if inputErr == nil {
				input, inputErr = s.attachReviewFiles(input, check.ID)
				if inputErr == nil {
					out.Files = input.CheckedFiles
					if out.Files != nil {
						fileStatus = "verified"
					}
				}
			}
			if inputErr != nil {
				fileStatus = "failed"
				out.FileError = "Входные файлы не соответствуют закрепленной проверке."
			}
		}
	}
	for n := range v.Results {
		if v.Results[n].ID == e.ResearchResult {
			out.Result = &v.Results[n]
			break
		}
	}
	blocking := false
	for _, f := range v.Findings {
		if f.Target == target {
			out.Findings = append(out.Findings, f)
			blocking = blocking || f.Severity != "editorial" && f.State != "resolved"
		}
	}
	reviewStatus, counterStatus := "pending", "pending"
	if out.Result != nil {
		reviewStatus = out.Result.Status
		counterStatus = out.Result.Counter.Outcome
	}
	if blocking {
		reviewStatus = "needs_changes"
	}
	out.Gates = []InspectionGate{{"native", "Проверка Lean", nativeStatus}, {"files", "Связь файлов с целью", fileStatus}, {"counterexamples", "Контрпримеры", counterStatus}, {"review", "Рецензия и соответствие постановке", reviewStatus}, {"decision", "Решение оператора", e.Status}}
	for _, id := range e.Dependencies {
		if dep := v.entity(id); dep != nil {
			item := InspectionDependency{ID: id, Title: dep.Title, Revision: dep.Revision, Status: v.effective(id, map[string]bool{})}
			for _, l := range v.Library {
				item.LibraryReady = item.LibraryReady || l.Lemma == id && l.Status == "ready" && libraryMatches(&v.Data, l)
			}
			out.Dependencies = append(out.Dependencies, item)
		}
	}
	for _, a := range v.Attempts {
		if a.Target != target {
			continue
		}
		item := InspectionAttempt{ID: a.ID, Role: a.Role, Status: a.Status, Outcome: a.RemoteOutcome}
		result, resultErr := s.readResult(a)
		if resultErr == nil {
			kind := ""
			if task := v.task(a.TaskID); task != nil {
				kind = task.Kind
			}
			item.FailureClass = inspectionCandidateClass(kind, a, result)
			item.Diagnostics = result.Diagnostics
		}
		out.Attempts = append(out.Attempts, item)
		if check == nil || a.ProofBinding == nil || check.Report == nil || a.ProofBinding.GoalSHA256 != check.Report.GoalSHA256 || a.ProofBinding.SourceSHA256 != check.Report.SourceSHA256 || v.task(a.TaskID) == nil || v.task(a.TaskID).Kind != "review" {
			continue
		}
		review := InspectionReview{Attempt: a.ID, Status: a.Status}
		var report ReviewReport
		if resultErr == nil && s.safeResultContent(a, result) && decodeAgentReport(result.Candidate, &report) == nil && validAttemptReview(a, report) {
			review.Report = &report
		} else if a.Status == "candidate" {
			review.Status = "invalid_format"
		}
		out.Reviews = append(out.Reviews, review)
	}
	if len(out.Attempts) > 12 {
		out.Attempts = out.Attempts[len(out.Attempts)-12:]
	}
	return out, nil
}

func inspectionCandidateClass(kind string, a Attempt, r execution.Result) string {
	if a.Status == "candidate" {
		switch kind {
		case "counterexample":
			var report CounterReport
			if decodeAgentReport(r.Candidate, &report) != nil || validateCounterReport(report) != nil {
				return "report_format"
			}
		case "review":
			var report ReviewReport
			if decodeAgentReport(r.Candidate, &report) != nil || !validAttemptReview(a, report) {
				return "report_format"
			}
		}
	}
	return inspectionFailureClass(r)
}

func inspectionFailureClass(r execution.Result) string {
	for i := len(r.Diagnostics) - 1; i >= 0; i-- {
		d := r.Diagnostics[i]
		if d.Stage == "dns" || d.Stage == "connect" || d.Stage == "tls" {
			if d.Kind == "timeout" || r.Status == "timed_out" {
				return d.Stage + "_timeout"
			}
			return d.Stage + "_failure"
		}
	}
	for _, e := range r.Events {
		if e.Type == "limit_reason" && e.Status == "tool_or_step_limit" {
			return "tool_limit"
		}
	}
	if r.Status == "invalid_format" {
		return "report_format"
	}
	return providerFailureClass(r)
}

func (h *HTTP) inspectionRoutes() {
	h.mux.HandleFunc("GET /api/entities/{id}/inspection", func(w http.ResponseWriter, r *http.Request) {
		value, err := h.service.InspectProof(r.PathValue("id"))
		if err != nil {
			h.fail(w, err)
			return
		}
		h.json(w, value)
	})
}
