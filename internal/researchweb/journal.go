package researchweb

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
)

type JournalEvent struct {
	Sequence int       `json:"sequence"`
	At       time.Time `json:"at"`
	execution.Event
}
type journalWriter struct {
	mu       sync.Mutex
	file     *os.File
	sequence int
	bytes    int
	err      error
	secrets  []string
	stop     func()
}

func openJournal(dir string, secrets []string, stop func()) (*journalWriter, error) {
	file, err := os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, errors.New("cannot create attempt journal")
	}
	return &journalWriter{file: file, secrets: secrets, stop: stop}, nil
}
func (j *journalWriter) observe(e execution.Event) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.err != nil || j.file == nil {
		return
	}
	for _, secret := range j.secrets {
		if secret == "" {
			continue
		}
		e.Type = strings.ReplaceAll(e.Type, secret, "[REDACTED]")
		e.Status = strings.ReplaceAll(e.Status, secret, "[REDACTED]")
		e.Input = strings.ReplaceAll(e.Input, secret, "[REDACTED]")
		e.Output = strings.ReplaceAll(e.Output, secret, "[REDACTED]")
		e.Tool = strings.ReplaceAll(e.Tool, secret, "[REDACTED]")
		e.CallID = strings.ReplaceAll(e.CallID, secret, "[REDACTED]")
	}
	e.Input, e.Output = execution.Preview(e.Input, 8192), execution.Preview(e.Output, 8192)
	row := JournalEvent{Sequence: j.sequence + 1, At: time.Now().UTC(), Event: e}
	body, err := json.Marshal(row)
	if err == nil && (j.sequence >= 4000 || j.bytes+len(body) > 4<<20) {
		err = errors.New("attempt journal limit reached")
	}
	if err == nil {
		_, err = j.file.Write(append(body, '\n'))
	}
	if err != nil {
		j.err = errors.New("attempt journal unavailable or full")
		j.stop()
		return
	}
	j.bytes += len(body) + 1
	j.sequence++
}
func (j *journalWriter) Err() error { j.mu.Lock(); defer j.mu.Unlock(); return j.err }
func (j *journalWriter) Close() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.file == nil {
		return
	}
	if err := j.file.Sync(); err != nil {
		j.err = errors.New("attempt journal sync failed")
	}
	if err := j.file.Close(); err != nil && j.err == nil {
		j.err = errors.New("attempt journal close failed")
	}
	j.file = nil
}
func (s *Service) profileSecrets(p execution.Profile) []string {
	keys := []string{}
	if p.Model != nil {
		keys = append(keys, p.Model.TokenEnv)
	}
	if p.External != nil {
		for _, key := range p.External.SecretEnv {
			keys = append(keys, key)
		}
	}
	out := []string{}
	for _, key := range keys {
		if value, ok := s.Options.Lookup(key); ok && value != "" {
			out = append(out, value)
		}
	}
	return out
}
func (s *Service) Journal(id string, after int) ([]JournalEvent, error) {
	if after < 0 {
		return nil, RuleError("Неверный номер события.")
	}
	view, err := s.Store.Read()
	if err != nil {
		return nil, err
	}
	if view.attempt(id) == nil {
		return nil, RuleError("Попытка не найдена.")
	}
	body, err := readBoundedFile(filepath.Join(s.Options.Config.DataDir, "attempts", id, "events.jsonl"), 4<<20)
	if os.IsNotExist(err) {
		return []JournalEvent{}, nil
	}
	if err != nil {
		return nil, err
	}
	rows := []JournalEvent{}
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	scanner.Buffer(make([]byte, 16384), 65536)
	for scanner.Scan() {
		var row JournalEvent
		if json.Unmarshal(scanner.Bytes(), &row) != nil {
			break
		} // A concurrent append may be incomplete.
		if row.Sequence > after {
			rows = append(rows, row)
		}
		if len(rows) >= 500 {
			break
		}
	}
	return rows, scanner.Err()
}
func (s *Service) continuationContext(d Data, a Attempt) any {
	if a.ParentAttempt == "" {
		return nil
	}
	parent := d.attempt(a.ParentAttempt)
	if parent == nil {
		return nil
	}
	result, err := s.readResult(*parent)
	if err != nil {
		return nil
	}
	if !s.safeResultContent(*parent, result) {
		return map[string]any{"attempt": parent.ID, "status": result.Status, "accepted": false, "content_omitted": "legacy_uncategorized_coddy_stream"}
	}
	events := []JournalEvent{}
	after := 0
	for {
		page, err := s.Journal(parent.ID, after)
		if err != nil || len(page) == 0 {
			break
		}
		for _, event := range page {
			if event.Type != "executor_diagnostic" {
				events = append(events, event)
			}
		}
		after = page[len(page)-1].Sequence
		if len(page) < 500 {
			break
		}
	}
	if len(events) > 30 {
		events = events[len(events)-30:]
	}
	return map[string]any{"attempt": parent.ID, "status": result.Status, "partial": execution.Preview(result.Partial, 24000),
		"candidate": execution.Preview(result.Candidate, 24000), "actions": events, "accepted": false}
}

