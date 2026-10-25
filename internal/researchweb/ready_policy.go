package researchweb

import "time"

type StudyPolicyRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	PauseOnReady     bool   `json:"pause_on_ready"`
	Confirm          bool   `json:"confirm"`
}

func studySearchSuspended(d *Data, id string) bool {
	study := d.study(id)
	if study == nil || !study.PauseOnReady || studyClosed(d, id) {
		return false
	}
	for _, result := range d.Results {
		if result.Target == study.Goal && result.Status == "ready" && resultMatches(d, result) {
			return true
		}
	}
	return false
}

func holdReadyStudies(d *Data) {
	now := time.Now().UTC()
	for _, study := range d.Studies {
		if !studySearchSuspended(d, study.ID) {
			continue
		}
		for i := range d.Attempts {
			a := &d.Attempts[i]
			e := d.entity(a.Target)
			if task := d.task(a.TaskID); task != nil && task.Kind == "answer" {
				continue
			}
			if e == nil || e.Study != study.ID || !active(a.Status) || a.StopCause == "study_result_ready" {
				continue
			}
			a.StopCause = "study_result_ready"
			if a.Status == "queued" || a.RemoteWorker != "" {
				a.Status, a.FinishedAt = "cancelled", &now
			} else {
				a.Status = "cancelling"
			}
			if task := d.task(a.TaskID); task != nil {
				task.State = a.Status
			}
		}
		for i := range d.Teams {
			team := &d.Teams[i]
			if team.Study == study.ID && team.Status == "running" {
				team.Status, team.Reason = "paused", "Готовый результат основной цели ожидает решения оператора."
			}
		}
		for i := range d.Cycles {
			cycle := &d.Cycles[i]
			if cycle.Study == study.ID && cycle.Status == "running" {
				cycle.Status, cycle.Reason = "paused", "Готовый результат основной цели ожидает решения оператора."
			}
		}
	}
}

func (s *Service) SetStudyPolicy(id string, r StudyPolicyRequest) error {
	if !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) {
		return RuleError("Подтвердите настройку остановки поиска.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(struct {
		Study string
		StudyPolicyRequest
	}{id, r}), "Изменена остановка на готовом результате", id, "operator", func(d *Data) error {
		study := d.study(id)
		if study == nil {
			return RuleError("Исследование не найдено.")
		}
		study.PauseOnReady = r.PauseOnReady
		return nil
	})
}
