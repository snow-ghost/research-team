package researchweb

import (
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
)

type StudyBudget struct {
	MaxModelRequests int        `json:"max_model_requests,omitempty"`
	OperatorNote     string     `json:"operator_note,omitempty"`
	MaxAttempts      int        `json:"max_attempts"`
	MaxOutputTokens  int64      `json:"max_output_tokens"`
	DeadlineAt       *time.Time `json:"deadline_at,omitempty"`
}

type BudgetRequest struct {
	ExpectedRevision int         `json:"expected_revision"`
	RequestID        string      `json:"request_id"`
	Budget           StudyBudget `json:"budget"`
	Confirm          bool        `json:"confirm"`
	Note             string      `json:"note"`
}

type BudgetObservation struct {
	KnownModelRequests      int          `json:"known_model_requests"`
	ChargedModelRequests    int          `json:"charged_model_requests"`
	ReservedModelRequests   int          `json:"reserved_model_requests"`
	UnreservedModelRequests int          `json:"unreserved_model_requests"`
	Limits                  *StudyBudget `json:"limits,omitempty"`
	Attempts                int          `json:"attempts"`
	KnownOutputTokens       int64        `json:"known_output_tokens"`
	ChargedTokens           int64        `json:"charged_tokens"`
	ReservedTokens          int64        `json:"reserved_tokens"`
	UnknownUsage            int          `json:"unknown_usage"`
	UnreservedUnknown       int          `json:"unreserved_unknown"`
	DeadlineReached         bool         `json:"deadline_reached"`
}

func (d *Data) study(id string) *Study {
	for i := range d.Studies {
		if d.Studies[i].ID == id {
			return &d.Studies[i]
		}
	}
	return nil
}

func (s *Service) SetStudyBudget(id string, r BudgetRequest) error {
	if r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || !r.Confirm || !textOK(r.Note, 4000) || r.Budget.MaxAttempts < 1 || r.Budget.MaxAttempts > 100 || r.Budget.MaxOutputTokens < 0 || r.Budget.MaxOutputTokens > 1000000000 || r.Budget.MaxModelRequests < 0 || r.Budget.MaxModelRequests > 10000 {
		return RuleError("Подтвердите бюджет: от 1 до 100 попыток, неотрицательный предел токенов и основание изменения.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(struct {
		ID string
		BudgetRequest
	}{id, r}), "Изменен общий бюджет исследования", id, "operator", func(d *Data) error {
		study := d.study(id)
		if study == nil {
			return RuleError("Исследование не найдено.")
		}
		for _, a := range d.Attempts {
			if e := d.entity(a.Target); e != nil && e.Study == id && active(a.Status) {
				return RuleError("Остановите действующие попытки перед изменением бюджета.")
			}
		}
		o := s.observeBudget(d, id)
		if r.Budget.MaxModelRequests > 0 && (o.UnreservedModelRequests > 0 || r.Budget.MaxModelRequests < o.ChargedModelRequests) {
			return RuleError("Предел обращений не покрывает историю или неизвестные резервы.")
		}
		if r.Budget.MaxAttempts < o.Attempts || (r.Budget.MaxOutputTokens > 0 && (o.UnreservedUnknown > 0 || r.Budget.MaxOutputTokens < o.ChargedTokens)) {
			return RuleError("Бюджет не покрывает историю расхода или исторические обращения с неизвестным резервом.")
		}
		r.Budget.OperatorNote = r.Note
		study.Budget = &r.Budget
		return nil
	})
}

func (s *Service) observeBudget(d *Data, study string) BudgetObservation {
	o := BudgetObservation{}
	if selected := d.study(study); selected != nil {
		o.Limits = selected.Budget
		if o.Limits != nil && o.Limits.DeadlineAt != nil {
			o.DeadlineReached = !time.Now().Before(*o.Limits.DeadlineAt)
		}
	}
	for _, a := range d.Attempts {
		e := d.entity(a.Target)
		if e == nil || e.Study != study {
			continue
		}
		o.Attempts++
		if active(a.Status) {
			o.ReservedModelRequests += a.ReservedModelRequests
			o.ReservedTokens += a.ReservedOutputTokens
			if a.ReservedOutputTokens == 0 && o.Limits != nil && o.Limits.MaxOutputTokens > 0 {
				o.UnreservedUnknown++
			}
			continue
		}
		result, err := s.readResult(a)
		requests := 0
		if err == nil {
			for _, event := range result.Events {
				if event.Type == "model_requested" {
					requests++
				}
			}
		}
		if requests > 0 {
			o.KnownModelRequests += requests
			o.ChargedModelRequests += requests
		} else if a.RemoteOutcome != "not_started" {
			o.ChargedModelRequests += a.ReservedModelRequests
			if a.ReservedModelRequests == 0 {
				o.UnreservedModelRequests++
			}
		}
		if err == nil && result.Usage != nil && !result.Usage.Incomplete && result.Usage.OutputTokens >= 0 && result.Usage.InputTokens >= 0 {
			o.KnownOutputTokens += result.Usage.OutputTokens
			o.ChargedTokens += result.Usage.OutputTokens
		} else if a.RemoteOutcome != "not_started" {
			o.UnknownUsage++
			o.ChargedTokens += a.ReservedOutputTokens
			if a.ReservedOutputTokens == 0 {
				o.UnreservedUnknown++
			}
		}
	}
	return o
}

