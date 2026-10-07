package researchweb

import (
	"sort"
	"time"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

type ResearchMethod struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Procedure string `json:"procedure"`
}

var researchMethods = []ResearchMethod{
	{"induction", "Индукция", "Определи параметр индукции, базу, гипотезу и полный шаг. Проверь покрытие области; каждое основание подтверждается отдельно."},
	{"generalization", "Обобщение", "Проверь более общее утверждение, в котором исходная цель следует подстановкой. Запиши дополнительные условия и докажи перенос результата."},
	{"representation", "Замена представления", "Предложи другое представление той же цели. До переноса результата докажи эквивалентность представлений; не меняй предпосылки неявно."},
	{"equivalence", "Необходимые и достаточные условия", "Раздели направления импликации. Для цепочки эквивалентностей проверь оба направления каждого звена и ограничения области."},
	{"extremal", "Принцип крайнего", "Определи допустимое множество и существование крайнего элемента. Обоснуй, почему его свойств достаточно для исходного утверждения."},
	{"minimal_counterexample", "Минимальный контрпример", "Укажи упорядочение, существование минимального контрпримера и уменьшающее преобразование. Проверь сохранение предпосылок."},
	{"double_counting", "Двойной подсчет", "Определи одно конечное множество и два способа подсчета. Проверь кратности, исключения и переход к требуемому равенству."},
	{"symmetry", "Симметрия и усреднение", "Определи действие преобразований и сохраняемые свойства. Обоснуй допустимость усреднения и переход к исходной цели."},
	{"decomposition", "Декомпозиция", "Выдели самостоятельные леммы и условную теорему покрытия. Отдельно проверь, что принятые части действительно дают исходный результат."},
	{"counterexample", "Контрпример", "Проверь крайние случаи и каждую предпосылку. Кандидат контрпримера требует проверки; отсутствие контрпримера не доказывает цель."},
	{"specialization", "Специализация", "Ограничи параметры и найди структуру частного случая. Зафиксируй границы переноса: частный случай не доказывает общее утверждение."},
}

func researchMethod(id string) (ResearchMethod, bool) {
	for _, m := range researchMethods {
		if m.ID == id {
			return m, true
		}
	}
	return ResearchMethod{}, false
}

