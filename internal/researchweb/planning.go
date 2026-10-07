package researchweb

import (
	"encoding/json"
	"time"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

type ProposedStrategy struct {
	Target    int    `json:"target"`
	Method    string `json:"method"`
	Priority  int    `json:"priority"`
	Rationale string `json:"rationale"`
}

type PlanningRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Target           string `json:"target"`
	Profile          string `json:"profile"`
	Workspace        string `json:"workspace"`
	Confirm          bool   `json:"confirm"`
}

func (s *Service) RequestPlanning(r PlanningRequest) error {
	if !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) {
		return RuleError("Подтвердите запрос плана и возможные расходы.")
	}
	if err := s.validateExecutor(r.Profile, r.Workspace); err != nil {
		return err
	}
	p := s.profile(r.Profile)
	methods, _ := json.Marshal(researchMethods)
	objective := `Предложи план исследования. Верни только JSON: {"summary":"...","claims":[{"title":"...","statement":"...","assumptions":"...","depends_on":[],"formal_goal":{"source":"...","declaration":"...","candidate":"..."}}],"coverage":{"title":"...","statement":"...","assumptions":"...","formal_goal":{"source":"...","declaration":"...","candidate":"..."}},"strategies":[{"target":0,"method":"induction","priority":50,"rationale":"..."}]}. От одной до шести лемм. Зависимости ссылаются только на предыдущие леммы. coverage явно обосновывает переход от лемм к исходной цели. target стратегии: -1 исходная цель; 0..len(claims)-1 леммы; len(claims) покрытие. Не изменяй исходные предпосылки неявно. Сформулируй проверяемые цели Lean, без доказательств и sorry. План проверит оператор до запуска ветвей. Приемы: ` + string(methods)
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Запрошен план исследования", r.Target, "operator", func(d *Data) error {
		e := d.entity(r.Target)
		if d.Paused || e == nil || e.FormalGoal == nil || e.Status == "accepted" || e.Status == "refuted" {
			return RuleError("Нужна открытая формальная цель и разрешенные запуски.")
		}
		reservation, err := s.reserveStudyBudget(d, e.ID, p)
		if err != nil {
			return err
		}
		d.addTask(e.ID, "План исследования: "+e.Title, "decompose", objective)
		t := &d.Tasks[len(d.Tasks)-1]
		id := identifier("run")
		t.Attempt, t.Agent = id, p.ID
		d.Attempts = append(d.Attempts, Attempt{ID: id, TaskID: t.ID, Target: e.ID, TargetRevision: e.Revision, Profile: p.ID, ProfileConfiguration: &p, Limits: &p.Limits, ReservedOutputTokens: reservation, ReservedModelRequests: p.Limits.MaxSteps, Workspace: r.Workspace, Status: "queued", CreatedAt: time.Now().UTC(), InputSnapshot: d.Revision + 1, RemoteOutcome: "not_started"})
		return nil
	})
}

func validateStrategies(report DecompositionReport) error {
	if len(report.Strategies) > 12 {
		return ErrLimit
	}
	seen := map[string]bool{}
	for _, b := range report.Strategies {
		if _, ok := researchMethod(b.Method); !ok || b.Target < -1 || b.Target > len(report.Claims) || b.Priority < 0 || b.Priority > 100 || !textOK(b.Rationale, 4000) {
			return RuleError("Некорректная цель, прием или приоритет плана.")
		}
		key := hash([]any{b.Target, b.Method})
		if seen[key] {
			return RuleError("Повтор цели и приема в плане.")
		}
		seen[key] = true
	}
	return nil
}

func (s *Service) planBranches(d *Data, p *Decomposition, c TeamRequest) error {
	if len(p.Report.Strategies) == 0 {
		return nil
	}
	if c.MaxAttempts < 4 || c.MaxAttempts > 20 || !c.RequireLean || s.Options.Checker == nil || len(c.Profiles) != 4 || len(c.Workers) != 0 {
		return RuleError("План требует четыре местные роли, Lean и предел 4..20 попыток на ветвь.")
	}
	pins := map[string]string{}
	for _, role := range []string{"proof", "counterexample", "formalize", "review"} {
		pins[role] = hash(s.profile(c.Profiles[role]))
	}
	if c.Profiles["review"] == c.Profiles["proof"] || c.Profiles["review"] == c.Profiles["formalize"] || c.Profiles["review"] == c.Profiles["counterexample"] {
		return RuleError("Рецензенту нужен отдельный профиль.")
	}
	for _, strategy := range p.Report.Strategies {
		target := p.Target
		if strategy.Target >= 0 {
			target = p.Children[strategy.Target]
		}
		e := d.entity(target)
		if e == nil || e.FormalGoal == nil {
			return RuleError("Ветвь требует закрепленную формальную цель.")
		}
		c.Study, c.Target, c.ExpectedRevision, c.RequestID, c.Confirm = e.Study, e.ID, 0, "", true
		_, libraryPins := s.libraryEnvironment(d, e)
		d.Branches = append(d.Branches, ResearchBranch{ID: identifier("branch"), Plan: p.ID, Study: e.Study, Target: e.ID, TargetRevision: e.Revision, GoalSHA256: leancheck.Digest(*e.FormalGoal), Method: strategy.Method, Rationale: strategy.Rationale, Priority: strategy.Priority, Status: "ready", Reason: "План подтвержден оператором; ожидание оснований.", Configuration: c, ProfileHashes: pins, LibraryPins: libraryPins, CreatedAt: time.Now().UTC()})
	}
	return nil
}
