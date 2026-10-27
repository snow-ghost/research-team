package researchweb

import (
	"encoding/json"
	"slices"
	"time"
)

type ResearchWorkflow struct {
	ID              string            `json:"id"`
	Target          string            `json:"target"`
	ScopeSHA256     string            `json:"scope_sha256"`
	Planner         string            `json:"planner"`
	PlannerSHA256   string            `json:"planner_sha256"`
	ProfileHashes   map[string]string `json:"profile_hashes"`
	Team            TeamRequest       `json:"team"`
	Status          string            `json:"status"`
	BeforePause     string            `json:"before_pause,omitempty"`
	Reason          string            `json:"reason"`
	PlanningAttempt string            `json:"planning_attempt,omitempty"`
	PlanningRound   int               `json:"planning_round"`
	Proposal        string            `json:"proposal,omitempty"`
	QuestionIDs     []string          `json:"question_ids,omitempty"`
	HeldBranches    []string          `json:"held_branches,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
}
type WorkflowRequest struct {
	ExpectedRevision int         `json:"expected_revision"`
	RequestID        string      `json:"request_id"`
	Study            string      `json:"study"`
	Planner          string      `json:"planner"`
	Team             TeamRequest `json:"team"`
	Confirm          bool        `json:"confirm"`
}
type WorkflowCommand struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Kind             string `json:"kind"`
	Note             string `json:"note"`
	Confirm          bool   `json:"confirm"`
}

type WorkflowAnswer struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Answer           string `json:"answer"`
	Confirm          bool   `json:"confirm"`
}

func (s *Service) AnswerWorkflowQuestion(id string, r WorkflowAnswer) error {
	if !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || !textOK(r.Answer, 4000) {
		return RuleError("Подтвердите ответ и укажите его текст.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(struct {
		ID     string
		Answer WorkflowAnswer
	}{id, r}), "Ответ оператора на вопрос планировщика", id, "operator", func(d *Data) error {
		for i := range d.Questions {
			q := &d.Questions[i]
			if q.ID != id {
				continue
			}
			e := d.entity(q.Target)
			if e == nil {
				break
			}
			st := d.study(e.Study)
			if st == nil || st.Workflow == nil || workflowClosed(st.Workflow.Status) || !slices.Contains(st.Workflow.QuestionIDs, id) || q.Answer != "" {
				break
			}
			q.Answer = r.Answer
			return nil
		}
		return RuleError("Нет открытого вопроса действующего полного цикла.")
	})
}

func workflowClosed(status string) bool {
	return slices.Contains([]string{"completed", "stopped", "invalidated"}, status)
}

func (s *Service) CreateWorkflow(r WorkflowRequest) error {
	if !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || s.Options.Checker == nil || !r.Team.RequireLean || r.Team.MaxAttempts < 4 || r.Team.MaxAttempts > 20 || len(r.Team.Profiles) != 4 || len(r.Team.Workers) != 0 || validateTeamStrategy(r.Team) != nil {
		return RuleError("Подтвердите полный цикл с четырьмя местными ролями и Lean.")
	}
	if err := s.validateExecutor(r.Planner, r.Team.Workspace); err != nil {
		return err
	}
	pins := map[string]string{}
	for _, role := range []string{"proof", "counterexample", "formalize", "review"} {
		if err := s.validateExecutor(r.Team.Profiles[role], r.Team.Workspace); err != nil {
			return err
		}
		if err := validateRoleTools(s.profile(r.Team.Profiles[role]), role, false); err != nil {
			return err
		}
		pins[role] = hash(s.profile(r.Team.Profiles[role]))
	}
	for _, role := range []string{"proof", "counterexample", "formalize"} {
		if r.Team.Profiles[role] == r.Team.Profiles["review"] {
			return RuleError("Рецензенту нужен отдельный профиль.")
		}
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Подготовлен полный исследовательский цикл", r.Study, "operator", func(d *Data) error {
		study := d.study(r.Study)
		if study == nil || study.Budget == nil || study.Budget.MaxAttempts < 1 || study.Budget.MaxModelRequests < 1 || studyClosed(d, r.Study) || studySearchSuspended(d, r.Study) {
			return RuleError("Нужна открытая цель и конечный общий бюджет попыток и обращений.")
		}
		e := d.entity(study.Goal)
		if e == nil || e.FormalGoal == nil || study.Workflow != nil && !workflowClosed(study.Workflow.Status) {
			return RuleError("Нужна формальная цель без другого полного цикла.")
		}
		for _, team := range d.Teams {
			if team.Study == study.ID && teamActive(team.Status) {
				return RuleError("Сначала завершите отдельную команду.")
			}
		}
		for _, cycle := range d.Cycles {
			if cycle.Study == study.ID && cycleActive(cycle.Status) {
				return RuleError("Сначала завершите отдельный цикл.")
			}
		}
		for _, branch := range d.Branches {
			if branch.Study == study.ID && !slices.Contains([]string{"completed", "abandoned", "cancelled", "invalidated"}, branch.Status) {
				return RuleError("Сначала завершите отдельные ветви исследования.")
			}
		}
		study.Workflow = &ResearchWorkflow{ID: identifier("workflow"), Target: e.ID, ScopeSHA256: memoryScope(*e), Planner: r.Planner, PlannerSHA256: hash(s.profile(r.Planner)), ProfileHashes: pins, Team: r.Team, Status: "prepared", Reason: "План будет предложен в общем бюджете; разбиение и приемка требуют оператора.", CreatedAt: time.Now().UTC()}
		return nil
	})
}

func (s *Service) ControlWorkflow(studyID string, r WorkflowCommand) error {
	if !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || !textOK(r.Note, 4000) || !slices.Contains([]string{"pause", "resume", "reprocess", "approve_plan", "stop"}, r.Kind) {
		return RuleError("Подтвердите действие полного цикла и его основание.")
	}
	v, err := s.Store.Read()
	if err != nil {
		return err
	}
	study := v.study(studyID)
	if study == nil || study.Workflow == nil {
		return RuleError("Полный цикл не найден.")
	}
	w := study.Workflow
	if r.Kind == "approve_plan" {
		if w.Status != "awaiting_plan" {
			return RuleError("Нет плана, ожидающего подтверждения.")
		}
		return s.ApplyDecomposition(w.Proposal, ProposalRequest{ExpectedRevision: r.ExpectedRevision, RequestID: r.RequestID, Confirm: true, Team: &w.Team})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err = s.Store.Change(r.ExpectedRevision, r.RequestID, hash(struct {
		Study   string
		Request WorkflowCommand
	}{studyID, r}), "Управление полным исследовательским циклом", studyID, "operator", func(d *Data) error {
		st := d.study(studyID)
		if st == nil || st.Workflow == nil || workflowClosed(st.Workflow.Status) {
			return RuleError("Цикл закрыт.")
		}
		w := st.Workflow
		if r.Kind == "reprocess" {
			if w.Status != "needs_operator" || !s.workflowReportsReprocessable(d, w, studyID) {
				return RuleError("Повторная обработка требует примененного плана и уже полученных текущих ответов; неизвестные передачи не повторяются.")
			}
			w.Status = "running"
		} else if r.Kind == "pause" {
			if w.Status == "paused" {
				return RuleError("Цикл уже на паузе.")
			}
			w.BeforePause, w.Status = w.Status, "paused"
		} else if r.Kind == "resume" {
			if w.Status == "needs_input" {
				for _, id := range w.QuestionIDs {
					for _, q := range d.Questions {
						if q.ID == id && q.Answer == "" {
							return RuleError("Сначала ответьте на вопросы планировщика.")
						}
					}
				}
				if w.PlanningRound >= 2 {
					return RuleError("Два предложения плана исчерпаны; нужен отдельный новый цикл.")
				}
				w.Status, w.PlanningAttempt, w.Proposal = "prepared", "", ""
			} else if w.Status == "paused" {
				w.Status = w.BeforePause
			} else if w.Status == "needs_operator" && w.Proposal != "" && s.observeBudget(d, studyID).UnknownUsage == 0 {
				applied := false
				for _, p := range d.Proposals {
					applied = applied || p.ID == w.Proposal && p.Status == "applied"
				}
				if !applied {
					return RuleError("Продолжение требует уже утвержденного плана; ошибку планирования повторять нельзя.")
				}
				w.Status = "running"
			} else {
				return RuleError("Неизвестный исход или ошибку нельзя повторить через продолжение; нужно отдельное назначение.")
			}
		} else {
			w.Status = "stopped"
		}
		w.Reason = r.Note
		if a := d.attempt(w.PlanningAttempt); a != nil && queuedBeforeExecution(*a) {
			a.QueueHeld = w.Status == "paused"
			a.QueueReason = r.Note
			if w.Status == "stopped" {
				a.Status = "cancelled"
				d.task(a.TaskID).State = "cancelled"
			}
		} else if a != nil && r.Kind == "stop" && active(a.Status) {
			a.Status = "cancelling"
			if task := d.task(a.TaskID); task != nil {
				task.State = "cancelling"
			}
		}
		for i := range d.Branches {
			b := &d.Branches[i]
			if b.Plan == "" || b.Plan != w.Proposal || slices.Contains([]string{"completed", "abandoned", "invalidated", "cancelled"}, b.Status) {
				continue
			}
			if r.Kind == "pause" && !b.Held {
				w.HeldBranches = append(w.HeldBranches, b.ID)
				if err := s.decideBranchData(d, b.ID, BranchDecisionRequest{RequestID: r.RequestID + "-" + b.ID, Kind: "split", Note: r.Note, NoteKind: "operational", Confirm: true}, "operator", ""); err != nil {
					return err
				}
			} else if r.Kind == "resume" && slices.Contains(w.HeldBranches, b.ID) {
				if err := s.decideBranchData(d, b.ID, BranchDecisionRequest{RequestID: r.RequestID + "-" + b.ID, Kind: "continue", Note: r.Note, NoteKind: "operational", Confirm: true}, "operator", ""); err != nil {
					return err
				}
			} else if r.Kind == "stop" {
				if err := s.decideBranchData(d, b.ID, BranchDecisionRequest{RequestID: r.RequestID + "-" + b.ID, Kind: "stop", Note: r.Note, NoteKind: "operational", Confirm: true}, "operator", ""); err != nil {
					return err
				}
			}
		}
		if r.Kind == "resume" {
			w.HeldBranches = nil
		}
		return nil
	})
	if err == nil {
		s.cancelRequested()
	}
	return err
}

func (s *Service) workflowState(study, status, reason string, questions []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.Store.Change(0, "", "", "Изменен этап полного цикла", study, "coordinator", func(d *Data) error {
		st := d.study(study)
		if st == nil || st.Workflow == nil || st.Workflow.Status == "paused" || workflowClosed(st.Workflow.Status) {
			return ErrConflict
		}
		w := st.Workflow
		if w.Status == status && w.Reason == reason {
			return ErrConflict
		}
		w.Status, w.Reason = status, reason
		if status == "invalidated" {
			if a := d.attempt(w.PlanningAttempt); a != nil && active(a.Status) {
				if queuedBeforeExecution(*a) {
					a.Status = "cancelled"
				} else {
					a.Status = "cancelling"
				}
				if task := d.task(a.TaskID); task != nil {
					task.State = a.Status
				}
			}
			for i := range d.Branches {
				b := &d.Branches[i]
				if b.Plan == "" || b.Plan != w.Proposal || slices.Contains([]string{"completed", "abandoned", "cancelled", "invalidated"}, b.Status) {
					continue
				}
				b.Held, b.Status, b.Reason = true, "invalidated", reason
				if team := d.team(b.Team); team != nil && teamActive(team.Status) {
					if err := s.controlTeamData(d, team.ID, TeamCommand{RequestID: w.ID + "-invalidate", Kind: "stop", Note: reason, NoteKind: "operational", Confirm: true}); err != nil {
						return err
					}
				}
			}
		}
		for _, text := range questions {
			id := identifier("Q")
			d.Questions = append(d.Questions, Question{ID: id, Target: w.Target, Text: text, Kind: "clarification", Snapshot: d.Revision + 1})
			w.QuestionIDs = append(w.QuestionIDs, id)
		}
		return nil
	})
	if err == nil {
		s.cancelRequested()
	}
}

func (s *Service) advanceWorkflows() {
	v, err := s.Store.Read()
	if err != nil || v.Maintenance != nil {
		return
	}
	for _, study := range v.Studies {
		w := study.Workflow
		if w == nil || workflowClosed(w.Status) || w.Status == "paused" {
			continue
		}
		e := v.entity(w.Target)
		if e == nil || memoryScope(*e) != w.ScopeSHA256 {
			s.workflowState(study.ID, "invalidated", "Постановка или условия изменились.", nil)
			return
		}
		if studyClosed(&v.Data, study.ID) {
			s.workflowState(study.ID, "completed", "Основная цель отдельно принята или опровергнута оператором.", nil)
			return
		}
		if w.Status == "prepared" && !v.Paused {
			if hash(s.profile(w.Planner)) != w.PlannerSHA256 {
				s.workflowState(study.ID, "needs_operator", "Профиль планировщика изменился.", nil)
				return
			}
			err := s.RequestPlanning(PlanningRequest{ExpectedRevision: v.Revision, RequestID: w.ID + "-plan-" + hash(w.PlanningRound)[:12], Target: w.Target, Profile: w.Planner, Workspace: w.Team.Workspace, Confirm: true})
			if err != nil && err != ErrConflict {
				s.workflowState(study.ID, "needs_operator", err.Error(), nil)
			}
			return
		}
		if w.Status == "planning" {
			a := v.attempt(w.PlanningAttempt)
			if a == nil {
				s.workflowState(study.ID, "needs_operator", "Потеряна связь с попыткой плана.", nil)
				return
			}
			if active(a.Status) {
				continue
			}
			if a.Status != "candidate" || a.RemoteOutcome == "unknown" {
				s.workflowState(study.ID, "needs_operator", "План не завершен; автоматического повтора нет.", nil)
				return
			}
			for _, p := range v.Proposals {
				if p.Attempt == a.ID {
					s.workflowState(study.ID, "awaiting_plan", "Проверьте леммы, формальные цели, покрытие и стратегии до подтверждения.", nil)
					return
				}
			}
			result, err := s.readResult(*a)
			if err != nil {
				s.workflowState(study.ID, "needs_operator", err.Error(), nil)
				return
			}
			var clarification struct {
				Outcome   string   `json:"outcome"`
				Questions []string `json:"questions"`
			}
			_ = json.Unmarshal([]byte(result.Candidate), &clarification)
			if clarification.Outcome == "needs_input" && len(clarification.Questions) > 0 && len(clarification.Questions) <= 6 {
				valid := true
				for _, q := range clarification.Questions {
					valid = valid && textOK(q, 4000)
				}
				if valid {
					s.workflowState(study.ID, "needs_input", "Нужны уточнения пользователя; новые попытки не назначаются.", clarification.Questions)
					return
				}
			}
			err = s.ImportDecomposition(ProposalRequest{ExpectedRevision: v.Revision, RequestID: w.ID + "-import-" + a.ID, Attempt: a.ID})
			if err != nil && err != ErrConflict {
				s.workflowState(study.ID, "needs_operator", err.Error(), nil)
			}
			return
		}
		if w.Status == "running" || w.Status == "awaiting_acceptance" {
			next, reason := "running", "Команды исследуют части и покрытие в общем бюджете."
			for _, b := range v.Branches {
				if b.Plan != w.Proposal {
					continue
				}
				if b.Status == "blocked" {
					next, reason = "needs_operator", "Ветвь остановлена; проверьте бюджет, вопросы и диагностику."
				}
				if b.Team != "" {
					if team := v.team(b.Team); team != nil && team.Status == "awaiting_review" {
						next, reason = "awaiting_acceptance", "Проверенный результат ожидает отдельной приемки оператора."
					}
				}
			}
			if next != w.Status || reason != w.Reason {
				s.workflowState(study.ID, next, reason, nil)
				return
			}
		}
	}
}
