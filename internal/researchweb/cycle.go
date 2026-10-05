package researchweb

import (
	"errors"
	"time"
)

type Cycle struct {
	ID               string    `json:"id"`
	Study            string    `json:"study"`
	Goal             string    `json:"goal"`
	Profile          string    `json:"profile"`
	ProfileSHA       string    `json:"profile_sha256"`
	Workspace        string    `json:"workspace"`
	MaxAttempts      int       `json:"max_attempts"`
	UsedAttempts     int       `json:"used_attempts"`
	Status           string    `json:"status"`
	Reason           string    `json:"reason"`
	CurrentAttempt   string    `json:"current_attempt,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	AcceptedSnapshot int       `json:"accepted_snapshot,omitempty"`
}
type CycleRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Study            string `json:"study"`
	Profile          string `json:"profile"`
	Workspace        string `json:"workspace"`
	MaxAttempts      int    `json:"max_attempts"`
	Confirm          bool   `json:"confirm"`
}
type CycleCommand struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Kind             string `json:"kind"`
	Confirm          bool   `json:"confirm"`
}

func (d *Data) cycle(id string) *Cycle {
	for i := range d.Cycles {
		if d.Cycles[i].ID == id {
			return &d.Cycles[i]
		}
	}
	return nil
}
func cycleActive(status string) bool {
	return status == "running" || status == "awaiting_review" || status == "paused" || status == "blocked"
}
func (s *Service) validateExecutor(profile, workspace string) error {
	p, ok := s.Options.Profiles[profile]
	if !ok {
		return RuleError("Исполнитель не разрешен сервером.")
	}
	if p.Kind == "external" && !s.Options.Config.AllowExternalExecution {
		return RuleError("Внешние процессы запрещены настройками сервера.")
	}
	if err := p.Validate(); err != nil {
		return err
	}
	available := false
	for _, v := range s.Profiles() {
		if v.ID == profile {
			available = v.Available
		}
	}
	if !available {
		return RuleError("Исполнитель недоступен. Проверьте настройки и учетные данные.")
	}
	for _, w := range s.Options.Config.Workspaces {
		if w.ID == workspace {
			return nil
		}
	}
	return RuleError("Каталог не разрешен сервером.")
}
func (s *Service) StartCycle(r CycleRequest) error {
	if !r.Confirm {
		return RuleError("Нужно подтверждение автоматических запусков и возможных расходов.")
	}
	if r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) {
		return RuleError("Нужны версия снимка и идентификатор команды.")
	}
	if r.MaxAttempts < 1 || r.MaxAttempts > 20 {
		return RuleError("Число попыток должно быть от 1 до 20.")
	}
	if err := s.validateExecutor(r.Profile, r.Workspace); err != nil {
		return err
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Начат исследовательский цикл", r.Study, "operator", func(d *Data) error {
		if d.Paused {
			return RuleError("Новые запуски приостановлены.")
		}
		for _, team := range d.Teams {
			if team.Study == r.Study && teamActive(team.Status) {
				return RuleError("Сначала завершите работу команды.")
			}
		}
		for _, c := range d.Cycles {
			if c.Study == r.Study && cycleActive(c.Status) {
				return RuleError("У исследования уже есть незавершенный цикл.")
			}
		}
		for _, study := range d.Studies {
			if study.ID != r.Study {
				continue
			}
			goal := d.entity(study.Goal)
			if goal == nil || d.effective(goal.ID, map[string]bool{}) == "accepted" || goal.Status == "refuted" {
				return RuleError("Нужна открытая цель исследования.")
			}
			d.Cycles = append(d.Cycles, Cycle{ID: identifier("cycle"), Study: study.ID, Goal: study.Goal, Profile: r.Profile,
				ProfileSHA: hash(s.Options.Profiles[r.Profile]), Workspace: r.Workspace, MaxAttempts: r.MaxAttempts,
				Status: "running", Reason: "Выбор следующего обязательства.", CreatedAt: time.Now().UTC()})
			return nil
		}
		return RuleError("Исследование не найдено.")
	})
}
func (s *Service) ControlCycle(id string, r CycleCommand) error {
	if r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) {
		return RuleError("Нужны версия снимка и идентификатор команды.")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.Store.Change(r.ExpectedRevision, r.RequestID, hash(struct {
		ID      string
		Command CycleCommand
	}{id, r}), "Изменено управление циклом", id, "operator", func(d *Data) error {
		c := d.cycle(id)
		if c == nil {
			return RuleError("Цикл не найден.")
		}
		switch r.Kind {
		case "pause":
			if c.Status != "running" && c.Status != "awaiting_review" {
				return RuleError("Цикл сейчас не выполняется.")
			}
			c.Status = "paused"
			c.Reason = "Новые задания цикла приостановлены; текущая попытка продолжает работу."
		case "resume":
			if !r.Confirm {
				return RuleError("Продолжение требует подтверждения возможных расходов.")
			}
			if c.Status != "paused" && c.Status != "blocked" {
				return RuleError("Для завершенного или прерванного цикла создайте новый запуск.")
			}
			if err := s.validateExecutor(c.Profile, c.Workspace); err != nil {
				return err
			}
			if c.ProfileSHA != hash(s.Options.Profiles[c.Profile]) {
				return RuleError("Профиль изменился. Создайте новый цикл.")
			}
			c.Status = "running"
			c.Reason = "Продолжение подтверждено оператором."
		case "stop":
			if !cycleActive(c.Status) {
				return RuleError("Цикл уже завершен.")
			}
			c.Status = "stopped"
			c.Reason = "Остановлен оператором."
			cancelCycle(d, c.ID)
		default:
			return RuleError("Неизвестная команда цикла.")
		}
		return nil
	})
	if err == nil {
		s.cancelRequested()
	}
	return err
}
func cancelCycle(d *Data, id string) {
	for i := range d.Attempts {
		a := &d.Attempts[i]
		if a.CycleID != id || !active(a.Status) {
			continue
		}
		if a.Status == "queued" {
			a.Status = "cancelled"
			now := time.Now().UTC()
			a.FinishedAt = &now
		} else {
			a.Status = "cancelling"
		}
		if t := d.task(a.TaskID); t != nil {
			t.State = a.Status
		}
	}
}

// Caller holds s.mu; cancellation follows the durable state change.
func (s *Service) cancelRequested() {
	v, err := s.Store.Read()
	if err != nil {
		s.cancel()
		return
	}
	for _, a := range v.Attempts {
		if a.Status == "cancelling" {
			if cancel := s.running[a.ID]; cancel != nil {
				cancel()
			}
		}
	}
}

var errNoCycleChange = errors.New("no cycle transition")

func (s *Service) advanceCycles() {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := s.Store.Read()
	if err != nil {
		s.cancel()
		return
	}
	for _, c := range v.Cycles {
		if !cycleActive(c.Status) {
			continue
		}
		err = s.Store.Change(0, "", "", "Шаг исследовательского цикла", c.ID, "coordinator", func(d *Data) error {
			current := d.cycle(c.ID)
			if current == nil || !cycleActive(current.Status) {
				return errNoCycleChange
			}
			return s.advanceCycle(d, current)
		})
		if err != nil && !errors.Is(err, errNoCycleChange) {
			s.cancel()
			return
		}
	}
	if len(v.Cycles) > 0 {
		s.cancelRequested()
	}
}
func (s *Service) advanceCycle(d *Data, c *Cycle) error {
	set := func(status, reason string) error {
		if c.Status == status && c.Reason == reason {
			return errNoCycleChange
		}
		c.Status = status
		c.Reason = reason
		return nil
	}
	goal := d.entity(c.Goal)
	if goal == nil {
		return set("blocked", "Цель отсутствует.")
	}
	if d.effective(c.Goal, map[string]bool{}) == "accepted" {
		cancelCycle(d, c.ID)
		c.AcceptedSnapshot = d.Revision
		return set("completed", "Цель принята оператором; основания действительны.")
	}
	if status := d.effective(c.Goal, map[string]bool{}); status == "blocked" || status == "challenged" || status == "refuted" {
		cancelCycle(d, c.ID)
		return set("blocked", "Цель или ее основания оспорены, опровергнуты либо изменились.")
	}
	if c.Status == "paused" || c.Status == "blocked" {
		return errNoCycleChange
	}
	if c.ProfileSHA != hash(s.Options.Profiles[c.Profile]) {
		cancelCycle(d, c.ID)
		return set("interrupted", "Настройки исполнителя изменились.")
	}
	if c.CurrentAttempt != "" {
		a := d.attempt(c.CurrentAttempt)
		if a == nil {
			return set("failed", "Запись попытки отсутствует.")
		}
		if active(a.Status) {
			return errNoCycleChange
		}
		if a.Status != "candidate" {
			return set("failed", "Попытка завершилась без кандидата. Автоматического повтора нет.")
		}
		item := d.entity(a.Target)
		if item == nil {
			return set("blocked", "Целевое утверждение отсутствует.")
		}
		if item.ProofAttempt == a.ID && item.Status == "accepted" && d.effective(item.ID, map[string]bool{}) == "accepted" {
			c.CurrentAttempt = ""
			c.Status = "running"
			c.Reason = "Основание принято. Выбор следующего обязательства."
			return nil
		}
		if item.ProofAttempt == a.ID && item.Status == "needs_changes" {
			c.CurrentAttempt = ""
			c.Status = "running"
			c.Reason = "Запрошены исправления; они будут учтены в следующей попытке."
			return nil
		}
		if item.ProofAttempt != a.ID {
			if err := s.attachProof(d, item.ID, a.ID); err != nil {
				return set("blocked", "Кандидат нельзя присоединить к текущей версии. Требуется проверка оператора.")
			}
		}
		if item.Status != "in_review" {
			return set("blocked", "Состояние кандидата изменилось. Требуется проверка оператора.")
		}
		return set("awaiting_review", "Кандидат сохранен. Ожидается решение оператора.")
	}
	if d.Paused {
		return errNoCycleChange
	}
	if c.UsedAttempts >= c.MaxAttempts {
		return set("exhausted", "Исчерпано разрешенное число попыток.")
	}
	if len(d.Attempts) >= 100 || len(d.Tasks) >= 2000 {
		return set("exhausted", "Достигнут предел заданий или попыток рабочей области.")
	}
	target, reason := nextObligation(d, c.Goal, map[string]bool{})
	if target == nil {
		return set("blocked", reason)
	}
	if target.Study != c.Study {
		return set("blocked", "Непринятое основание относится к другому исследованию.")
	}
	for _, a := range d.Attempts {
		if a.Target == target.ID && active(a.Status) {
			return set("blocked", "Для выбранного утверждения уже выполняется другая попытка.")
		}
	}
	if err := s.validateExecutor(c.Profile, c.Workspace); err != nil {
		return set("blocked", "Исполнитель или каталог недоступен.")
	}
	p := s.Options.Profiles[c.Profile]
	reservation, err := s.reserveStudyBudget(d, target.ID, p)
	if err != nil {
		return set("blocked", err.Error())
	}
	d.addTask(target.ID, "Доказательство: "+target.Title, "proof", "Построй доказательство указанного утверждения. Проверь предпосылки, используй только действительные принятые основания. Если доказательство не найдено, явно укажи пробелы. Замечания предыдущей рецензии: "+target.ReviewReason)
	task := &d.Tasks[len(d.Tasks)-1]
	id := identifier("run")
	task.CycleID = c.ID
	task.Attempt = id
	task.Agent = c.Profile
	d.Attempts = append(d.Attempts, Attempt{ID: id, TaskID: task.ID, Target: target.ID, TargetRevision: target.Revision,
		CycleID: c.ID, Profile: c.Profile, Limits: &p.Limits, ReservedOutputTokens: reservation, ReservedModelRequests: p.Limits.MaxSteps, Workspace: c.Workspace, Status: "queued", CreatedAt: time.Now().UTC(),
		InputSnapshot: d.Revision + 1, RemoteOutcome: "not_started"})
	c.UsedAttempts++
	c.CurrentAttempt = id
	return set("running", "Создано задание для обязательства: "+target.Title)
}
func nextObligation(d *Data, id string, seen map[string]bool) (*Entity, string) {
	e := d.entity(id)
	if e == nil {
		return nil, "Отсутствует одно из оснований."
	}
	if seen[id] {
		return nil, "Обнаружен цикл зависимостей."
	}
	if d.effective(id, map[string]bool{}) == "accepted" {
		return nil, "Все обязательства приняты."
	}
	if e.Status == "accepted" || e.Status == "challenged" || e.Status == "refuted" {
		return nil, "Есть недействительное или оспоренное основание."
	}
	seen[id] = true
	defer delete(seen, id)
	for _, dep := range e.Dependencies {
		if d.effective(dep, map[string]bool{}) != "accepted" {
			return nextObligation(d, dep, seen)
		}
	}
	if e.Status == "in_review" {
		return nil, "Обязательство уже ожидает приемки."
	}
	return e, ""
}

func (s *Service) attachProof(d *Data, target, id string) error {
	attempt, item := d.attempt(id), d.entity(target)
	if attempt == nil || item == nil || attempt.Status != "candidate" || attempt.Target != item.ID || attempt.TargetRevision != item.Revision {
		return RuleError("Материал не соответствует текущей версии утверждения.")
	}
	if item.Status == "accepted" || item.Status == "refuted" {
		return RuleError("Принятая версия не изменяется.")
	}
	task := d.task(attempt.TaskID)
	if task == nil || (task.Kind != "proof" && task.Kind != "formalize") {
		return RuleError("Для доказательства нужно задание вида proof.")
	}
	result, err := s.readResult(*attempt)
	if err != nil {
		return err
	}
	if !s.safeResultContent(*attempt, result) {
		return RuleError("Нужна новая попытка после исправления разделения текста Coddy.")
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
	item.ProofVerification = ""
	item.DependencyRevisions = pins
	item.Status = "in_review"
	for i := range d.Verifications {
		v := &d.Verifications[i]
		if v.Attempt == attempt.ID && verificationMatches(d, *v) {
			v.TargetRevision = item.Revision + 1
		}
	}
	item.Revision++
	return nil
}
