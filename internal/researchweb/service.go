package researchweb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
)

type Service struct {
	Options   Options
	Store     *Store
	mu        sync.Mutex
	running   map[string]context.CancelFunc
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closeOnce sync.Once
	closeErr  error
}
type RunRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	TaskID           string `json:"task_id"`
	Profile          string `json:"profile"`
	Workspace        string `json:"workspace"`
	Confirm          bool   `json:"confirm"`
}
type ProfileView struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Kind      string   `json:"kind"`
	Provider  string   `json:"provider"`
	Model     string   `json:"model"`
	Available bool     `json:"available"`
	Skills    []string `json:"skills"`
}

func NewService(o Options) (*Service, error) {
	if o.Lookup == nil {
		o.Lookup = os.LookupEnv
	}
	if o.Factory == nil {
		o.Factory = execution.Build
	}
	if o.Config.MaxParallel < 1 || o.Config.MaxParallel > 4 {
		return nil, errors.New("invalid parallel limit")
	}
	store, err := OpenStore(o.Config.DataDir)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Join(o.Config.DataDir, "attempts"), 0700); err != nil {
		store.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{Options: o, Store: store, running: map[string]context.CancelFunc{}, ctx: ctx, cancel: cancel}
	v, err := store.Read()
	if err != nil {
		s.Close()
		return nil, err
	}
	interrupted := false
	for _, a := range v.Attempts {
		if active(a.Status) {
			interrupted = true
		}
	}
	if interrupted {
		err = store.Change(0, "", "", "Прерванные попытки восстановлены как неизвестный исход", "", "server", func(d *Data) error {
			for i := range d.Attempts {
				a := &d.Attempts[i]
				if active(a.Status) {
					a.Status = "interrupted"
					if a.RemoteOutcome != "not_started" {
						a.RemoteOutcome = "unknown"
					}
					if t := d.task(a.TaskID); t != nil {
						t.State = "interrupted"
					}
				}
			}
			return nil
		})
		if err != nil {
			s.Close()
			return nil, err
		}
	}
	s.wg.Add(1)
	go s.schedule()
	return s, nil
}
func (s *Service) Close() error {
	s.closeOnce.Do(func() { s.cancel(); s.wg.Wait(); s.closeErr = s.Store.Close() })
	return s.closeErr
}
func active(status string) bool {
	return status == "queued" || status == "preparing" || status == "running" || status == "cancelling"
}
func (s *Service) Profiles() []ProfileView {
	out := []ProfileView{}
	for id, p := range s.Options.Profiles {
		v := ProfileView{ID: id, Label: s.Options.Labels[id], Kind: p.Kind, Available: true, Skills: []string{}}
		if v.Label == "" {
			v.Label = id
		}
		keys := []string{}
		if p.Model != nil {
			v.Provider = p.Model.Protocol
			v.Model = p.Model.Model
			keys = append(keys, p.Model.TokenEnv)
		}
		if p.External != nil {
			v.Provider = p.External.Provider
			v.Model = p.External.Model
			for _, key := range p.External.SecretEnv {
				keys = append(keys, key)
			}
			info, err := os.Stat(p.External.Executable)
			v.Available = s.Options.Config.AllowExternalExecution && err == nil && info.Mode().IsRegular()
		}
		for _, key := range keys {
			if key != "" {
				value, ok := s.Options.Lookup(key)
				v.Available = v.Available && ok && value != ""
			}
		}
		for _, skill := range p.Skills {
			v.Skills = append(v.Skills, skill.ID+"@"+skill.Version)
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (s *Service) Act(a Action) error {
	if !requestPattern.MatchString(a.RequestID) {
		return RuleError("Нужен идентификатор команды.")
	}
	if a.ExpectedRevision < 1 {
		return RuleError("Нужен номер снимка.")
	}
	if a.Type == "ATTACH_PROOF" {
		return s.Store.Change(a.ExpectedRevision, a.RequestID, hash(a), actionLabel(a.Type), a.Target, "operator", func(d *Data) error {
			attempt := d.attempt(a.Attempt)
			item := d.entity(a.Target)
			if attempt == nil || item == nil || attempt.Status != "candidate" || attempt.Target != item.ID || attempt.TargetRevision != item.Revision {
				return RuleError("Материал не соответствует текущей версии утверждения.")
			}
			if item.Status == "accepted" || item.Status == "refuted" {
				return RuleError("Принятая версия не изменяется.")
			}
			task := d.task(attempt.TaskID)
			if task == nil || task.Kind != "proof" {
				return RuleError("Для доказательства нужно задание вида proof.")
			}
			result, err := s.readResult(*attempt)
			if err != nil {
				return err
			}
			source, err := s.readInput(*attempt)
			if err != nil {
				return err
			}
			previous := source.entity(item.ID)
			if previous == nil {
				return ErrConflict
			}
			pins := map[string]int{}
			for _, dep := range previous.Dependencies {
				old, current := source.entity(dep), d.entity(dep)
				if old == nil || current == nil || old.Revision != current.Revision {
					return RuleError("Основания изменились после запуска попытки.")
				}
				pins[dep] = old.Revision
			}
			if !textOK(result.Candidate, 64000) {
				return RuleError("Материал пуст или превышает размер текстового доказательства.")
			}
			item.Proof = result.Candidate
			item.ProofAuthor = "executor:" + attempt.Profile
			item.ProofAttempt = attempt.ID
			item.DependencyRevisions = pins
			item.Status = "in_review"
			item.Revision++
			return nil
		})
	}
	return s.Store.Change(a.ExpectedRevision, a.RequestID, hash(a), actionLabel(a.Type), a.Target, "operator", func(d *Data) error { return applyAction(d, a) })
}
func (s *Service) Start(r RunRequest) error {
	if !requestPattern.MatchString(r.RequestID) {
		return RuleError("Нужен идентификатор команды.")
	}
	if !r.Confirm {
		return RuleError("Запуск требует подтверждения возможных расходов.")
	}
	if r.ExpectedRevision < 1 {
		return RuleError("Нужен номер снимка.")
	}
	profile, ok := s.Options.Profiles[r.Profile]
	if !ok {
		return RuleError("Исполнитель не разрешен сервером.")
	}
	if profile.Kind == "external" && !s.Options.Config.AllowExternalExecution {
		return RuleError("Внешние процессы запрещены настройками сервера.")
	}
	if err := profile.Validate(); err != nil {
		return err
	}
	workspace := false
	for _, w := range s.Options.Config.Workspaces {
		workspace = workspace || w.ID == r.Workspace
	}
	if !workspace {
		return RuleError("Каталог не разрешен сервером.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Попытка поставлена в очередь", r.TaskID, "operator", func(d *Data) error {
		if d.Paused {
			return RuleError("Новые запуски приостановлены.")
		}
		t := d.task(r.TaskID)
		if t == nil || t.State != "queued" || t.Attempt != "" {
			return RuleError("Задание уже запускалось или недоступно.")
		}
		item := d.entity(t.Target)
		if item == nil {
			return RuleError("Утверждение не найдено.")
		}
		id := identifier("run")
		t.Attempt = id
		t.Agent = r.Profile
		d.Attempts = append(d.Attempts, Attempt{ID: id, TaskID: t.ID, Target: item.ID, TargetRevision: item.Revision,
			Profile: r.Profile, Workspace: r.Workspace, Status: "queued", CreatedAt: time.Now().UTC(),
			InputSnapshot: d.Revision, RemoteOutcome: "not_started"})
		return nil
	})
}
func (s *Service) Cancel(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.Store.Change(0, "", "", "Запрошена остановка попытки", id, "operator", func(d *Data) error {
		a := d.attempt(id)
		if a == nil {
			return RuleError("Попытка не найдена.")
		}
		if !active(a.Status) {
			return RuleError("Попытка уже завершена.")
		}
		if a.Status == "queued" {
			a.Status = "cancelled"
		} else {
			a.Status = "cancelling"
		}
		d.task(a.TaskID).State = a.Status
		return nil
	})
	if err == nil {
		if cancel := s.running[id]; cancel != nil {
			cancel()
		}
	}
	return err
}
func (s *Service) schedule() {
	defer s.wg.Done()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			if len(s.running) >= s.Options.Config.MaxParallel {
				s.mu.Unlock()
				continue
			}
			v, err := s.Store.Read()
			if err != nil || v.Paused {
				s.mu.Unlock()
				continue
			}
			for _, a := range v.Attempts {
				if a.Status == "queued" {
					err = s.Store.Change(0, "", "", "Подготовка снимка входных файлов", a.Target, "server", func(d *Data) error {
						current := d.attempt(a.ID)
						if current == nil || current.Status != "queued" || d.Paused {
							return ErrConflict
						}
						current.Status = "preparing"
						d.task(a.TaskID).State = "preparing"
						return nil
					})
					if err == nil {
						ctx, cancel := context.WithCancel(s.ctx)
						s.running[a.ID] = cancel
						s.wg.Add(1)
						go s.execute(ctx, a)
					}
					break
				}
			}
			s.mu.Unlock()
		}
	}
}
func (s *Service) execute(ctx context.Context, a Attempt) {
	defer s.wg.Done()
	result := execution.Result{TaskID: a.TaskID, AttemptID: a.ID, ProfileID: a.Profile, LeaseEpoch: 1, Status: "failed", RemoteOutcome: "not_started", Events: []execution.Event{}}
	err := s.perform(ctx, a, &result)
	if ctx.Err() != nil {
		result.Status = "cancelled"
		result.Candidate = ""
	}
	if err != nil && result.Status == "candidate" {
		result.Status = "failed"
		result.Candidate = ""
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	defer delete(s.running, a.ID)
	if cancel := s.running[a.ID]; cancel != nil {
		defer cancel()
	}
	changeErr := s.Store.Change(0, "", "", "Попытка завершена", a.Target, "executor", func(d *Data) error {
		current := d.attempt(a.ID)
		if current == nil {
			return errors.New("attempt missing")
		}
		if current.Status == "cancelling" || ctx.Err() != nil {
			result.Status = "cancelled"
			result.Candidate = ""
		}
		if result.Status == "" {
			result.Status = "failed"
		}
		if result.Status == "candidate" && strings.TrimSpace(result.Candidate) == "" {
			result.Status = "failed"
		}
		data, _ := json.Marshal(result)
		dir := filepath.Join(s.Options.Config.DataDir, "attempts", a.ID)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		if err := atomicFile(filepath.Join(dir, "result.json"), data); err != nil {
			return err
		}
		current.ResultSHA256 = hash(result)
		current.Status = result.Status
		current.FinishedAt = &now
		current.RemoteOutcome = result.RemoteOutcome
		task := d.task(a.TaskID)
		task.State = result.Status
		for i := range d.Questions {
			q := &d.Questions[i]
			if q.ID == task.Question {
				q.AnswerAttempt = a.ID
				if result.Status == "candidate" {
					if len(result.Candidate) <= 64000 {
						q.Answer = result.Candidate
					} else {
						q.Answer = "Полный ответ сохранен в попытке " + a.ID
					}
				}
			}
		}
		return nil
	})
	if changeErr != nil {
		s.cancel()
	} // Fail closed if completion cannot be persisted.
}
func (s *Service) perform(ctx context.Context, a Attempt, result *execution.Result) error {
	d, err := s.Store.Snapshot(a.InputSnapshot)
	if err != nil {
		return err
	}
	task := d.task(a.TaskID)
	if task == nil {
		return errors.New("task missing from snapshot")
	}
	dir := filepath.Join(s.Options.Config.DataDir, "attempts", a.ID)
	if err = os.Mkdir(dir, 0700); err != nil {
		return err
	}
	var source string
	for _, w := range s.Options.Config.Workspaces {
		if w.ID == a.Workspace {
			source = w.Path
		}
	}
	files, err := copyWorkspace(ctx, source, filepath.Join(dir, "workspace"))
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	input := struct {
		Research Data              `json:"research"`
		Files    []FileDigest      `json:"files"`
		Profile  execution.Profile `json:"profile"`
	}{d, files, s.Options.Profiles[a.Profile]}
	inputSHA := hash(input)
	data, _ := json.Marshal(input)
	if err = atomicFile(filepath.Join(dir, "input.json"), data); err != nil {
		return err
	}
	// Keep the complete research snapshot on disk; send only the selected study and dependencies.
	entity := d.entity(a.Target)
	relevant := []Entity{}
	seen := map[string]bool{}
	var visit func(string)
	visit = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		e := d.entity(id)
		if e == nil {
			return
		}
		relevant = append(relevant, *e)
		for _, dep := range e.Dependencies {
			visit(dep)
		}
	}
	visit(entity.ID)
	contextData, _ := json.Marshal(struct {
		Entities  []Entity   `json:"entities"`
		Questions []Question `json:"questions"`
	}{relevant, questionsFor(d, a.Target)})
	t := execution.Task{ID: task.ID, AttemptID: a.ID, Snapshot: inputSHA, LeaseEpoch: 1, Workspace: filepath.Join(dir, "workspace"), Objective: task.Objective, Context: string(contextData)}
	profile, ok := s.Options.Profiles[a.Profile]
	if !ok {
		return errors.New("profile unavailable")
	}
	executor, err := s.Options.Factory(profile, s.Options.Lookup)
	if err != nil {
		return err
	}
	s.mu.Lock()
	err = s.Store.Change(0, "", "", "Исполнитель запущен", a.Target, "server", func(d *Data) error {
		current := d.attempt(a.ID)
		if current.Status != "preparing" || ctx.Err() != nil {
			return context.Canceled
		}
		current.Status = "running"
		current.InputSHA256 = inputSHA
		current.RemoteOutcome = "unknown"
		d.task(a.TaskID).State = "running"
		return nil
	})
	s.mu.Unlock()
	if err != nil {
		return err
	}
	*result, err = executor.Run(ctx, t)
	if err == nil && (result.TaskID != task.ID || result.AttemptID != a.ID || result.Snapshot != inputSHA || result.LeaseEpoch != 1 || result.ProfileID != a.Profile) {
		result.Status = "failed"
		result.Candidate = ""
		return execution.ErrProtocol
	}
	return err
}
func questionsFor(d Data, target string) []Question {
	out := []Question{}
	for _, q := range d.Questions {
		if q.Target == target {
			out = append(out, q)
		}
	}
	return out
}
func (s *Service) readResult(a Attempt) (execution.Result, error) {
	var r execution.Result
	if a.ResultSHA256 == "" {
		return r, RuleError("Результат еще не сохранен.")
	}
	data, err := readBoundedFile(filepath.Join(s.Options.Config.DataDir, "attempts", a.ID, "result.json"), 32<<20)
	if err != nil || len(data) > 32<<20 {
		return r, errors.New("result unavailable")
	}
	if json.Unmarshal(data, &r) != nil || hash(r) != a.ResultSHA256 || r.AttemptID != a.ID {
		return r, errors.New("result integrity check failed")
	}
	return r, nil
}
func (s *Service) readInput(a Attempt) (Data, error) {
	var input struct {
		Research Data `json:"research"`
	}
	data, err := readBoundedFile(filepath.Join(s.Options.Config.DataDir, "attempts", a.ID, "input.json"), 32<<20)
	if err != nil || len(data) > 32<<20 {
		return Data{}, errors.New("input unavailable")
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != a.InputSHA256 || json.Unmarshal(data, &input) != nil {
		return Data{}, errors.New("input integrity check failed")
	}
	return input.Research, nil
}
func (s *Service) Result(id string) (execution.Result, error) {
	v, err := s.Store.Read()
	if err != nil {
		return execution.Result{}, err
	}
	a := v.attempt(id)
	if a == nil {
		return execution.Result{}, RuleError("Попытка не найдена.")
	}
	return s.readResult(*a)
}
