package researchweb

import (
	"fmt"
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
	if a.MinRemainingAttempts < 0 || a.MinRemainingAttempts > 20 || len(a.Roles) > 4 || len(a.Methods) > len(researchMethods) {
		return ErrLimit
	}
	seen := map[string]bool{}
	for _, r := range a.Roles {
		if (r != "proof" && r != "counterexample" && r != "formalize" && r != "review") || seen["role:"+r] {
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
	out := []SkillSelection{}
	selected := []execution.Skill{}
	method := ""
	if t.MethodIndex < len(t.Methods) {
		method = t.Methods[t.MethodIndex]
	}
	e := d.entity(t.Target)
	for _, skill := range p.Skills {
		reason := "Условия применения выполнены."
		if c := skill.Contract; c != nil && c.Applicability != nil {
			a := c.Applicability
			switch {
			case validateSkillApplicability(a) != nil:
				reason = "Некорректные условия применения."
			case len(a.Roles) > 0 && !slices.Contains(a.Roles, role):
				reason = "Навык предназначен для другой роли."
			case len(a.Methods) > 0 && !slices.Contains(a.Methods, method):
				reason = "Метод не выбран оператором."
			case a.RequireFormalGoal && (e == nil || e.FormalGoal == nil):
				reason = "Отсутствует закрепленная формальная цель."
			case a.MinRemainingAttempts > t.MaxAttempts-t.UsedAttempts:
				reason = "Недостаточный остаток попыток."
			case a.RequireVerifiedCandidate:
				v := d.verification(t.Verification)
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
