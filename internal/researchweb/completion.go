package researchweb

import "time"

func studyClosed(d *Data, study string) bool {
	s := d.study(study)
	if s == nil {
		return false
	}
	e := d.entity(s.Goal)
	return e != nil && (e.Status == "refuted" || d.effective(e.ID, map[string]bool{}) == "accepted")
}

// Closing a study stops computation, not its history or the remote provider's accounting.
func stopClosedStudies(d *Data) {
	now := time.Now().UTC()
	for i := range d.Attempts {
		a := &d.Attempts[i]
		e := d.entity(a.Target)
		if task := d.task(a.TaskID); task != nil && task.Kind == "answer" {
			continue
		}
		if e == nil || !active(a.Status) || !studyClosed(d, e.Study) {
			continue
		}
		a.StopCause = "study_goal_closed"
		if a.Status == "queued" || a.RemoteWorker != "" {
			a.Status = "cancelled"
			a.FinishedAt = &now
		} else {
			a.Status = "cancelling"
		}
		if task := d.task(a.TaskID); task != nil {
			task.State = a.Status
		}
	}
	for i := range d.Teams {
		t := &d.Teams[i]
		if (teamActive(t.Status) || t.Status == "interrupted") && studyClosed(d, t.Study) {
			t.Status = "completed"
			t.Reason = "Основная цель исследования закрыта оператором."
		}
	}
	for i := range d.Cycles {
		c := &d.Cycles[i]
		if cycleActive(c.Status) && studyClosed(d, c.Study) {
			c.Status = "completed"
			c.AcceptedSnapshot = d.Revision + 1
			c.Reason = "Основная цель исследования закрыта оператором."
		}
	}
}

type StudyCompletion struct {
	MathematicalHints          int               `json:"mathematical_hints"`
	UnclassifiedNotes          int               `json:"unclassified_notes"`
	PlannerMethods             int               `json:"planner_methods"`
	PauseOnReady               bool              `json:"pause_on_ready"`
	SearchSuspended            bool              `json:"search_suspended"`
	Revision                   int               `json:"revision"`
	Study                      string            `json:"study"`
	Goal                       string            `json:"goal"`
	Status                     string            `json:"status"`
	GoalStatus                 string            `json:"goal_status"`
	ReadyResults               int               `json:"ready_results"`
	ActiveAttempts             int               `json:"active_attempts"`
	ExecutorSources            int               `json:"executor_sources"`
	SubmittedSources           int               `json:"submitted_sources"`
	ImportedSources            int               `json:"imported_sources"`
	OperatorNotes              int               `json:"operator_notes"`
	OperatorAcceptanceRequired bool              `json:"operator_acceptance_required"`
	Budget                     BudgetObservation `json:"budget"`
}

func (s *Service) StudyCompletion(id string) (StudyCompletion, error) {
	v, err := s.Store.Read()
	if err != nil {
		return StudyCompletion{}, err
	}
	st := v.study(id)
	if st == nil {
		return StudyCompletion{}, RuleError("Исследование не найдено.")
	}
	out := StudyCompletion{Revision: v.Revision, Study: id, Goal: st.Goal, Status: "researching", GoalStatus: v.effective(st.Goal, map[string]bool{}), Budget: s.observeBudget(&v.Data, id), OperatorAcceptanceRequired: true}
	out.PauseOnReady, out.SearchSuspended = st.PauseOnReady, studySearchSuspended(&v.Data, id)
	if studyClosed(&v.Data, id) {
		out.Status = "completed"
		out.OperatorAcceptanceRequired = false
	}
	for _, e := range v.Entities {
		if e.Study != id || e.Proof == "" {
			continue
		}
		if e.ProofAttempt != "" {
			out.ExecutorSources++
		} else {
			out.SubmittedSources++
		}
		for _, r := range v.Results {
			if r.ID == e.ResearchResult && r.Target == e.ID && r.Transfer != nil {
				out.ImportedSources++
			}
		}
	}
	for _, r := range v.Results {
		if e := v.entity(r.Target); e != nil && e.Study == id && r.Status == "ready" && resultMatches(&v.Data, r) {
			out.ReadyResults++
			if r.Target == st.Goal {
				out.Status = "awaiting_operator"
			}
		}
	}
	for _, a := range v.Attempts {
		if e := v.entity(a.Target); e != nil && e.Study == id && active(a.Status) {
			out.ActiveAttempts++
		}
	}
	for _, t := range v.Teams {
		if t.Study != id {
			continue
		}
		if t.MethodBasis != nil && t.MethodBasis.Source == "planner" {
			out.PlannerMethods++
		}
		if len(t.Notes) == 0 && t.OperatorNote != "" {
			out.UnclassifiedNotes++
		}
		for _, note := range t.Notes {
			out.OperatorNotes++
			if note.Kind == "mathematical_hint" {
				out.MathematicalHints++
			}
			if note.Kind == "unspecified" {
				out.UnclassifiedNotes++
			}
		}
	}
	return out, nil
}
