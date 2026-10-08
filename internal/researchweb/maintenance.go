package researchweb

import "reflect"

type MaintenanceHold struct {
	Queue map[string]string `json:"queue"`
}

type MaintenanceRequest struct {
	Kind             string `json:"kind,omitempty"`
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Confirm          bool   `json:"confirm"`
}

func maintenanceQueue(d Data) (map[string]string, error) {
	queue := map[string]string{}
	for _, a := range d.Attempts {
		if !active(a.Status) {
			continue
		}
		if a.Status != "queued" || a.RemoteOutcome != "not_started" || a.InputSHA256 != "" || a.ResultSHA256 != "" {
			return nil, RuleError("Сначала завершите выполняющиеся попытки.")
		}
		queue["attempt:"+a.ID] = hash(a)
	}
	for _, v := range d.Verifications {
		if v.Status == "running" {
			return nil, RuleError("Сначала завершите проверку Lean.")
		}
		if v.Status == "queued" {
			queue["verification:"+v.ID] = hash(v)
		}
	}
	for _, l := range d.Library {
		if l.Status == "running" {
			return nil, RuleError("Сначала завершите сборку леммы.")
		}
		if l.Status == "queued" {
			queue["library:"+l.ID] = hash(l)
		}
	}
	return queue, nil
}

func maintenanceValid(d Data) bool {
	if !d.Paused || d.Maintenance == nil {
		return false
	}
	queue, err := maintenanceQueue(d)
	return err == nil && reflect.DeepEqual(queue, d.Maintenance.Queue)
}

func (s *Service) PrepareMaintenance(r MaintenanceRequest) error {
	if !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || (r.Kind != "" && r.Kind != "prepare" && r.Kind != "release") {
		return RuleError("Подтвердите обслуживание и снимок очереди.")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	label := "Очередь закреплена для обслуживания"
	if r.Kind == "release" {
		label = "Обслуживание завершено; новые запуски оставлены на паузе"
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), label, "", "operator", func(d *Data) error {
		if !d.Paused || len(s.running) != 0 {
			return RuleError("Приостановите новые запуски и дождитесь завершения процессов.")
		}
		if r.Kind == "release" {
			d.Maintenance = nil
			return nil
		}
		queue, err := maintenanceQueue(*d)
		if err != nil {
			return err
		}
		d.Maintenance = &MaintenanceHold{Queue: queue}
		return nil
	})
}
