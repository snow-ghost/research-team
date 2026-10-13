package researchweb

import (
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
)

type SkillSelection struct {
	ID       string `json:"id"`
	SHA256   string `json:"sha256"`
	Selected bool   `json:"selected"`
	Reason   string `json:"reason"`
}

type TeamDecision struct {
	Attempt        string           `json:"attempt"`
	Role           string           `json:"role"`
	Stage          string           `json:"stage"`
	Target         string           `json:"target"`
	TargetRevision int              `json:"target_revision"`
	GoalSHA256     string           `json:"goal_sha256"`
	Method         string           `json:"method,omitempty"`
	Reason         string           `json:"reason"`
	Skills         []SkillSelection `json:"skills,omitempty"`
	CreatedAt      time.Time        `json:"created_at"`
}

func assignmentReason(t *ResearchTeam, role string) string {
	if t.Strategy != "demand" {
		return t.Reason
	}
	switch role {
	case "proof":
		return "Сначала один исследователь; отдельная формализация зависит от его результата."
	case "formalize":
		if t.Diagnostic != "" {
			return "Формализация назначена по диагностике: " + t.Diagnostic
		}
		return "Нужен завершенный исходник точного обязательства."
	case "counterexample":
		return "Проверка границ и предпосылок не заменяется положительным доказательством."
	case "review":
		return "После проверки Lean нужен отдельный рецензент точного кандидата."
	}
	return t.Reason
}

func validateSkillApplicability(a *execution.SkillApplicability) error {
	if a == nil {
		return nil
	}
	if a.MinRemainingAttempts < 0 || a.MinRemainingAttempts > 20 || a.Priority < 0 || a.Priority > 100 || len(a.Roles) > 6 || len(a.Methods) > len(researchMethods) {
		return ErrLimit
	}
	if a.ExclusiveGroup != "" && !regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`).MatchString(a.ExclusiveGroup) {
		return RuleError("Некорректное имя группы выбора навыка.")
	}
	seen := map[string]bool{}
	for _, r := range a.Roles {
		if (r != "proof" && r != "counterexample" && r != "formalize" && r != "review" && r != "decompose" && r != "answer") || seen["role:"+r] {
			return RuleError("Неизвестная или повторная роль навыка.")
		}
		seen["role:"+r] = true
	}
	for _, m := range a.Methods {
		if _, ok := researchMethod(m); !ok || seen["method:"+m] {
			return RuleError("Неизвестный или повторный метод навыка.")
		}
		seen["method:"+m] = true
	}
	return nil
}

func selectTeamSkills(d *Data, t *ResearchTeam, role string, p *execution.Profile) []SkillSelection {
	method := ""
	if t.MethodIndex < len(t.Methods) {
		method = t.Methods[t.MethodIndex]
	}
	return selectAssignmentSkills(d, skillFacts{Target: t.Target, Role: role, Method: method, Verification: t.Verification, Remaining: min(remainingStudyAttempts(d, t.Target), t.MaxAttempts-t.UsedAttempts)}, p)
}

type skillFacts struct {
	Target, Role, Method, Verification string
	Remaining                          int
}

func remainingStudyAttempts(d *Data, target string) int {
	n := 100 - len(d.Attempts)
	if e := d.entity(target); e != nil {
		if study := d.study(e.Study); study != nil && study.Budget != nil {
			used := 0
			for _, a := range d.Attempts {
				if x := d.entity(a.Target); x != nil && x.Study == e.Study && a.ImportedPackageSHA256 == "" {
					used++
				}
			}
			n = min(n, study.Budget.MaxAttempts-used)
		}
	}
	return n
}

func skillPriority(s execution.Skill) int {
	if s.Contract != nil && s.Contract.Applicability != nil {
		return s.Contract.Applicability.Priority
	}
	return 0
}

func selectAssignmentSkills(d *Data, f skillFacts, p *execution.Profile) []SkillSelection {
	out := []SkillSelection{}
	selected := []execution.Skill{}
	ordered := slices.Clone(p.Skills)
	slices.SortFunc(ordered, func(a, b execution.Skill) int {
		if x := skillPriority(b) - skillPriority(a); x != 0 {
			return x
		}
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		if a.Version < b.Version {
			return -1
		}
		if a.Version > b.Version {
			return 1
		}
		return 0
	})
	groups := map[string]string{}
	e := d.entity(f.Target)
	for _, skill := range ordered {
		reason := "Условия применения выполнены."
		if skill.Contract != nil {
			for _, required := range skill.Contract.RequiredTools {
				if !slices.Contains(leanToolNames(*p), required) {
					reason = "Требуемый инструмент не разрешен профилю: " + required
					break
				}
			}
		}
		if c := skill.Contract; c != nil && c.Applicability != nil {
			a := c.Applicability
			switch {
			case reason != "Условия применения выполнены.":
			case validateSkillApplicability(a) != nil:
				reason = "Некорректные условия применения."
			case len(a.Roles) > 0 && !slices.Contains(a.Roles, f.Role):
				reason = "Навык предназначен для другой роли."
			case len(a.Methods) > 0 && !slices.Contains(a.Methods, f.Method):
				reason = "Метод не выбран оператором."
			case a.RequireFormalGoal && (e == nil || e.FormalGoal == nil):
				reason = "Отсутствует закрепленная формальная цель."
			case a.MinRemainingAttempts > f.Remaining:
				reason = "Недостаточный остаток попыток."
			case a.RequireVerifiedCandidate:
				v := d.verification(f.Verification)
				if v == nil || !verifiedReportMatches(*v) || !verificationMatches(d, *v) || v.Report.AuditSHA256 != leancheck.AuditDigest() {
					reason = "Нет действующей проверки кандидата Lean."
				}
			}
			if reason == "Условия применения выполнены." && a.RequireAcceptedDependencies && e != nil {
				for _, dep := range e.Dependencies {
					if d.effective(dep, map[string]bool{}) != "accepted" {
						reason = "Основания еще не приняты."
						break
					}
				}
			}
			if reason == "Условия применения выполнены." && a.ExclusiveGroup != "" {
				if chosen, ok := groups[a.ExclusiveGroup]; ok {
					reason = "В группе уже выбран навык по приоритету и порядку имен: " + chosen
				} else {
					groups[a.ExclusiveGroup] = skill.ID + "@" + skill.Version
				}
			}
		}
		ok := reason == "Условия применения выполнены."
		out = append(out, SkillSelection{ID: fmt.Sprint(skill.ID, "@", skill.Version), SHA256: hash(skill), Selected: ok, Reason: reason})
		if ok {
			selected = append(selected, skill)
		}
	}
	p.Skills = selected
	return out
}

func selectStandaloneSkills(d *Data, target, role, method, verification string, p *execution.Profile) ([]SkillSelection, error) {
	if method != "" {
		if _, ok := researchMethod(method); !ok {
			return nil, RuleError("Неизвестный метод исследования.")
		}
	}
	selections := selectAssignmentSkills(d, skillFacts{Target: target, Role: role, Method: method, Verification: verification, Remaining: remainingStudyAttempts(d, target)}, p)
	if len(selections) > 0 && len(p.Skills) == 0 {
		return selections, RuleError("Условия всех навыков выбранного профиля не выполнены; запуск не назначен.")
	}
	return selections, nil
}