// Output reservation is a scheduling bound, not a price or an input-token estimate.
func (s *Service) reserveStudyBudget(d *Data, target string, p execution.Profile) (int64, error) {
	e := d.entity(target)
	if e == nil {
		return 0, RuleError("Утверждение не найдено.")
	}
	reservation := int64(p.Limits.MaxOutputTokens) * int64(p.Limits.MaxSteps)
	o := s.observeBudget(d, e.Study)
	if o.Limits == nil {
		return reservation, nil
	}
	if o.DeadlineReached {
		return 0, RuleError("Достигнут срок общего бюджета исследования.")
	}
	if o.Attempts >= o.Limits.MaxAttempts {
		return 0, RuleError("Исчерпан общий предел попыток исследования.")
	}
	if o.Limits.MaxModelRequests > 0 && (p.Limits.MaxSteps < 1 || o.UnreservedModelRequests > 0 || o.ChargedModelRequests+o.ReservedModelRequests+p.Limits.MaxSteps > o.Limits.MaxModelRequests) {
		return 0, RuleError("Общий бюджет обращений не покрывает резерв новой попытки.")
	}
	if o.Limits.MaxOutputTokens > 0 && (reservation <= 0 || o.UnreservedUnknown > 0 || o.ChargedTokens+o.ReservedTokens+reservation > o.Limits.MaxOutputTokens) {
		return 0, RuleError("Общий бюджет выходных токенов не покрывает резерв новой попытки.")
	}
	return reservation, nil
}

func (s *Service) expireStudyBudgets() {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := s.Store.Read()
	if err != nil {
		return
	}
	for _, study := range v.Studies {
		if study.Budget == nil || study.Budget.DeadlineAt == nil || time.Now().Before(*study.Budget.DeadlineAt) {
			continue
		}
		needed := false
		for _, a := range v.Attempts {
			if e := v.entity(a.Target); e != nil && e.Study == study.ID && active(a.Status) && a.Status != "cancelling" {
				needed = true
			}
		}
		if !needed {
			continue
		}
		err = s.Store.Change(0, "", "", "Достигнут срок общего бюджета", study.ID, "server", func(d *Data) error {
			for i := range d.Attempts {
				a := &d.Attempts[i]
				if e := d.entity(a.Target); e == nil || e.Study != study.ID || !active(a.Status) {
					continue
				}
				if a.Status == "queued" || a.RemoteWorker != "" {
					a.Status = "cancelled"
				} else {
					a.Status = "cancelling"
				}
				a.StopCause = "study_deadline"
				d.task(a.TaskID).State = a.Status
			}
			for i := range d.Teams {
				if d.Teams[i].Study == study.ID && teamActive(d.Teams[i].Status) {
					d.Teams[i].Status, d.Teams[i].Reason = "blocked", "Достигнут срок общего бюджета исследования."
				}
			}
			for i := range d.Cycles {
				if d.Cycles[i].Study == study.ID && cycleActive(d.Cycles[i].Status) {
					d.Cycles[i].Status, d.Cycles[i].Reason = "blocked", "Достигнут срок общего бюджета исследования."
				}
			}
			return nil
		})
		if err == nil {
			s.cancelRequested()
		}
	}
}

func attemptStopCause(r execution.Result, limits *execution.Limits) string {
	for _, diagnostic := range r.Diagnostics {
		if diagnostic.ByteLimitReached {
			return "output_bytes"
		}
	}
	switch r.Status {
	case "candidate":
		return "completed"
	case "timed_out":
		return "attempt_timeout"
	case "cancelled":
		return "cancelled"
	case "limit_reached":
		for _, event := range r.Events {
			if event.Type == "acp_token_limit_reached" {
				return "output_tokens"
			}
			if event.Type == "acp_turn_limit_reached" {
				return "execution_steps"
			}
		}
		if limits != nil && limits.MaxSteps == 1 && r.Usage != nil && !r.Usage.Incomplete && r.Usage.OutputTokens >= int64(limits.MaxOutputTokens) && limits.MaxOutputTokens > 0 {
			return "output_tokens"
		}
		return "execution_limit"
	}
	for _, diagnostic := range r.Diagnostics {
		if diagnostic.Kind == "rpc_error" || diagnostic.Kind == "transport_error" {
			return "acp_or_transport_error"
		}
	}
	return "unspecified_failure"
}
