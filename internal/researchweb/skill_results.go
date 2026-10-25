package researchweb

import (
	"fmt"
	"slices"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
	"github.com/snow-ghost/research-team/internal/proofreview"
)

func checkSkillResult(p execution.Profile, candidate string) []execution.SkillResultCheck {
	out := []execution.SkillResultCheck{}
	for _, skill := range p.Skills {
		if skill.Contract == nil {
			continue
		}
		for _, check := range skill.Contract.ResultChecks {
			passed := false
			switch check {
			case "lean_source":
				source, err := leancheck.ExtractSource(candidate)
				passed = err == nil && len(source) > 0 && len(source) <= 65536
			case "review_report":
				var review ReviewReport
				passed = decodeAgentReport(candidate, &review) == nil && proofreview.Validate(review) == nil && review.Rubric == proofreview.Rubric
			case "counterexample_report":
				var report CounterReport
				passed = decodeAgentReport(candidate, &report) == nil && slices.Contains([]string{"none_found", "counterexample_candidate", "inconclusive"}, report.Outcome) && textOK(report.Evidence, 4000)
			}
			out = append(out, execution.SkillResultCheck{Skill: fmt.Sprint(skill.ID, "@", skill.Version), Check: check, Passed: passed})
		}
	}
	return out
}

func enforceSkillResults(p execution.Profile, r *execution.Result) {
	if r.Status != "candidate" {
		return
	}
	r.SkillChecks = checkSkillResult(p, r.Candidate)
	for _, check := range r.SkillChecks {
		if !check.Passed {
			r.PrecheckStatus = r.Status
			r.Status = "failed"
			r.FailureCode = "skill_result_contract"
			r.Events = append(r.Events, execution.Event{Type: "skill_result_rejected", Status: check.Skill + ":" + check.Check})
			return
		}
	}
}

type SkillTrialRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Target           string `json:"target"`
	Profile          string `json:"profile"`
	Role             string `json:"role"`
	Method           string `json:"method,omitempty"`
	Verification     string `json:"verification,omitempty"`
	Candidate        string `json:"candidate"`
}
type SkillTrial struct {
	Applicable               bool                         `json:"applicable"`
	Selections               []SkillSelection             `json:"selections"`
	Checks                   []execution.SkillResultCheck `json:"checks"`
	MathematicalVerification bool                         `json:"mathematical_verification"`
}

func (s *Service) TrialSkill(id string, r SkillTrialRequest) (SkillTrial, error) {
	out := SkillTrial{}
	if len(r.Candidate) > 65536 || !requestPattern.MatchString(r.RequestID) || !slices.Contains([]string{"proof", "formalize", "counterexample", "review", "decompose", "answer"}, r.Role) {
		return out, ErrLimit
	}
	if r.Method != "" {
		if _, ok := researchMethod(r.Method); !ok {
			return out, RuleError("Неизвестный прием исследования.")
		}
	}
	v, err := s.Store.Read()
	if err != nil {
		return out, err
	}
	if v.Revision != r.ExpectedRevision {
		return out, ErrConflict
	}
	p, ok := s.lookupProfile(r.Profile)
	if !ok || v.entity(r.Target) == nil {
		return out, RuleError("Нужны разрешенный профиль и цель.")
	}
	found := false
	for _, revision := range v.SkillRevisions {
		if revision.ID == id {
			if revision.SHA256 != hash(revision.Configuration) || revision.Configuration.Contract == nil || validateSkillContract(*revision.Configuration.Contract) != nil {
				return out, RuleError("Нарушена целостность навыка.")
			}
			p.Skills = []execution.Skill{revision.Configuration}
			found = true
			break
		}
	}
	if !found {
		return out, RuleError("Версия навыка не найдена.")
	}
	out.Selections = selectAssignmentSkills(&v.Data, skillFacts{Target: r.Target, Role: r.Role, Method: r.Method, Verification: r.Verification, Remaining: remainingStudyAttempts(&v.Data, r.Target)}, &p)
	out.Applicable = len(p.Skills) > 0
	out.Checks = checkSkillResult(p, r.Candidate)
	return out, nil
}