type ResumeRequest struct {
	ExpectedRevision int               `json:"expected_revision"`
	RequestID        string            `json:"request_id"`
	Limits           *execution.Limits `json:"limits,omitempty"`
	Note             string            `json:"note,omitempty"`
	Confirm          bool              `json:"confirm"`
}

func (s *Service) ResumeAttempt(id string, r ResumeRequest) error {
	if !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || len(r.Note) > 4000 {
		return RuleError("Нужны подтверждение, снимок и идентификатор команды.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Продолжение попытки", id, "operator", func(d *Data) error {
		parent := d.attempt(id)
		if parent == nil || active(parent.Status) || parent.ResultSHA256 == "" {
			return RuleError("Нужна завершенная попытка.")
		}
		if parent.TeamID != "" {
			return RuleError("Продолжайте попытку через управление командой.")
		}
		item := d.entity(parent.Target)
		if item == nil || item.Revision != parent.TargetRevision || item.Status == "accepted" || item.Status == "refuted" {
			return RuleError("Изменилась или закрыта цель попытки.")
		}
		for _, a := range d.Attempts {
			if a.ParentAttempt == id && active(a.Status) {
				return RuleError("Продолжение уже выполняется.")
			}
		}
		if d.Paused {
			return RuleError("Новые запуски приостановлены.")
		}
		if err := s.validateAssignment(parent.Profile, parent.Workspace, parent.RemoteWorker); err != nil {
			return err
		}
		if parent.RemoteWorker != "" {
			if err := s.workerScope(*d, parent.RemoteWorker, parent.Profile, parent.Target); err != nil {
				return err
			}
		}
		profile := attemptProfile(s, *parent)
		limits := r.Limits
		if limits == nil {
			limits = parent.Limits
		}
		if limits != nil {
			profile.Limits = *limits
		}
		if err := profile.Validate(); err != nil {
			return err
		}
		task := d.task(parent.TaskID)
		if task == nil {
			return RuleError("Задание отсутствует.")
		}
		d.addTask(item.ID, "Продолжение: "+task.Title, task.Kind, task.Objective+"\nУточнение оператора: "+r.Note)
		nextTask := &d.Tasks[len(d.Tasks)-1]
		nextTask.Question = task.Question
		attemptID := identifier("run")
		nextTask.Attempt = attemptID
		nextTask.Agent = parent.Profile
		d.Attempts = append(d.Attempts, Attempt{ID: attemptID, ParentAttempt: id, TaskID: nextTask.ID, Target: item.ID, TargetRevision: item.Revision,
			Profile: parent.Profile, RemoteWorker: parent.RemoteWorker, Workspace: parent.Workspace, Limits: limits, Status: "queued", CreatedAt: time.Now().UTC(),
			InputSnapshot: d.Revision + 1, RemoteOutcome: "not_started"})
		return nil
	})
}
