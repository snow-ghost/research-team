package researchweb

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
)

var errWorkerQueued = errors.New("waiting for assigned worker")

type WorkerConfig struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	TokenEnv string   `json:"token_env"`
	Profiles []string `json:"profiles"`
}
type WorkerPacket struct {
	Files         []FileDigest      `json:"files"`
	Task          execution.Task    `json:"task"`
	Skills        []execution.Skill `json:"skills"`
	Limits        execution.Limits  `json:"limits"`
	Profile       string            `json:"profile"`
	ProfileSHA256 string            `json:"profile_sha256"`
}
type WorkerRequest struct {
	RequestID string            `json:"request_id"`
	Epoch     uint64            `json:"epoch,omitempty"`
	Snapshot  string            `json:"snapshot,omitempty"`
	Result    *execution.Result `json:"result,omitempty"`
}

func validateWorkers(workers []WorkerConfig) error {
	seen := map[string]bool{}
	if len(workers) > 20 {
		return errors.New("too many workers")
	}
	for _, w := range workers {
		if !requestPattern.MatchString(w.ID) || seen[w.ID] || w.TokenEnv == "" || len(w.Profiles) == 0 || len(w.Profiles) > 20 {
			return errors.New("invalid worker configuration")
		}
		seen[w.ID] = true
	}
	return nil
}
func (s *Service) worker(id string) *WorkerConfig {
	for i := range s.Options.Config.Workers {
		w := &s.Options.Config.Workers[i]
		if w.ID == id {
			return w
		}
	}
	return nil
}
func workerEnabled(d Data, study, id string) bool {
	for _, b := range d.Bindings {
		if b.Kind == "worker" && b.Study == study && b.Connector == id && b.Enabled {
			return true
		}
	}
	return false
}
func (s *Service) workerScope(d Data, id, profile, target string) error {
	w := s.worker(id)
	e := d.entity(target)
	if w == nil || e == nil || !slices.Contains(w.Profiles, profile) || !workerEnabled(d, e.Study, id) {
		return RuleError("Исполнитель не подключен к исследованию или профилю.")
	}
	return nil
}
func (s *Service) validateAssignment(profile, workspace, worker string) error {
	if worker == "" {
		return s.validateExecutor(profile, workspace)
	}
	p, ok := s.Options.Profiles[profile]
	if !ok {
		return RuleError("Профиль не найден.")
	}
	if err := p.Validate(); err != nil {
		return err
	}
	w := s.worker(worker)
	if w == nil || !slices.Contains(w.Profiles, profile) {
		return RuleError("Профиль не разрешен исполнителю.")
	}
	for _, source := range s.Options.Config.Workspaces {
		if source.ID == workspace {
			return nil
		}
	}
	return RuleError("Каталог не разрешен.")
}
func (s *Service) BindWorker(r BindingRequest) error {
	if r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || s.worker(r.Connector) == nil {
		return RuleError("Нужен зарегистрированный исполнитель и снимок.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Настройка удаленного исполнителя", r.Study, "operator", func(d *Data) error {
		found := false
		for _, st := range d.Studies {
			if st.ID == r.Study {
				found = true
			}
		}
		if !found {
			return RuleError("Исследование не найдено.")
		}
		for i, b := range d.Bindings {
			if b.Kind == "worker" && b.Study == r.Study && b.Connector == r.Connector {
				d.Bindings[i].Enabled = r.Enabled
				return nil
			}
		}
		d.Bindings = append(d.Bindings, ConnectorBinding{ID: identifier("binding"), Kind: "worker", Study: r.Study, Connector: r.Connector, Enabled: r.Enabled})
		return nil
	})
}
func (s *Service) ClaimWorker(id string, r WorkerRequest) (*WorkerPacket, error) {
	if !requestPattern.MatchString(r.RequestID) {
		return nil, RuleError("Нужен идентификатор запроса.")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := s.Store.Read()
	if err != nil {
		return nil, err
	}
	if v.Paused {
		return nil, nil
	}
	var a *Attempt
	for _, next := range v.Attempts {
		if next.RemoteWorker == id && next.Status == "running" && next.ClaimRequest != r.RequestID {
			return nil, nil
		}
	}
	for i := range v.Attempts {
		next := &v.Attempts[i]
		if next.RemoteWorker != id {
			continue
		}
		if next.Status == "running" && next.ClaimRequest == r.RequestID {
			if _, err := s.workerLease(&v.Data, id, next.ID, WorkerRequest{Epoch: next.LeaseEpoch, Snapshot: next.InputSHA256}); err != nil {
				return nil, err
			}
			a = next
			break
		}
		if next.Status == "awaiting_worker" && s.workerScope(v.Data, id, next.Profile, next.Target) == nil {
			if e := v.entity(next.Target); e != nil {
				if study := v.study(e.Study); study != nil && study.Budget != nil && study.Budget.DeadlineAt != nil && !time.Now().Before(*study.Budget.DeadlineAt) {
					return nil, ErrConflict
				}
			}
			a = next
			break
		}
	}
	if a == nil {
		return nil, nil
	}
	body, err := readBoundedFile(filepath.Join(s.Options.Config.DataDir, "attempts", a.ID, "worker-task.json"), 1<<20)
	if err != nil {
		return nil, err
	}
	var packet WorkerPacket
	if json.Unmarshal(body, &packet) != nil {
		return nil, errors.New("worker packet invalid")
	}
	if a.Status == "awaiting_worker" {
		now := time.Now().UTC()
		expires := now.Add(45 * time.Second)
		deadline := now.Add(time.Duration(packet.Limits.TimeoutSeconds) * time.Second)
		if e := v.entity(a.Target); e != nil {
			if study := v.study(e.Study); study != nil && study.Budget != nil && study.Budget.DeadlineAt != nil && study.Budget.DeadlineAt.Before(deadline) {
				deadline = *study.Budget.DeadlineAt
			}
		}
		if deadline.Before(expires) {
			expires = deadline
		}
		err = s.Store.Change(0, "", "", "Удаленный исполнитель получил задание", a.Target, "worker:"+id, func(d *Data) error {
			current := d.attempt(a.ID)
			if current.Status != "awaiting_worker" {
				return ErrConflict
			}
			current.LeaseEpoch++
			current.LeaseExpires = &expires
			current.LeaseDeadline = &deadline
			current.ClaimRequest = r.RequestID
			current.Status = "running"
			d.task(current.TaskID).State = "running"
			return nil
		})
		if err != nil {
			return nil, err
		}
		a.LeaseEpoch++
	}
	packet.Task.LeaseEpoch = a.LeaseEpoch
	return &packet, nil
}
func (s *Service) workerLease(d *Data, id, attempt string, r WorkerRequest) (*Attempt, error) {
	a := d.attempt(attempt)
	if a == nil || a.RemoteWorker != id || a.Status != "running" || a.LeaseEpoch != r.Epoch || a.InputSHA256 != r.Snapshot ||
		a.LeaseExpires == nil || !time.Now().Before(*a.LeaseExpires) || a.LeaseDeadline == nil || !time.Now().Before(*a.LeaseDeadline) {
		return nil, ErrConflict
	}
	if err := s.workerScope(*d, id, a.Profile, a.Target); err != nil {
		return nil, err
	}
	e := d.entity(a.Target)
	if study := d.study(e.Study); study != nil && study.Budget != nil && study.Budget.DeadlineAt != nil && !time.Now().Before(*study.Budget.DeadlineAt) {
		return nil, ErrConflict
	}
	if e.Revision != a.TargetRevision || e.Status == "accepted" || e.Status == "refuted" {
		return nil, ErrConflict
	}
	return a, nil
}
func (s *Service) HeartbeatWorker(id, attempt string, r WorkerRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Store.Change(0, "", "", "Продлена аренда задания", attempt, "worker:"+id, func(d *Data) error {
		a, err := s.workerLease(d, id, attempt, r)
		if err != nil {
			return err
		}
		expires := time.Now().UTC().Add(45 * time.Second)
		if a.LeaseDeadline.Before(expires) {
			expires = *a.LeaseDeadline
		}
		a.LeaseExpires = &expires
		return nil
	})
}
func (s *Service) SubmitWorker(id, attempt string, r WorkerRequest) error {
	if !requestPattern.MatchString(r.RequestID) || r.Result == nil {
		return RuleError("Нужен результат и идентификатор запроса.")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Store.Change(0, r.RequestID, hash(r), "Сохранен результат удаленного исполнителя", attempt, "worker:"+id, func(d *Data) error {
		a, err := s.workerLease(d, id, attempt, r)
		if err != nil {
			return err
		}
		result := *r.Result
		if result.TaskID != a.TaskID || result.AttemptID != a.ID || result.ProfileID != a.Profile || result.LeaseEpoch != a.LeaseEpoch || result.Snapshot != a.InputSHA256 {
			return ErrConflict
		}
		if !slices.Contains([]string{"candidate", "failed", "cancelled", "timed_out", "limit_reached"}, result.Status) || len(result.Candidate) > 64000 || len(result.Partial) > 64000 || len(result.Events) > 500 {
			return ErrLimit
		}
		if result.Status == "candidate" && !textOK(result.Candidate, 64000) {
			return RuleError("Пустой кандидат.")
		}
		if result.Status != "candidate" {
			result.Candidate = ""
		}
		// A worker's assertion is evidence, not verification or operator acceptance.
		if result.Usage != nil && (result.Usage.InputTokens < 0 || result.Usage.OutputTokens < 0 || result.Usage.InputTokens > 1e9 || result.Usage.OutputTokens > 1e9) {
			return ErrLimit
		}
		profile := s.Options.Profiles[a.Profile]
		if a.Limits != nil {
			profile.Limits = *a.Limits
		}
		if result.ProfileSHA256 != hash(profile) {
			return ErrConflict
		}
		for _, secret := range s.profileSecrets(profile) {
			result.Candidate = strings.ReplaceAll(result.Candidate, secret, "[REDACTED]")
			result.Partial = strings.ReplaceAll(result.Partial, secret, "[REDACTED]")
			for i := range result.Events {
				e := &result.Events[i]
				e.Input = strings.ReplaceAll(e.Input, secret, "[REDACTED]")
				e.Output = strings.ReplaceAll(e.Output, secret, "[REDACTED]")
				e.Tool = strings.ReplaceAll(e.Tool, secret, "[REDACTED]")
			}
		}
		dir := filepath.Join(s.Options.Config.DataDir, "attempts", a.ID)
		journal, err := openJournal(dir, s.profileSecrets(profile), func() {})
		if err != nil {
			return err
		}
		for _, event := range result.Events {
			journal.observe(event)
		}
		journal.observe(execution.Event{Type: result.Status, Usage: result.Usage})
		journal.Close()
		if err := journal.Err(); err != nil {
			return err
		}
		body, _ := json.Marshal(result)
		if err := atomicFile(filepath.Join(dir, "result.json"), body); err != nil {
			return err
		}
		now := time.Now().UTC()
		a.Status = result.Status
		a.StopCause = attemptStopCause(result, a.Limits)
		a.FinishedAt = &now
		a.RemoteOutcome = result.RemoteOutcome
		a.ResultSHA256 = hash(result)
		d.task(a.TaskID).State = result.Status
		return nil
	})
}
func (s *Service) expireWorkerLeases() {
	v, err := s.Store.Read()
	if err != nil {
		return
	}
	for _, a := range v.Attempts {
		if a.RemoteWorker == "" || a.Status != "running" || a.LeaseExpires == nil || time.Now().Before(*a.LeaseExpires) {
			continue
		}
		s.mu.Lock()
		err = s.Store.Change(0, "", "", "Истекла аренда удаленного задания", a.Target, "server", func(d *Data) error {
			current := d.attempt(a.ID)
			if current.Status != "running" || current.LeaseExpires == nil || time.Now().Before(*current.LeaseExpires) {
				return errNoCycleChange
			}
			current.Status = "interrupted"
			current.RemoteOutcome = "unknown"
			now := time.Now().UTC()
			current.FinishedAt = &now
			d.task(current.TaskID).State = "interrupted"
			return nil
		})
		s.mu.Unlock()
		if err != nil && !errors.Is(err, errNoCycleChange) {
			s.cancel()
			return
		}
	}
}
