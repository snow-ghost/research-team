package researchweb

import (
	"encoding/json"
	"slices"
	"strings"
	"time"
)

type CounterObligation struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

func counterObligations(r CounterReport) ([]CounterObligation, error) {
	if len(r.Unresolved) == 0 {
		return nil, nil
	}
	var legacy string
	if json.Unmarshal(r.Unresolved, &legacy) == nil {
		if r.Schema != "" || len(legacy) > 8000 {
			return nil, RuleError("В новой схеме нужен массив вопросов.")
		}
		if strings.TrimSpace(legacy) == "" {
			return nil, nil
		}
		return []CounterObligation{{Kind: "unclassified", Text: legacy}}, nil
	}
	var issues []CounterObligation
	if decodeJSON(r.Unresolved, &issues) != nil || issues == nil || len(issues) > 16 {
		return nil, RuleError("Некорректные вопросы контрпримеров.")
	}
	for _, issue := range issues {
		if !slices.Contains([]string{"mathematical", "operational"}, issue.Kind) || !textOK(issue.Text, 4000) {
			return nil, RuleError("Укажите вид и текст каждого вопроса.")
		}
	}
	return issues, nil
}

func validateCounterReport(r CounterReport) error {
	if r.Schema != "" && r.Schema != "counterexample-report@2" || !slices.Contains([]string{"none_found", "counterexample_candidate", "inconclusive"}, r.Outcome) || !textOK(r.Evidence, 4000) || len(r.Assumptions) > 8000 || len(r.Limitations) > 8000 || len(r.RefutationSource) > 64000 {
		return RuleError("Некорректная схема отчета контрпримеров.")
	}
	if r.Schema != "" && len(r.Unresolved) == 0 {
		return RuleError("Новая схема требует явного массива вопросов.")
	}
	_, err := counterObligations(r)
	return err
}

func counterHasObligations(r CounterReport) bool {
	issues, err := counterObligations(r)
	return err != nil || len(issues) > 0
}

type CounterAudit struct {
	ID           string    `json:"id"`
	Attempt      string    `json:"attempt"`
	Target       string    `json:"target"`
	ResultSHA256 string    `json:"result_sha256"`
	ReportSHA256 string    `json:"report_sha256"`
	ScopeSHA256  string    `json:"scope_sha256"`
	Note         string    `json:"note"`
	CreatedAt    time.Time `json:"created_at"`
}

type CounterAuditRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Attempt          string `json:"attempt"`
	ReportSHA256     string `json:"report_sha256"`
	Note             string `json:"note"`
	Confirm          bool   `json:"confirm"`
}

func counterCleared(d *Data, a *Attempt, r CounterReport) bool {
	issues, err := counterObligations(r)
	if err != nil {
		return false
	}
	if len(issues) == 0 {
		return true
	}
	for _, issue := range issues {
		if issue.Kind == "mathematical" {
			return false
		}
	}
	if a == nil || d.entity(a.Target) == nil {
		return false
	}
	for _, audit := range d.CounterAudits {
		if audit.Attempt == a.ID && audit.Target == a.Target && audit.ResultSHA256 == a.ResultSHA256 && audit.ReportSHA256 == hash(r) && audit.ScopeSHA256 == memoryScope(*d.entity(a.Target)) {
			return true
		}
	}
	return false
}

// Classifying a legacy question is a recorded operator action, never a parser heuristic.
func (s *Service) AuditCounter(r CounterAuditRequest) error {
	if !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || !textOK(r.Note, 4000) {
		return RuleError("Подтвердите разбор только операционных вопросов и укажите основание.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Разобраны операционные вопросы отчета контрпримеров", r.Attempt, "operator", func(d *Data) error {
		a := d.attempt(r.Attempt)
		if a == nil || a.Status != "candidate" || d.entity(a.Target) == nil || d.task(a.TaskID) == nil || d.task(a.TaskID).Kind != "counterexample" {
			return RuleError("Нужен завершенный отчет контрпримеров.")
		}
		result, err := s.readResult(*a)
		if err != nil {
			return err
		}
		var report CounterReport
		if !s.safeResultContent(*a, result) || decodeAgentReport(result.Candidate, &report) != nil || validateCounterReport(report) != nil || hash(report) != r.ReportSHA256 || !counterHasObligations(report) {
			return RuleError("Нужен точный сохраненный отчет с незакрытыми вопросами.")
		}
		issues, _ := counterObligations(report)
		for _, issue := range issues {
			if issue.Kind == "mathematical" {
				return RuleError("Математический вопрос требует нового проверенного результата, а не операционной отметки.")
			}
		}
		input, err := s.readInput(*a)
		if err != nil {
			return err
		}
		old, current := input.entity(a.Target), d.entity(a.Target)
		if old == nil || memoryScope(*old) != memoryScope(*current) {
			return RuleError("Постановка изменилась после проверки контрпримеров.")
		}
		if len(d.CounterAudits) >= 200 {
			return ErrLimit
		}
		d.CounterAudits = append(d.CounterAudits, CounterAudit{ID: identifier("counter-audit"), Attempt: a.ID, Target: a.Target, ResultSHA256: a.ResultSHA256, ReportSHA256: hash(report), ScopeSHA256: memoryScope(*current), Note: r.Note, CreatedAt: time.Now().UTC()})
		return nil
	})
}
