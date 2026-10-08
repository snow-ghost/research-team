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
	"sync/atomic"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
)

type Service struct {
	schedulerCycles atomic.Int64
	profileMu       sync.RWMutex
	profileCatalog  map[string]execution.Profile
	profileLabels   map[string]string
	Options         Options
	Store           *Store
	mu              sync.Mutex
	running         map[string]context.CancelFunc
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	closeOnce       sync.Once
	closeErr        error
}
type RunRequest struct {
	ReviewVerification string            `json:"review_verification,omitempty"`
	RemoteWorker       string            `json:"remote_worker,omitempty"`
	Limits             *execution.Limits `json:"limits,omitempty"`
	ExpectedRevision   int               `json:"expected_revision"`
	RequestID          string            `json:"request_id"`
	TaskID             string            `json:"task_id"`
	Profile            string            `json:"profile"`
	Workspace          string            `json:"workspace"`
	Confirm            bool              `json:"confirm"`
}
type ProfileView struct {
	Limits    execution.Limits `json:"limits"`
	ID        string           `json:"id"`
	Label     string           `json:"label"`
	Kind      string           `json:"kind"`
	Provider  string           `json:"provider"`
	Model     string           `json:"model"`
	Available bool             `json:"available"`
	Skills    []string         `json:"skills"`
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
	store, err := OpenDatabase(o.Config, o.Lookup)
	if err != nil {
		return nil, err
	}
	if err = store.RequireImportedSQLite(o.Config.DataDir); err != nil {
		store.Close()
		return nil, err
	}
	if err = os.MkdirAll(filepath.Join(o.Config.DataDir, "attempts"), 0700); err != nil {
		store.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{Options: o, Store: store, running: map[string]context.CancelFunc{}, profileCatalog: map[string]execution.Profile{}, profileLabels: map[string]string{}, ctx: ctx, cancel: cancel}
	if err := validateWorkers(o.Config.Workers); err != nil {
		s.Close()
		return nil, err
	}
	if err := validateTelegram(o.Config.Telegram); err != nil {
		s.Close()
		return nil, err
	}
	v, err := store.Read()
	if err != nil {
		s.Close()
		return nil, err
	}
	for _, revision := range v.ProfileRevisions {
		if revision.ID != revision.Configuration.ID || revision.SHA256 != hash(revision.Configuration) || revision.Configuration.Validate() != nil {
			s.Close()
			return nil, errors.New("profile revision integrity mismatch")
		}
		s.profileCatalog[revision.ID] = cloneProfile(revision.Configuration)
		s.profileLabels[revision.ID] = revision.Label
	}
	for _, revision := range v.SkillRevisions {
		if revision.SHA256 != hash(revision.Configuration) || revision.Name != revision.Configuration.ID || revision.Configuration.Contract == nil || validateSkillContract(*revision.Configuration.Contract) != nil {
			s.Close()
			return nil, errors.New("skill revision integrity mismatch")
		}
	}
	interrupted := false
	for _, l := range v.Library {
		if l.Status == "running" {
			interrupted = true
		}
	}
	for _, a := range v.Attempts {
		if active(a.Status) {
			interrupted = true
		}
	}
	for _, c := range v.Cycles {
		if cycleActive(c.Status) {
			interrupted = true
		}
	}
	for _, v := range v.Verifications {
		if v.Status == "queued" || v.Status == "running" {
			interrupted = true
		}
	}
	for _, team := range v.Teams {
		if teamActive(team.Status) {
			interrupted = true
		}
	}
	if interrupted && !maintenanceValid(v.Data) {
		err = store.Change(0, "", "", "Прерванные попытки восстановлены как неизвестный исход", "", "server", func(d *Data) error {
			for i := range d.Library {
				if d.Library[i].Status == "running" {
					d.Library[i].Status = "interrupted"
				}
			}
			for i := range d.Teams {
				if teamActive(d.Teams[i].Status) {
					d.Teams[i].Status = "interrupted"
					d.Teams[i].Reason = "Сервер перезапущен; проверьте завершенные попытки."
				}
			}
			for i := range d.Verifications {
				if d.Verifications[i].Status == "queued" || d.Verifications[i].Status == "running" {
					d.Verifications[i].Status = "interrupted"
				}
			}
			for i := range d.Cycles {
				if cycleActive(d.Cycles[i].Status) {
					d.Cycles[i].Status = "interrupted"
					d.Cycles[i].Reason = "Сервер перезапущен. Проверьте результаты и отдельно разрешите новый цикл."
				}
			}
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
	if len(o.Config.Telegram) > 0 {
		s.wg.Add(1)
		go s.telegramLoop()
	}
	return s, nil
}
func (s *Service) Close() error {
	s.closeOnce.Do(func() { s.cancel(); s.wg.Wait(); s.closeErr = s.Store.Close() })
	return s.closeErr
}
func active(status string) bool {
	return status == "queued" || status == "preparing" || status == "running" || status == "awaiting_worker" || status == "cancelling"
}
func (s *Service) Profiles() []ProfileView {
	out := []ProfileView{}
	for id, p := range s.profileList() {
		v := ProfileView{ID: id, Label: s.Options.Labels[id], Kind: p.Kind, Limits: p.Limits, Available: true, Skills: []string{}}
		s.profileMu.RLock()
		if label := s.profileLabels[id]; label != "" {
			v.Label = label
		}
		s.profileMu.RUnlock()
		if v.Label == "" {
			v.Label = id
		}
		keys := []string{}
		if p.Model != nil {
			if modelHasLeanTool(p) && s.Options.Checker == nil {
				v.Available = false
			}
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
			return s.attachProof(d, a.Target, a.Attempt)
		})
	}
	return s.Store.Change(a.ExpectedRevision, a.RequestID, hash(a), actionLabel(a.Type), a.Target, "operator", func(d *Data) error {
		if a.Type == "SUBMIT_REVIEW" {
			e := d.entity(a.Target)
			if e != nil && e.Status == "challenged" && e.FormalGoal != nil && !hasVerifiedProof(d, e) {
				return RuleError("Оспоренный исходник требует действительной проверки Lean перед повторной рецензией.")
			}
		}
		if a.Type == "REVIEW" && a.Decision == "accept" {
			e := d.entity(a.Target)
			if e != nil && e.FormalGoal != nil && !hasVerifiedProof(d, e) {
				return RuleError("Нужна успешная проверка Lean для текущего доказательства и цели.")
			}
			if e != nil && e.FormalGoal != nil {
				v := proofVerification(d, e)
				if v == nil || v.Report.AuditSHA256 != leancheck.AuditDigest() {
					return RuleError("Нужна новая проверка Lean актуальной программой аудита.")
				}
			}
			if e != nil && !teamReviewReady(d, e) {
				return RuleError("Дождитесь независимого задания рецензирования команды.")
			}
			if e != nil && e.ResearchResult != "" {
				valid := false
				for _, r := range d.Results {
					if r.ID == e.ResearchResult && r.Status == "ready" && resultMatches(d, r) {
						valid = true
					}
				}
				if !valid {
					return RuleError("Связанная запись результата устарела или содержит незавершенные проверки.")
				}
			}
		}
		return applyAction(d, a)
	})
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
	profile, ok := s.lookupProfile(r.Profile)
	if !ok {
		return RuleError("Исполнитель не разрешен сервером.")
	}
	if profile.Kind == "external" && !s.Options.Config.AllowExternalExecution {
		return RuleError("Внешние процессы запрещены настройками сервера.")
	}
	if r.Limits != nil {
		profile.Limits = *r.Limits
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
		var binding *ProofBinding
		reviewOf := ""
		if r.ReviewVerification != "" {
			v := d.verification(r.ReviewVerification)
			if t.Kind != "review" || v == nil || v.Target != item.ID || !verifiedReportMatches(*v) || !verificationMatches(d, *v) || v.TargetRevision != item.Revision || (v.Origin == "submitted" && v.SubmittedRevision != item.Revision) || v.Author == "executor:"+r.Profile {
				return RuleError("Рецензии нужен проверенный файл текущей версии и другой автор.")
			}
			if v.Attempt != "" {
				author := d.attempt(v.Attempt)
				if author == nil || author.Profile == r.Profile {
					return RuleError("Для рецензии нужен другой профиль автора исходной попытки.")
				}
				reviewOf = author.ID
			}
			binding = &ProofBinding{v.ID, v.Report.GoalSHA256, v.Report.SourceSHA256}
		}
		if r.RemoteWorker != "" {
			if err := s.workerScope(*d, r.RemoteWorker, r.Profile, item.ID); err != nil {
				return err
			}
		}
		reservation, err := s.reserveStudyBudget(d, item.ID, profile)
		if err != nil {
			return err
		}
		id := identifier("run")
		t.Attempt = id
		t.Agent = r.Profile
		d.Attempts = append(d.Attempts, Attempt{ID: id, TaskID: t.ID, Target: item.ID, TargetRevision: item.Revision,
			Profile: r.Profile, ProfileConfiguration: &profile, RemoteWorker: r.RemoteWorker, Limits: &profile.Limits, ProofBinding: binding, ReviewOf: reviewOf, ReservedOutputTokens: reservation, ReservedModelRequests: profile.Limits.MaxSteps, Workspace: r.Workspace, Status: "queued", CreatedAt: time.Now().UTC(),
			InputSnapshot: d.Revision + 1, RemoteOutcome: "not_started"})
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
		if a.Status == "queued" || a.RemoteWorker != "" {
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
	lastRevision := 0
	var deadline time.Time
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			revision, err := s.Store.Revision()
			if err != nil || (revision == lastRevision && (deadline.IsZero() || time.Now().Before(deadline))) {
				continue
			}
			lastRevision = revision
			s.schedulerCycles.Add(1)
			s.expireStudyBudgets()
			s.expireWorkerLeases()
			s.advanceCycles()
			s.advanceTeams()
			s.advanceBranches()
			s.startChecks()
			s.startLibraryBuilds()
			if s.ctx.Err() != nil {
				return
			}
			s.mu.Lock()
			if len(s.running) >= s.Options.Config.MaxParallel {
				s.mu.Unlock()
				continue
			}
			v, err := s.Store.Read()
			if err == nil {
				deadline = nextSchedulerDeadline(v.Data, time.Now())
			}
			if err == nil {
				for id, cancel := range s.running {
					if a := v.attempt(id); a != nil && a.Status == "interrupted" {
						cancel()
					}
					for _, check := range v.Verifications {
						if id == "verify:"+check.ID && check.Status == "interrupted" {
							cancel()
						}
					}
					for _, module := range v.Library {
						if id == "library:"+module.ID && module.Status == "interrupted" {
							cancel()
						}
					}
				}
			}
			if err != nil || v.Paused {
				s.mu.Unlock()
				continue
			}
			for _, a := range v.Attempts {
				if a.Status == "queued" {
					if a.TeamID != "" {
						team := v.team(a.TeamID)
						if team == nil || team.Status != "running" {
							continue
						}
					}
					if a.CycleID != "" {
						c := v.cycle(a.CycleID)
						if c == nil || c.Status != "running" {
							continue
						}
					}
					err = s.Store.Change(0, "", "", "Подготовка снимка входных файлов", a.Target, "server", func(d *Data) error {
						current := d.attempt(a.ID)
						if current == nil || current.Status != "queued" || d.Paused {
							return ErrConflict
						}
						if current.CycleID != "" {
							c := d.cycle(current.CycleID)
							if c == nil || c.Status != "running" || d.effective(c.Goal, map[string]bool{}) == "accepted" {
								return ErrConflict
							}
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
	if errors.Is(err, errWorkerQueued) {
		s.mu.Lock()
		if cancel := s.running[a.ID]; cancel != nil {
			cancel()
		}
		delete(s.running, a.ID)
		s.mu.Unlock()
		return
	}
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
		if current.Status == "cancelling" || current.Status == "interrupted" || ctx.Err() != nil {
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
		if current.StopCause == "" {
			current.StopCause = attemptStopCause(result, current.Limits)
		}
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
	if a.ParentAttempt != "" {
		source = filepath.Join(s.Options.Config.DataDir, "attempts", a.ParentAttempt, "workspace")
	}
	files, err := copyWorkspace(ctx, source, filepath.Join(dir, "workspace"))
	if err != nil {
		return err
	}
	files, err = prepareGoalFiles(d, a, filepath.Join(dir, "workspace"), files)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	profile := attemptProfile(s, a)
	if a.Limits != nil {
		profile.Limits = *a.Limits
	}
	input := struct {
		Research Data              `json:"research"`
		Files    []FileDigest      `json:"files"`
		Profile  execution.Profile `json:"profile"`
	}{d, files, profile}
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
	materials := []map[string]string{}
	for _, name := range []string{"TASK.md", "Goal.lean", "Candidate.template.lean"} {
		if content, readErr := readBoundedFile(filepath.Join(dir, "workspace", name), 65536); readErr == nil {
			materials = append(materials, map[string]string{"path": name, "content": string(content)})
		}
	}
	previous := s.continuationContext(d, a)
	modules := []map[string]any{}
	for _, l := range d.Library {
		if l.Status == "ready" && libraryMatches(&d, l) {
			for _, e := range relevant {
				if e.ID == l.Lemma {
					modules = append(modules, map[string]any{"module": l.Module, "lemma": l.Lemma, "revision": l.LemmaRevision, "declaration": l.Goal.Declaration, "candidate": l.Goal.Candidate, "artifact_sha256": l.Report.ArtifactSHA256})
				}
			}
		}
	}
	contextData, _ := json.Marshal(struct {
		CandidateTemplate string                    `json:"candidate_template,omitempty"`
		Memory            []MemoryHit               `json:"research_memory,omitempty"`
		ToolGoals         map[string]leancheck.Goal `json:"tool_goals,omitempty"`
		Entities          []Entity                  `json:"entities"`
		Questions         []Question                `json:"questions"`
		Materials         []map[string]string       `json:"materials"`
		Previous          any                       `json:"previous_unverified,omitempty"`
		Team              any                       `json:"team_materials,omitempty"`
		Library           []Entity                  `json:"accepted_lemmas,omitempty"`
		Modules           []map[string]any          `json:"lean_modules,omitempty"`
		Review            any                       `json:"review_materials,omitempty"`
	}{candidateTemplateFor(d, a), memoryForTarget(d, a.Target), attemptToolGoals(d, a, profile), relevant, questionsFor(d, a.Target), materials, previous, s.teamContext(d, a), acceptedLemmas(d, a.Target), modules, reviewMaterials(d, a)})
	if a.RemoteWorker != "" {
		selected := []Entity{}
		for _, e := range relevant {
			if e.Study == entity.Study {
				selected = append(selected, e)
			}
		}
		contextData, _ = json.Marshal(map[string]any{"candidate_template": candidateTemplateFor(d, a), "entities": selected, "questions": questionsFor(d, a.Target), "materials": materials, "team_materials": s.teamContext(d, a), "previous_unverified": previous, "review_materials": reviewMaterials(d, a)})
	}
	t := execution.Task{ID: task.ID, AttemptID: a.ID, Snapshot: inputSHA, LeaseEpoch: 1, Workspace: filepath.Join(dir, "workspace"), Objective: task.Objective, Context: string(contextData)}
	_, ok := s.lookupProfile(a.Profile)
	if !ok {
		return errors.New("profile unavailable")
	}
	var executor execution.Executor
	if a.RemoteWorker == "" {
		ctx, err = s.leanToolContext(ctx, d, a)
		if err != nil {
			return err
		}
		executor, err = s.Options.Factory(profile, s.Options.Lookup)
	}
	if err != nil {
		return err
	}
	if a.RemoteWorker != "" {
		t.Workspace = ""
		packet := WorkerPacket{Task: t, Skills: profile.Skills, Limits: profile.Limits, Profile: profile.ID, ProfileSHA256: hash(profile)}
		for _, f := range files {
			if f.Path == "TASK.md" || f.Path == "Goal.lean" {
				packet.Files = append(packet.Files, f)
			}
		}
		body, _ := json.Marshal(packet)
		if err := atomicFile(filepath.Join(dir, "worker-task.json"), body); err != nil {
			return err
		}
	}
	s.mu.Lock()
	err = s.Store.Change(0, "", "", "Исполнитель запущен", a.Target, "server", func(d *Data) error {
		current := d.attempt(a.ID)
		if current.Status != "preparing" || ctx.Err() != nil {
			return context.Canceled
		}
		current.Status = "running"
		if a.RemoteWorker != "" {
			current.Status = "awaiting_worker"
		}
		current.InputSHA256 = inputSHA
		current.RemoteOutcome = "unknown"
		d.task(a.TaskID).State = "running"
		return nil
	})
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if a.RemoteWorker != "" {
		return errWorkerQueued
	}
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	journal, jErr := openJournal(dir, s.profileSecrets(profile), stop)
	if jErr != nil {
		return jErr
	}
	defer journal.Close()
	journal.observe(execution.Event{Type: "attempt_started"})
	runCtx = execution.WithObserver(runCtx, journal.observe)
	*result, err = executor.Run(runCtx, t)
	journal.observe(execution.Event{Type: result.Status, Usage: result.Usage})
	journal.Close()
	if jErr := journal.Err(); jErr != nil {
		err = errors.Join(err, jErr)
	}
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
func (s *Service) safeResultContent(a Attempt, result execution.Result) bool {
	p := attemptProfile(s, a)
	return p.External == nil || p.External.Provider != "coddy-agent" || result.ContentPolicy == execution.CoddyContentPolicy
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
