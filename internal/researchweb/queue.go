package researchweb

import (
	"slices"
	"time"
)

func queuedBeforeExecution(a Attempt) bool {
	return a.Status == "queued" && a.RemoteOutcome == "not_started" && a.InputSHA256 == "" && a.ResultSHA256 == ""
}

type QueueCommand struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Kind             string `json:"kind"`
	Note             string `json:"note"`
	Confirm          bool   `json:"confirm"`
}

type QueueEntry struct {
	Attempt               string `json:"attempt"`
	Target                string `json:"target"`
	Study                 string `json:"study"`
	Status                string `json:"status"`
	Held                  bool   `json:"held"`
	Reason                string `json:"reason"`
	RemoteOutcome         string `json:"remote_outcome"`
	CanRelease            bool   `json:"can_release"`
	RequiresNewAssignment bool   `json:"requires_new_assignment"`
}

func (s *Service) Queue(study string) ([]QueueEntry, error) {
	v, err := s.Store.Read()
	if err != nil {
		return nil, err
	}
	out := []QueueEntry{}
	for _, a := range v.Attempts {
		e := v.entity(a.Target)
		if e == nil || study != "" && study != e.Study || !active(a.Status) && a.RemoteOutcome != "unknown" && a.Status != "interrupted" {
			continue
		}
		out = append(out, QueueEntry{Attempt: a.ID, Target: a.Target, Study: e.Study, Status: a.Status, Held: a.QueueHeld, Reason: a.QueueReason, RemoteOutcome: a.RemoteOutcome, CanRelease: queuedBeforeExecution(a) && a.QueueHeld && e.Revision == a.TargetRevision, RequiresNewAssignment: a.RemoteOutcome == "unknown" || a.Status == "interrupted"})
	}
	return out, nil
}

func (s *Service) ControlQueue(id string, r QueueCommand) error {
	if r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || !r.Confirm || !textOK(r.Note, 4000) || !slices.Contains([]string{"hold", "release", "cancel"}, r.Kind) {
		return RuleError("Подтвердите действие с очередью и основание.")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(struct {
		ID      string
		Request QueueCommand
	}{id, r}), "Изменена сохраненная очередь", id, "operator", func(d *Data) error {
		a := d.attempt(id)
		if a == nil || !queuedBeforeExecution(*a) || d.Maintenance != nil {
			return RuleError("Управление доступно только не начатому заданию вне обслуживания; неизвестный запрос повторять нельзя.")
		}
		if r.Kind == "release" {
			e := d.entity(a.Target)
			if !a.QueueHeld || e == nil || e.Revision != a.TargetRevision || studyClosed(d, e.Study) || studySearchSuspended(d, e.Study) {
				return RuleError("Нужна удержанная очередь действующей открытой цели.")
			}
			if study := d.study(e.Study); study != nil && study.Workflow != nil && study.Workflow.PlanningAttempt == a.ID && study.Workflow.Status != "planning" {
				return RuleError("Сначала отдельно продолжите полный цикл планировщика.")
			}
			budget := s.observeBudget(d, e.Study)
			if budget.UnknownUsage > 0 || budget.DeadlineReached {
				return RuleError("Сначала уточните неизвестный расход или срок исследования.")
			}
		}
		a.QueueReason = r.Note
		a.QueueHeld = r.Kind == "hold"
		if r.Kind == "cancel" {
			now := time.Now().UTC()
			a.Status, a.FinishedAt, a.StopCause = "cancelled", &now, "operator_cancelled_before_start"
			if task := d.task(a.TaskID); task != nil {
				task.State = "cancelled"
			}
		}
		return nil
	})
}