type ResearchBranch struct {
	Plan           string            `json:"plan,omitempty"`
	ID             string            `json:"id"`
	Study          string            `json:"study"`
	Target         string            `json:"target"`
	TargetRevision int               `json:"target_revision"`
	GoalSHA256     string            `json:"goal_sha256"`
	Method         string            `json:"method"`
	Rationale      string            `json:"rationale"`
	Priority       int               `json:"priority"`
	Status         string            `json:"status"`
	Reason         string            `json:"reason"`
	Team           string            `json:"team,omitempty"`
	Configuration  TeamRequest       `json:"configuration"`
	ProfileHashes  map[string]string `json:"profile_hashes"`
	LibraryPins    map[string]string `json:"library_pins,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
}
type BranchRequest struct {
	ExpectedRevision int         `json:"expected_revision"`
	RequestID        string      `json:"request_id"`
	Method           string      `json:"method"`
	Rationale        string      `json:"rationale"`
	Priority         int         `json:"priority"`
	Confirm          bool        `json:"confirm"`
	Team             TeamRequest `json:"team"`
}
type BranchCommand struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Kind             string `json:"kind"`
	Priority         int    `json:"priority"`
	Note             string `json:"note"`
	Confirm          bool   `json:"confirm"`
}

func (s *Service) CreateBranch(r BranchRequest) error {
	if !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || r.Priority < 0 || r.Priority > 100 || !textOK(r.Rationale, 4000) {
		return RuleError("Подтвердите ветвь, приоритет и основание выбора.")
	}
	if _, ok := researchMethod(r.Method); !ok {
		return RuleError("Неизвестный исследовательский прием.")
	}
	c := r.Team
	if c.MaxAttempts < 4 || c.MaxAttempts > 20 || !c.RequireLean || s.Options.Checker == nil || len(c.Profiles) != 4 {
		return RuleError("Ветвь требует четырех ролей, проверки Lean и предела 4..20 попыток.")
	}
	pins := map[string]string{}
	for _, role := range []string{"proof", "counterexample", "formalize", "review"} {
		if err := s.validateAssignment(c.Profiles[role], c.Workspace, c.Workers[role]); err != nil {
			return err
		}
		pins[role] = hash(s.profile(c.Profiles[role]))
	}
	if c.Profiles["review"] == c.Profiles["proof"] || c.Profiles["review"] == c.Profiles["formalize"] || c.Profiles["review"] == c.Profiles["counterexample"] {
		return RuleError("Рецензент должен иметь отдельный профиль.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Разрешена исследовательская ветвь", c.Study, "operator", func(d *Data) error {
		study := d.study(c.Study)
		if study == nil {
			return RuleError("Исследование не найдено.")
		}
		target := c.Target
		if target == "" {
			target = study.Goal
		}
		e := d.entity(target)
		if e == nil || e.Study != c.Study || e.FormalGoal == nil || e.Status == "accepted" || e.Status == "refuted" {
			return RuleError("Нужна открытая закрепленная цель этого исследования.")
		}
		for role, worker := range c.Workers {
			if worker != "" {
				if err := s.workerScope(*d, worker, c.Profiles[role], e.ID); err != nil {
					return err
				}
			}
		}
		c.Target = target
		c.ExpectedRevision = 0
		c.RequestID = ""
		c.Confirm = true
		_, libraryPins := s.libraryEnvironment(d, e)
		d.Branches = append(d.Branches, ResearchBranch{ID: identifier("branch"), Study: c.Study, Target: target, TargetRevision: e.Revision, GoalSHA256: leancheck.Digest(*e.FormalGoal), Method: r.Method, Rationale: r.Rationale, Priority: r.Priority, Status: "ready", Reason: "Ветвь разрешена оператором.", Configuration: c, ProfileHashes: pins, LibraryPins: libraryPins, CreatedAt: time.Now().UTC()})
		return nil
	})
}

func (s *Service) ControlBranch(id string, r BranchCommand) error {
	if r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || !textOK(r.Note, 4000) {
		return RuleError("Нужны снимок и основание решения.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(struct {
		ID string
		BranchCommand
	}{id, r}), "Изменена исследовательская ветвь", id, "operator", func(d *Data) error {
		for i := range d.Branches {
			b := &d.Branches[i]
			if b.ID != id {
				continue
			}
			switch r.Kind {
			case "abandon":
				b.Status = "abandoned"
				if b.Team != "" {
					cancelTeam(d, b.Team)
					if team := d.team(b.Team); team != nil {
						team.Status = "cancelled"
						team.Reason = r.Note
					}
				}
			case "prioritize":
				if r.Priority < 0 || r.Priority > 100 || b.Status != "ready" {
					return RuleError("Менять приоритет можно только ожидающей ветви.")
				}
				b.Priority = r.Priority
			case "resume":
				if !r.Confirm || b.Team != "" || b.Status != "blocked" {
					return RuleError("Продолжение назначенной команды выполняется отдельно.")
				}
				b.Status = "ready"
			default:
				return RuleError("Неизвестная команда ветви.")
			}
			b.Reason = r.Note
			return nil
		}
		return RuleError("Ветвь не найдена.")
	})
}

func (s *Service) advanceBranches() {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := s.Store.Read()
	if err != nil {
		return
	}
	for _, branch := range v.Branches {
		if branch.Status == "completed" || branch.Status == "invalidated" || branch.Status == "abandoned" {
			continue
		}
		e := v.entity(branch.Target)
		team := v.team(branch.Team)
		status, reason := branch.Status, branch.Reason
		if e == nil || e.FormalGoal == nil || leancheck.Digest(*e.FormalGoal) != branch.GoalSHA256 {
			status, reason = "invalidated", "Формальная цель изменена."
		} else if !branchLibrariesMatch(&v.Data, branch) {
			status, reason = "invalidated", "Закрепленная принятая зависимость изменена."
		} else if e.Status == "accepted" || e.Status == "refuted" {
			status, reason = "completed", "Оператор принял результат выбранной цели."
		} else if team != nil {
			status, reason = team.Status, team.Reason
			if status == "interrupted" {
				status = "blocked"
			}
		} else if e.Revision != branch.TargetRevision {
			status, reason = "invalidated", "Версия цели изменена до запуска."
		}
		if status != branch.Status || reason != branch.Reason {
			_ = s.Store.Change(0, "", "", "Состояние исследовательской ветви", branch.ID, "coordinator", func(d *Data) error {
				if status == "invalidated" && branch.Team != "" {
					cancelTeam(d, branch.Team)
					if t := d.team(branch.Team); t != nil {
						t.Status, t.Reason = "cancelled", reason
					}
				}
				for i := range d.Branches {
					if d.Branches[i].ID == branch.ID {
						d.Branches[i].Status, d.Branches[i].Reason = status, reason
					}
				}
				return nil
			})
		}
	}
	v, err = s.Store.Read()
	if err != nil || v.Paused {
		return
	}
	ready := []ResearchBranch{}
	for _, b := range v.Branches {
		if b.Status == "ready" && b.Team == "" {
			ready = append(ready, b)
		}
	}
	sort.SliceStable(ready, func(i, j int) bool { return ready[i].Priority > ready[j].Priority })
	for _, b := range ready {
		busy := false
		for _, t := range v.Teams {
			if t.Study == b.Study && teamActive(t.Status) {
				busy = true
			}
		}
		for _, c := range v.Cycles {
			if c.Study == b.Study && cycleActive(c.Status) {
				busy = true
			}
		}
		if busy {
			continue
		}
		goal := v.entity(b.Target)
		if goal == nil {
			continue
		}
		dependenciesReady := true
		for _, dep := range goal.Dependencies {
			if v.effective(dep, map[string]bool{}) != "accepted" {
				dependenciesReady = false
			}
		}
		if !dependenciesReady {
			continue
		}
		_ = s.Store.Change(0, "", "", "Выбрана исследовательская ветвь по приоритету", b.ID, "coordinator", func(d *Data) error {
			var current *ResearchBranch
			for i := range d.Branches {
				if d.Branches[i].ID == b.ID {
					current = &d.Branches[i]
				}
			}
			if current == nil || current.Status != "ready" || current.Team != "" {
				return errNoCycleChange
			}
			goal := d.entity(current.Target)
			if goal == nil || goal.FormalGoal == nil || goal.Revision != current.TargetRevision || leancheck.Digest(*goal.FormalGoal) != current.GoalSHA256 || !branchLibrariesMatch(d, *current) {
				return errNoCycleChange
			}
			for _, dep := range goal.Dependencies {
				if d.effective(dep, map[string]bool{}) != "accepted" {
					return errNoCycleChange
				}
			}
			for role, pin := range current.ProfileHashes {
				if hash(s.profile(current.Configuration.Profiles[role])) != pin {
					current.Status = "blocked"
					current.Reason = "Профиль изменен; требуется новое разрешение."
					return nil
				}
			}
			for _, team := range d.Teams {
				if team.Study == b.Study && teamActive(team.Status) {
					return errNoCycleChange
				}
			}
			method, _ := researchMethod(current.Method)
			c := current.Configuration
			team := ResearchTeam{ID: identifier("team"), Study: current.Study, Goal: current.Target, Profiles: c.Profiles, Workers: c.Workers, ProfileHashes: current.ProfileHashes, Workspace: c.Workspace, MaxAttempts: c.MaxAttempts, RequireLean: true, Status: "running", Stage: "planning", Current: map[string]string{}, OperatorNote: method.Procedure + " Основание выбора: " + current.Rationale, CreatedAt: time.Now().UTC(), Reason: "Выбрана ветвь: " + method.Label}
			d.Teams = append(d.Teams, team)
			current.Team = team.ID
			current.Status = "running"
			current.Reason = team.Reason
			return nil
		})
		break
	}
}

func branchLibrariesMatch(d *Data, b ResearchBranch) bool {
	for id, pin := range b.LibraryPins {
		found := false
		for _, l := range d.Library {
			if l.ID == id && l.Status == "ready" && l.Report != nil && l.Report.ArtifactSHA256 == pin && libraryMatches(d, l) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
