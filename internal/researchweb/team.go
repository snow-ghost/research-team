package researchweb

import (
	"errors"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
)

type ResearchTeam struct {
	Refuting       bool                        `json:"refuting,omitempty"`
	Workers        map[string]string           `json:"workers,omitempty"`
	OperatorNote   string                      `json:"operator_note,omitempty"`
	RetryRole      string                      `json:"retry_role,omitempty"`
	RoleLimits     map[string]execution.Limits `json:"role_limits,omitempty"`
	ID             string                      `json:"id"`
	Study          string                      `json:"study"`
	Goal           string                      `json:"goal"`
	Target         string                      `json:"target,omitempty"`
	TargetRevision int                         `json:"target_revision,omitempty"`
	Profiles       map[string]string           `json:"profiles"`
	ProfileHashes  map[string]string           `json:"profile_hashes"`
	Workspace      string                      `json:"workspace"`
	MaxAttempts    int                         `json:"max_attempts"`
	UsedAttempts   int                         `json:"used_attempts"`
	RequireLean    bool                        `json:"require_lean"`
	Status         string                      `json:"status"`
	Stage          string                      `json:"stage"`
	Reason         string                      `json:"reason"`
	Current        map[string]string           `json:"current"`
	Verification   string                      `json:"verification,omitempty"`
	CreatedAt      time.Time                   `json:"created_at"`
}
type TeamRequest struct {
	Workers          map[string]string `json:"workers,omitempty"`
	ExpectedRevision int               `json:"expected_revision"`
	RequestID        string            `json:"request_id"`
	Study            string            `json:"study"`
	Target           string            `json:"target,omitempty"`
	Profiles         map[string]string `json:"profiles"`
	Workspace        string            `json:"workspace"`
	MaxAttempts      int               `json:"max_attempts"`
	RequireLean      bool              `json:"require_lean"`
	Confirm          bool              `json:"confirm"`
}
type TeamCommand struct {
	Note             string                      `json:"note,omitempty"`
	RoleLimits       map[string]execution.Limits `json:"role_limits,omitempty"`
	MaxAttempts      int                         `json:"max_attempts,omitempty"`
	ExpectedRevision int                         `json:"expected_revision"`
	RequestID        string                      `json:"request_id"`
	Kind             string                      `json:"kind"`
	Confirm          bool                        `json:"confirm"`
}

