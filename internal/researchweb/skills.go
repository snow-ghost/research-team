package researchweb

import (
	"fmt"
	"regexp"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
)

type SkillRevision struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Version       int             `json:"version"`
	Label         string          `json:"label"`
	SHA256        string          `json:"sha256"`
	Configuration execution.Skill `json:"configuration"`
	CreatedAt     time.Time       `json:"created_at"`
}

type SkillRequest struct {
	ExpectedRevision int                     `json:"expected_revision"`
	RequestID        string                  `json:"request_id"`
	Name             string                  `json:"name"`
	Label            string                  `json:"label"`
	Instructions     string                  `json:"instructions"`
	Contract         execution.SkillContract `json:"contract"`
	Confirm          bool                    `json:"confirm"`
}

func validateSkillContract(c execution.SkillContract) error {
	if len(c.Preconditions) == 0 || len(c.Inputs) == 0 || len(c.StopCriteria) == 0 || !textOK(c.Output, 4000) {
		return RuleError("Нужны условия применения, входы, результат и критерии остановки.")
	}
	for _, values := range [][]string{c.Preconditions, c.Inputs, c.StopCriteria, c.Examples} {
		if len(values) > 16 {
			return ErrLimit
		}
		for _, value := range values {
			if !textOK(value, 4000) {
				return ErrLimit
			}
		}
	}
	seen := map[string]bool{}
	for _, tool := range c.RequiredTools {
		if (tool != "read_file" && tool != "check_lean" && tool != "check_refutation") || seen[tool] {
			return RuleError("Неизвестный или повторный инструмент навыка.")
		}
		seen[tool] = true
	}
	return nil
}

func (s *Service) CreateSkill(r SkillRequest) error {
	if !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || !regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`).MatchString(r.Name) || !textOK(r.Label, 200) || !textOK(r.Instructions, 16000) {
		return RuleError("Подтвердите имя, название и инструкции навыка.")
	}
	if err := validateSkillContract(r.Contract); err != nil {
		return err
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Создана версия навыка", r.Name, "operator", func(d *Data) error {
		version := 1
		for _, old := range d.SkillRevisions {
			if old.Name == r.Name && old.Version >= version {
				version = old.Version + 1
			}
		}
		skill := execution.Skill{ID: r.Name, Version: fmt.Sprint(version), Instructions: r.Instructions, Contract: &r.Contract}
		d.SkillRevisions = append(d.SkillRevisions, SkillRevision{ID: fmt.Sprintf("%s@%d", r.Name, version), Name: r.Name, Version: version, Label: r.Label, SHA256: hash(skill), Configuration: skill, CreatedAt: time.Now().UTC()})
		return nil
	})
}

func resolveSkills(d *Data, refs []string, p *execution.Profile) error {
	if len(refs) > 16 {
		return ErrLimit
	}
	for _, ref := range refs {
		found := false
		for _, revision := range d.SkillRevisions {
			if revision.ID != ref {
				continue
			}
			if revision.SHA256 != hash(revision.Configuration) || revision.Configuration.Contract == nil || validateSkillContract(*revision.Configuration.Contract) != nil {
				return RuleError("Нарушена целостность версии навыка.")
			}
			allowed := map[string]bool{}
			for _, tool := range leanToolNames(*p) {
				allowed[tool] = true
			}
			for _, tool := range revision.Configuration.Contract.RequiredTools {
				if !allowed[tool] {
					return RuleError("Инструмент навыка не разрешен профилю: " + tool)
				}
			}
			p.Skills = append(p.Skills, revision.Configuration)
			found = true
			break
		}
		if !found {
			return RuleError("Версия навыка не найдена: " + ref)
		}
	}
	return nil
}
