package researchweb

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
)

type ProfileRevision struct {
	SkillRefs     []string          `json:"skill_refs,omitempty"`
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Version       int               `json:"version"`
	Label         string            `json:"label"`
	Template      string            `json:"template"`
	SHA256        string            `json:"sha256"`
	Configuration execution.Profile `json:"configuration"`
	CreatedAt     time.Time         `json:"created_at"`
}

type ProfileRequest struct {
	SkillRefs        []string          `json:"skill_refs,omitempty"`
	ExpectedRevision int               `json:"expected_revision"`
	RequestID        string            `json:"request_id"`
	Name             string            `json:"name"`
	Label            string            `json:"label"`
	Template         string            `json:"template"`
	Model            string            `json:"model"`
	Skills           []execution.Skill `json:"skills"`
	Tools            []string          `json:"tools"`
	Limits           execution.Limits  `json:"limits"`
	Confirm          bool              `json:"confirm"`
}

func cloneProfile(p execution.Profile) execution.Profile {
	body, _ := json.Marshal(p)
	var out execution.Profile
	_ = json.Unmarshal(body, &out)
	return out
}

func (s *Service) lookupProfile(id string) (execution.Profile, bool) {
	s.profileMu.RLock()
	p, ok := s.profileCatalog[id]
	s.profileMu.RUnlock()
	if !ok {
		p, ok = s.Options.Profiles[id]
	}
	return cloneProfile(p), ok
}

func (s *Service) profile(id string) execution.Profile { p, _ := s.lookupProfile(id); return p }

func (s *Service) profileList() map[string]execution.Profile {
	s.profileMu.RLock()
	defer s.profileMu.RUnlock()
	out := map[string]execution.Profile{}
	for id, p := range s.Options.Profiles {
		out[id] = cloneProfile(p)
	}
	for id, p := range s.profileCatalog {
		out[id] = cloneProfile(p)
	}
	return out
}

func (s *Service) CreateProfile(r ProfileRequest) error {
	if len(r.SkillRefs) != 0 && len(r.Skills) != 0 {
		return RuleError("Выберите ссылки на версии навыков либо встроенные инструкции.")
	}
	if !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) ||
		!regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`).MatchString(r.Name) || !textOK(r.Label, 200) || !textOK(r.Model, 200) || len(r.Skills) > 16 {
		return RuleError("Подтвердите имя, название, модель и настройки профиля.")
	}
	// A browser may specialize a trusted template, not introduce a new command, endpoint or credential.
	p, ok := s.Options.Profiles[r.Template]
	if !ok {
		return RuleError("Выберите шаблон из конфигурации сервера.")
	}
	p = cloneProfile(p)
	p.Limits, p.Skills = r.Limits, r.Skills
	for _, skill := range r.Skills {
		if len(skill.ID) > 100 || len(skill.Version) > 100 || !textOK(skill.Instructions, 16000) {
			return ErrLimit
		}
	}
	if p.Model != nil {
		p.Model.Model, p.Model.Tools = r.Model, r.Tools
	}
	if p.External != nil {
		p.External.Model = r.Model
		if len(r.Tools) != 0 {
			return RuleError("Инструменты внешнего процесса закрепляются шаблоном сервера.")
		}
	}
	if err := p.Validate(); err != nil {
		return RuleError("Некорректные ограничения или инструменты профиля.")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Создана версия профиля", r.Name, "operator", func(d *Data) error {
		if err := resolveSkills(d, r.SkillRefs, &p); err != nil {
			return err
		}
		if err := p.Validate(); err != nil {
			return RuleError("Некорректный состав навыков профиля.")
		}
		version := 1
		for _, old := range d.ProfileRevisions {
			if old.Name == r.Name && old.Version >= version {
				version = old.Version + 1
			}
		}
		p.ID = fmt.Sprintf("%s@%d", r.Name, version)
		if _, exists := s.Options.Profiles[p.ID]; exists {
			return RuleError("Имя занято настройками сервера.")
		}
		d.ProfileRevisions = append(d.ProfileRevisions, ProfileRevision{SkillRefs: r.SkillRefs, ID: p.ID, Name: r.Name, Version: version, Label: r.Label, Template: r.Template, SHA256: hash(p), Configuration: p, CreatedAt: time.Now().UTC()})
		return nil
	})
	if err != nil {
		return err
	}
	v, err := s.Store.Read()
	if err != nil {
		return err
	}
	s.profileMu.Lock()
	defer s.profileMu.Unlock()
	for _, revision := range v.ProfileRevisions {
		s.profileCatalog[revision.ID] = cloneProfile(revision.Configuration)
		s.profileLabels[revision.ID] = revision.Label
	}
	return nil
}

func attemptProfile(s *Service, a Attempt) execution.Profile {
	if a.ProfileConfiguration != nil {
		return cloneProfile(*a.ProfileConfiguration)
	}
	return s.profile(a.Profile)
}