func teamActive(status string) bool {
	return status == "running" || status == "paused" || status == "blocked" || status == "awaiting_review"
}
func (d *Data) team(id string) *ResearchTeam {
	for i := range d.Teams {
		if d.Teams[i].ID == id {
			return &d.Teams[i]
		}
	}
	return nil
}
func (s *Service) StartTeam(r TeamRequest) error {
	if !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || r.MaxAttempts < 4 || r.MaxAttempts > 20 {
		return RuleError("Нужны подтверждение и предел от 4 до 20 попыток.")
	}
	if r.RequireLean && s.Options.Checker == nil {
		return RuleError("Проверка Lean не настроена.")
	}
	required := []string{"proof", "counterexample", "review"}
	if len(r.Profiles) > 4 || len(r.Workers) > 4 {
		return RuleError("У команды предусмотрены четыре роли.")
	}
	for role := range r.Profiles {
		if role != "proof" && role != "counterexample" && role != "formalize" && role != "review" {
			return RuleError("Неизвестная роль.")
		}
	}
	if r.RequireLean {
		required = append(required, "formalize")
	}
	pins := map[string]string{}
	for _, role := range required {
		profile := r.Profiles[role]
		if err := s.validateAssignment(profile, r.Workspace, r.Workers[role]); err != nil {
			return err
		}
		pins[role] = hash(s.profile(profile))
	}
	author := r.Profiles["proof"]
	if r.RequireLean {
		author = r.Profiles["formalize"]
	}
	if r.Profiles["review"] == author || r.Profiles["review"] == r.Profiles["proof"] || r.Profiles["review"] == r.Profiles["counterexample"] {
		return RuleError("Для рецензии нужен отдельный профиль.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Начата работа команды", r.Study, "operator", func(d *Data) error {
		if d.Paused {
			return RuleError("Новые запуски приостановлены.")
		}
		for _, team := range d.Teams {
			if team.Study == r.Study && teamActive(team.Status) {
				return RuleError("У исследования уже есть действующая команда.")
			}
		}
		for _, cycle := range d.Cycles {
			if cycle.Study == r.Study && cycleActive(cycle.Status) {
				return RuleError("Сначала завершите отдельный цикл исследования.")
			}
		}
		for _, study := range d.Studies {
			if study.ID != r.Study {
				continue
			}
			goalID := study.Goal
			if r.Target != "" {
				goalID = r.Target
			}
			goal := d.entity(goalID)
			if goal == nil || goal.Study != study.ID {
				return RuleError("Обязательство должно относиться к выбранному исследованию.")
			}
			if goal == nil || goal.Status == "accepted" || goal.Status == "refuted" {
				return RuleError("Нужна открытая цель.")
			}
			if r.RequireLean && goal.FormalGoal == nil {
				return RuleError("Сначала закрепите формальную цель.")
			}
			for role, id := range r.Workers {
				if id != "" {
					if err := s.workerScope(*d, id, r.Profiles[role], goal.ID); err != nil {
						return err
					}
				}
			}
			d.Teams = append(d.Teams, ResearchTeam{ID: identifier("team"), Study: study.ID, Goal: goal.ID,
				Profiles: r.Profiles, Workers: r.Workers, ProfileHashes: pins, Workspace: r.Workspace, MaxAttempts: r.MaxAttempts, RequireLean: r.RequireLean,
				Status: "running", Stage: "planning", Current: map[string]string{}, CreatedAt: time.Now().UTC(), Reason: "Выбор обязательства."})
			return nil
		}
		return RuleError("Исследование не найдено.")
	})
}
func (s *Service) ControlTeam(id string, r TeamCommand) error {
	if r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || len(r.Note) > 4000 {
		return RuleError("Нужны снимок и идентификатор команды.")
	}
	if r.MaxAttempts != 0 && (r.MaxAttempts < 4 || r.MaxAttempts > 20 || (r.Kind != "resume" && r.Kind != "configure")) {
		return RuleError("Предел от 4 до 20 попыток меняется при настройке или продолжении команды.")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Команда: "+r.Kind, id, "operator", func(d *Data) error {
		team := d.team(id)
		if team == nil || (!teamActive(team.Status) && team.Status != "interrupted") {
			return RuleError("Команда уже завершена.")
		}
		switch r.Kind {
		case "pause":
			team.Status = "paused"
			team.Reason = "Приостановлена оператором."
		case "reprocess":
			if !r.Confirm || (team.Status != "blocked" && team.Status != "interrupted") || (team.Stage != "exploring" && team.Stage != "review" && team.Stage != "refutation_review") || r.MaxAttempts != 0 || len(r.RoleLimits) != 0 {
				return RuleError("Повторная обработка доступна заблокированному отчету без изменения ограничений.")
			}
			for _, attempt := range team.Current {
				if a := d.attempt(attempt); a == nil || a.Status != "candidate" {
					return RuleError("Все текущие ответы должны быть завершены.")
				}
			}
			team.RetryRole = ""
			team.Status = "running"
			team.OperatorNote = r.Note
			team.Reason = "Разрешена повторная обработка сохраненных отчетов."
		case "resume", "configure":
			if !r.Confirm {
				return RuleError("Подтвердите продолжение и возможные расходы.")
			}
			if r.Kind == "resume" || r.Note != "" {
				team.OperatorNote = r.Note
			}
			if team.Status != "paused" && team.Status != "blocked" && team.Status != "interrupted" {
				return RuleError("Команда уже работает.")
			}
			if r.MaxAttempts != 0 {
				if r.MaxAttempts < team.UsedAttempts {
					return RuleError("Предел не может быть меньше числа использованных попыток.")
				}
				team.MaxAttempts = r.MaxAttempts
			}
			for _, other := range d.Teams {
				if other.ID != id && other.Study == team.Study && teamActive(other.Status) {
					return RuleError("Другая команда уже работает над исследованием.")
				}
			}
			for role, limits := range r.RoleLimits {
				profile, ok := s.lookupProfile(team.Profiles[role])
				if !ok {
					return RuleError("Неизвестная роль.")
				}
				profile.Limits = limits
				if err := profile.Validate(); err != nil {
					return err
				}
				if team.RoleLimits == nil {
					team.RoleLimits = map[string]execution.Limits{}
				}
				team.RoleLimits[role] = limits
			}
			if r.Kind == "configure" {
				team.Reason = "Ограничения обновлены без запуска новых попыток."
				return nil
			}
			for role, attemptID := range team.Current {
				a := d.attempt(attemptID)
				if a != nil && !active(a.Status) && a.Status != "candidate" {
					if err := s.queueTeamAttempt(d, team, role, a.ID); err != nil {
						return err
					}
				}
			}
			if team.RetryRole != "" {
				role := team.RetryRole
				if err := s.queueTeamAttempt(d, team, role, team.Current[role]); err != nil {
					return err
				}
				team.RetryRole = ""
			}
			team.Status = "running"
			if team.Stage == "acceptance" || team.Stage == "refutation_acceptance" {
				if e := d.entity(team.Target); e != nil && e.Status == "in_review" {
					team.Status = "awaiting_review"
				}
			}
			team.Reason = "Продолжение работы."
		case "stop":
			team.Status = "cancelled"
			team.Reason = "Остановлена оператором."
			cancelTeam(d, id)
		default:
			return RuleError("Неизвестная команда.")
		}
		return nil
	})
	if err == nil {
		s.cancelRequested()
	}
	return err
}
func cancelTeam(d *Data, id string) {
	for i := range d.Attempts {
		a := &d.Attempts[i]
		if a.TeamID == id && active(a.Status) {
			if a.Status == "queued" {
				a.Status = "cancelled"
			} else {
				a.Status = "cancelling"
			}
			if task := d.task(a.TaskID); task != nil {
				task.State = a.Status
			}
		}
	}
}
func (s *Service) advanceTeams() {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := s.Store.Read()
	if err != nil {
		return
	}
	for _, team := range v.Teams {
		if !teamActive(team.Status) && !(team.Status == "interrupted" && teamGoalClosed(&v.Data, team)) {
			continue
		}
		err = s.Store.Change(0, "", "", "Шаг команды", team.ID, "coordinator", func(d *Data) error {
			current := d.team(team.ID)
			if current == nil || (!teamActive(current.Status) && !(current.Status == "interrupted" && teamGoalClosed(d, *current))) {
				return errNoCycleChange
			}
			return s.advanceTeam(d, current)
		})
		if err != nil && !errors.Is(err, errNoCycleChange) {
			s.cancel()
			return
		}
	}
	s.cancelRequested()
}

func teamGoalClosed(d *Data, team ResearchTeam) bool {
	e := d.entity(team.Goal)
	return e != nil && (e.Status == "refuted" || d.effective(e.ID, map[string]bool{}) == "accepted")
}
func (s *Service) advanceTeam(d *Data, team *ResearchTeam) error {
	block := func(reason string) error { team.Status = "blocked"; team.Reason = reason; return nil }
	if goal := d.entity(team.Goal); goal != nil && goal.Status == "refuted" {
		cancelTeam(d, team.ID)
		team.Status = "completed"
		team.Reason = "Оператор принял проверенное отрицание цели."
		return nil
	}
	if d.effective(team.Goal, map[string]bool{}) == "accepted" {
		cancelTeam(d, team.ID)
		team.Status = "completed"
		team.Reason = "Цель принята оператором."
		return nil
	}
	if team.Status == "paused" || team.Status == "blocked" || d.Paused {
		return errNoCycleChange
	}
	for role, pin := range team.ProfileHashes {
		if hash(s.profile(team.Profiles[role])) != pin {
			cancelTeam(d, team.ID)
			team.Status = "interrupted"
			team.Reason = "Изменились настройки профиля."
			return nil
		}
	}
	if team.Target != "" {
		e := d.entity(team.Target)
		if e == nil || e.Revision != team.TargetRevision {
			return block("Изменилась версия обязательства.")
		}
		if e.Status == "accepted" && d.effective(e.ID, map[string]bool{}) == "accepted" {
			team.Stage = "planning"
			team.Current = map[string]string{}
			team.Verification = ""
			team.Target = ""
		}
	}
	if team.Stage == "planning" {
		if team.MaxAttempts-team.UsedAttempts < 2 {
			return block("Для поиска и проверки нужны две оставшиеся попытки.")
		}
		target, reason := nextObligation(d, team.Goal, map[string]bool{})
		if target == nil {
			return block(reason)
		}
		if target.Study != team.Study {
			return block("Непринятое основание относится к другому исследованию.")
		}
		if team.RequireLean && target.FormalGoal == nil {
			return block("Для обязательства нужна формальная постановка Lean.")
		}
		team.Target, team.TargetRevision = target.ID, target.Revision
		if err := s.queueTeamAttempt(d, team, "proof", ""); err != nil {
			return block(err.Error())
		}
		if err := s.queueTeamAttempt(d, team, "counterexample", ""); err != nil {
			return block(err.Error())
		}
		team.Stage = "exploring"
		team.Reason = "Поиск доказательства и проверка контрпримеров."
		return nil
	}
	for _, id := range team.Current {
		a := d.attempt(id)
		if a == nil {
			return block("Отсутствует запись попытки.")
		}
		if active(a.Status) {
			return errNoCycleChange
		}
		if a.Status != "candidate" {
			team.Status = "paused"
			team.Reason = "Попытка " + a.ID + " остановилась: " + a.Status + ". Проверьте журнал и продолжите."
			return nil
		}
	}
	switch team.Stage {
	case "exploring":
		counter := d.attempt(team.Current["counterexample"])
		if report, err := s.readResult(*counter); err == nil {
			if !s.safeResultContent(*counter, report) {
				team.RetryRole = "counterexample"
				return block("Отчет получен до исправления разделения текста Coddy. Нужна новая проверка контрпримеров.")
			}
			var found CounterReport
			if decodeAgentReport(report.Candidate, &found) != nil || (found.Outcome != "none_found" && found.Outcome != "counterexample_candidate" && found.Outcome != "inconclusive") || !textOK(found.Evidence, 4000) {
				team.RetryRole = "counterexample"
				return block("Ответ проверяющего контрпримеры не соответствует форме отчета.")
			}
			if found.Outcome == "counterexample_candidate" {
				if team.RequireLean && found.RefutationSource != "" {
					id, err := s.queueRefutation(d, counter.ID)
					if err != nil {
						return block(err.Error())
					}
					team.Refuting = true
					team.Verification = id
					team.Stage = "refuting"
					team.Reason = "Проверка отрицания исходной цели."
					return nil
				}
				team.RetryRole = "counterexample"
				d.Findings = append(d.Findings, Finding{ID: identifier("F"), Target: team.Target, Text: execution.Preview(found.Evidence, 4000),
					Severity: "major", State: "open", Revision: team.TargetRevision})
				return block("Получен кандидат контрпримера. Нужна проверка оператора.")
			}
		} else {
			team.RetryRole = "counterexample"
			return block("Отчет проверки контрпримеров поврежден или недоступен.")
		}
		role := "review"
		if team.RequireLean {
			role = "formalize"
		}
		if err := s.queueTeamAttempt(d, team, role, ""); err != nil {
			return block(err.Error())
		}
		team.Stage = role
		team.Reason = map[string]string{"formalize": "Начата формализация.", "review": "Начата независимая рецензия."}[role]
		return nil
	case "formalize":
		team.Refuting = false
		id, err := s.queueVerification(d, team.Current["formalize"], "")
		if err != nil {
			team.RetryRole = "formalize"
			return block(err.Error())
		}
		team.Verification = id
		team.Stage = "verifying"
		team.Reason = "Проверка Lean."
		return nil
	case "refuting":
		v := d.verification(team.Verification)
		if v == nil {
			return block("Отсутствует проверка отрицания.")
		}
		if v.Status == "queued" || v.Status == "running" {
			return errNoCycleChange
		}
		if !verifiedReportMatches(*v) || !verificationMatches(d, *v) {
			return block("Отрицание не прошло проверку; гипотеза не опровергнута.")
		}
		if err := s.queueTeamAttempt(d, team, "review", ""); err != nil {
			return block(err.Error())
		}
		team.Stage = "refutation_review"
		team.Reason = "Рецензия проверенного отрицания."
		return nil
	case "refutation_review":
		if err := s.bindRefutationReview(d, team.Verification, team.Current["review"]); err != nil {
			return block(err.Error())
		}
		team.Status = "awaiting_review"
		team.Stage = "refutation_acceptance"
		team.Reason = "Отрицание проверено; решение об опровержении принимает оператор."
		return nil
	case "refutation_acceptance":
		return errNoCycleChange
	case "verifying":
		v := d.verification(team.Verification)
		if v == nil {
			return block("Проверка отсутствует.")
		}
		if v.Status == "queued" || v.Status == "running" {
			return errNoCycleChange
		}
		if v.Status != "verified" {
			if team.UsedAttempts >= team.MaxAttempts {
				return block("Исчерпан бюджет исправлений Lean.")
			}
			parent := team.Current["formalize"]
			if err := s.queueTeamAttempt(d, team, "formalize", parent); err != nil {
				return block(err.Error())
			}
			team.Stage = "formalize"
			team.Reason = "Исправление по диагностике Lean."
			return nil
		}
		if err := s.queueTeamAttempt(d, team, "review", ""); err != nil {
			return block(err.Error())
		}
		team.Stage = "review"
		team.Reason = "Независимая рецензия."
		return nil
	case "review":
		source := team.Current["proof"]
		if team.RequireLean {
			source = team.Current["formalize"]
		}
		if result, err := s.readResult(*d.attempt(team.Current["review"])); err == nil {
			if !s.safeResultContent(*d.attempt(team.Current["review"]), result) {
				team.RetryRole = "review"
				return block("Рецензия получена до исправления разделения текста Coddy.")
			}
			var report struct {
				Summary  string `json:"summary"`
				Findings []struct {
					Severity string `json:"severity"`
					Text     string `json:"text"`
				} `json:"findings"`
			}
			if decodeAgentReport(result.Candidate, &report) != nil || !textOK(report.Summary, 4000) || report.Findings == nil || len(report.Findings) > 50 {
				team.RetryRole = "review"
				return block("Рецензия не соответствует форме отчета.")
			}
			for _, f := range report.Findings {
				if !textOK(f.Text, 4000) || !slicesContainsSeverity(f.Severity) {
					team.RetryRole = "review"
					return block("Некорректное замечание в рецензии.")
				}
			}
			{
				for _, f := range report.Findings {
					if textOK(f.Text, 4000) && (f.Severity == "major" || f.Severity == "editorial" || f.Severity == "question") {
						d.Findings = append(d.Findings, Finding{ID: identifier("F"), Target: team.Target, Text: f.Text, Severity: f.Severity, State: "open", Revision: team.TargetRevision, ReviewAttempt: team.Current["review"]})
					}
				}
			}
		} else {
			team.RetryRole = "review"
			return block("Файл рецензии поврежден или недоступен.")
		}
		if err := s.attachProof(d, team.Target, source); err != nil {
			return block(err.Error())
		}
		if team.RequireLean {
			if err := s.bindResult(d, ResultRequest{Target: team.Target, ReviewAttempt: team.Current["review"], CounterAttempt: team.Current["counterexample"]}); err != nil {
				return block(err.Error())
			}
		}
		team.TargetRevision = d.entity(team.Target).Revision
		team.Status = "awaiting_review"
		team.Stage = "acceptance"
		team.Reason = "Материал и рецензия сохранены; решение принимает оператор."
		return nil
	case "acceptance":
		if e := d.entity(team.Target); e != nil && e.Status == "needs_changes" {
			if team.MaxAttempts-team.UsedAttempts < 2 {
				return block("Исчерпан бюджет исправлений после рецензии.")
			}
			parent := team.Current["proof"]
			team.Current = map[string]string{}
			if err := s.queueTeamAttempt(d, team, "proof", parent); err != nil {
				return block(err.Error())
			}
			if err := s.queueTeamAttempt(d, team, "counterexample", ""); err != nil {
				return block(err.Error())
			}
			team.Verification = ""
			team.Stage, team.Status = "exploring", "running"
			team.Reason = "Исправление по замечаниям оператора."
			return nil
		}
		return errNoCycleChange
	}
	return block("Неизвестный этап команды.")
}
func (s *Service) queueTeamAttempt(d *Data, team *ResearchTeam, role, parent string) error {
	if team.UsedAttempts >= team.MaxAttempts {
		return RuleError("Исчерпан бюджет попыток команды.")
	}
	if role == "formalize" && team.RequireLean && team.MaxAttempts-team.UsedAttempts < 2 {
		return RuleError("Для формализации и последующей рецензии нужны две оставшиеся попытки.")
	}
	target := d.entity(team.Target)
	if target == nil {
		return RuleError("Обязательство отсутствует.")
	}
	objectives := map[string]string{
		"proof":          "Построй краткое математическое доказательство точного утверждения. Материалы уже находятся в контексте. Явно перечисли пробелы и используемые леммы. Код Lean подготовит формализатор: не включай код в этот ответ. При неизвестном имени леммы запиши ее утверждение.",
		"counterexample": "Проверь граничные случаи и предпосылки. Верни JSON {outcome: none_found|counterexample_candidate|inconclusive, evidence: текст}. Отсутствие контрпримера не доказывает утверждение.",
		"formalize":      "Формализуй предложенное доказательство. Верни ровно один блок кода lean с import Goal и теоремой с указанным именем кандидата. Цель фиксирована; не используй sorry или дополнительные аксиомы. Проверяющая служба выполнит Lean отдельно.",
		"review":         "Проверь доказательство, предпосылки, контрпримеры и результат Lean. Верни JSON с полями summary и findings, где каждый элемент содержит severity (major|editorial|question) и text. Решение о приемке не принимается агентом.",
	}
	profile := team.Profiles[role]
	if role == "counterexample" {
		objectives[role] += " Если найден контрпример и доступен check_refutation, подготовь исходник отрицания из tool_goals. Добавь в JSON поле refutation_source с полным Lean-файлом. Проверка отрицания не является доказательством исходной цели. При none_found поле не требуется."
	}
	if err := s.validateAssignment(profile, team.Workspace, team.Workers[role]); err != nil {
		return err
	}
	if id := team.Workers[role]; id != "" {
		if err := s.workerScope(*d, id, profile, target.ID); err != nil {
			return err
		}
	}
	labels := map[string]string{"proof": "Доказательство", "counterexample": "Контрпримеры", "formalize": "Формализация", "review": "Рецензия"}
	p := s.profile(profile)
	if limits, ok := team.RoleLimits[role]; ok {
		p.Limits = limits
	}
	reservation, err := s.reserveStudyBudget(d, target.ID, p)
	if err != nil {
		return err
	}
	d.addTask(target.ID, labels[role]+": "+target.Title, role, objectives[role])
	task := &d.Tasks[len(d.Tasks)-1]
	id := identifier("run")
	task.Attempt, task.Agent = id, profile
	reviewOf := team.Current["proof"]
	if role == "review" && team.RequireLean {
		if team.Refuting {
			reviewOf = team.Current["counterexample"]
		} else {
			reviewOf = team.Current["formalize"]
		}
	}
	d.Attempts = append(d.Attempts, Attempt{ID: id, TaskID: task.ID, Target: target.ID, TargetRevision: target.Revision,
		TeamID: team.ID, Role: role, ReviewOf: reviewOf, ParentAttempt: parent, Profile: profile, RemoteWorker: team.Workers[role], Workspace: team.Workspace,
		Status: "queued", ProfileConfiguration: &p, Limits: &p.Limits, ReservedOutputTokens: reservation, ReservedModelRequests: p.Limits.MaxSteps, CreatedAt: time.Now().UTC(), InputSnapshot: d.Revision + 1, RemoteOutcome: "not_started"})
	if role == "review" && team.RequireLean {
		if v := d.verification(team.Verification); v != nil && verifiedReportMatches(*v) {
			d.Attempts[len(d.Attempts)-1].ProofBinding = &ProofBinding{v.ID, v.Report.GoalSHA256, v.Report.SourceSHA256}
		}
	}
	if limits, ok := team.RoleLimits[role]; ok {
		d.Attempts[len(d.Attempts)-1].Limits = &limits
	}
	team.Current[role] = id
	team.UsedAttempts++
	return nil
}
func (s *Service) teamContext(d Data, a Attempt) any {
	if a.TeamID == "" {
		return nil
	}
	team := d.team(a.TeamID)
	if team == nil {
		return nil
	}
	out := map[string]any{"role": a.Role, "accepted": false}
	if a.ProofBinding != nil {
		out["proof_binding"] = a.ProofBinding
	}
	out["operator_note"] = team.OperatorNote
	if e := d.entity(a.Target); e != nil {
		out["operator_review"] = e.ReviewReason
	}
	for role, id := range team.Current {
		if id == a.ID {
			continue
		}
		attempt := d.attempt(id)
		if attempt != nil && attempt.Status == "candidate" {
			if result, err := s.readResult(*attempt); err == nil {
				if s.safeResultContent(*attempt, result) {
					out[role] = execution.Preview(result.Candidate, 32000)
				}
			}
		}
	}
	if e := d.entity(a.Target); e != nil && e.FormalGoal != nil {
		out["formal_goal"] = e.FormalGoal
	}
	if v := d.verification(team.Verification); v != nil {
		out["lean_verification"] = v.Report
	}
	return out
}
func acceptedLemmas(d Data, target string) []Entity {
	out := []Entity{}
	seen := map[string]bool{}
	var visit func(string)
	visit = func(id string) {
		if seen[id] || len(out) >= 20 {
			return
		}
		seen[id] = true
		e := d.entity(id)
		if e == nil {
			return
		}
		if e.Kind == "lemma" && d.effective(e.ID, map[string]bool{}) == "accepted" {
			copy := *e
			copy.Proof = execution.Preview(copy.Proof, 8000)
			out = append(out, copy)
		}
		for _, dep := range e.Dependencies {
			visit(dep)
		}
	}
	visit(target)
	return out
}
func teamReviewReady(d *Data, e *Entity) bool {
	for _, a := range d.Attempts {
		if a.ID == e.ProofAttempt && a.TeamID != "" {
			team := d.team(a.TeamID)
			if team == nil {
				return false
			}
			review := d.attempt(team.Current["review"])
			return review != nil && review.Status == "candidate" && review.Profile != a.Profile && review.ReviewOf == a.ID && review.TargetRevision == a.TargetRevision
		}
	}
	return true
}
func decodeAgentReport(text string, out any) error {
	if err := execution.DecodeAgentReport(text, out); err != nil {
		return RuleError("Нужен один однозначный JSON-отчет без неизвестных полей.")
	}
	return nil
}
func slicesContainsSeverity(s string) bool {
	return s == "major" || s == "editorial" || s == "question"
}
