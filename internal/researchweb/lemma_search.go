package researchweb

import (
	"strings"
	"time"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

type LemmaParameter struct {
	Name string `json:"name"`
	Type string `json:"type"`
}
type LemmaSignature struct {
	ID         string           `json:"id"`
	Library    string           `json:"library"`
	GoalSHA256 string           `json:"goal_sha256"`
	Domain     string           `json:"domain"`
	Parameters []LemmaParameter `json:"parameters"`
	Conditions []string         `json:"conditions"`
	Conclusion string           `json:"conclusion"`
	CreatedAt  time.Time        `json:"created_at"`
}
type SignatureRequest struct {
	ExpectedRevision int            `json:"expected_revision"`
	RequestID        string         `json:"request_id"`
	Signature        LemmaSignature `json:"signature"`
}
type LemmaQuery struct {
	Domain         string   `json:"domain"`
	ParameterTypes []string `json:"parameter_types"`
	Conclusion     string   `json:"conclusion"`
	Conditions     []string `json:"conditions"`
}
type LemmaMatch struct {
	Signature         LemmaSignature `json:"signature"`
	Lemma             string         `json:"lemma"`
	Module            string         `json:"module"`
	Candidate         string         `json:"candidate"`
	MissingConditions []string       `json:"missing_conditions"`
	Applicability     string         `json:"applicability"`
}
type ApplicabilityRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Target           string `json:"target"`
	Library          string `json:"library"`
	Substitution     string `json:"substitution"`
	Confirm          bool   `json:"confirm"`
}

func normalized(s string) string { return strings.Join(strings.Fields(s), " ") }

func (s *Service) IndexLemma(r SignatureRequest) error {
	q := r.Signature
	if r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || !textOK(q.Domain, 200) || !textOK(q.Conclusion, 1000) || len(q.Parameters) > 16 || len(q.Conditions) > 16 {
		return RuleError("Нужны область, параметры и заключение леммы.")
	}
	seen := map[string]bool{}
	for _, p := range q.Parameters {
		if !textOK(p.Name, 100) || !textOK(p.Type, 200) || seen[p.Name] {
			return RuleError("Некорректные параметры леммы.")
		}
		seen[p.Name] = true
	}
	for _, c := range q.Conditions {
		if !textOK(c, 1000) {
			return ErrLimit
		}
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Описана структура леммы", q.Library, "operator", func(d *Data) error {
		for _, l := range d.Library {
			if l.ID != q.Library {
				continue
			}
			if l.Status != "ready" || !libraryMatches(d, l) {
				return RuleError("Нужна действующая собранная лемма.")
			}
			q.ID, q.GoalSHA256, q.CreatedAt = identifier("signature"), leancheck.Digest(l.Goal), time.Now().UTC()
			d.LemmaSignatures = append(d.LemmaSignatures, q)
			return nil
		}
		return RuleError("Лемма не найдена.")
	})
}

func (s *Service) SearchLemmas(q LemmaQuery) ([]LemmaMatch, error) {
	if len(q.Domain) > 200 || len(q.Conclusion) > 1000 || len(q.ParameterTypes) > 16 || len(q.Conditions) > 16 {
		return nil, ErrLimit
	}
	for _, t := range append(append([]string{}, q.ParameterTypes...), q.Conditions...) {
		if len(t) > 1000 {
			return nil, ErrLimit
		}
	}
	v, err := s.Store.Read()
	if err != nil {
		return nil, err
	}
	out := []LemmaMatch{}
	seen := map[string]bool{}
	for i := len(v.LemmaSignatures) - 1; i >= 0; i-- {
		sig := v.LemmaSignatures[i]
		if seen[sig.Library] {
			continue
		}
		seen[sig.Library] = true
		if q.Domain != "" && normalized(q.Domain) != normalized(sig.Domain) {
			continue
		}
		if q.Conclusion != "" && normalized(q.Conclusion) != normalized(sig.Conclusion) {
			continue
		}
		if len(q.ParameterTypes) > 0 {
			if len(q.ParameterTypes) != len(sig.Parameters) {
				continue
			}
			matches := true
			for j, t := range q.ParameterTypes {
				if normalized(t) != normalized(sig.Parameters[j].Type) {
					matches = false
				}
			}
			if !matches {
				continue
			}
		}
		for _, l := range v.Library {
			if l.ID != sig.Library || l.Status != "ready" || !libraryMatches(&v.Data, l) || sig.GoalSHA256 != leancheck.Digest(l.Goal) {
				continue
			}
			missing := []string{}
			for _, c := range sig.Conditions {
				found := false
				for _, supplied := range q.Conditions {
					if normalized(c) == normalized(supplied) {
						found = true
					}
				}
				if !found {
					missing = append(missing, c)
				}
			}
			out = append(out, LemmaMatch{Signature: sig, Lemma: l.Lemma, Module: l.Module, Candidate: l.Goal.Candidate, MissingConditions: missing, Applicability: "requires_lean_obligation"})
			if len(out) == 100 {
				return out, nil
			}
		}
	}
	return out, nil
}

func (s *Service) CreateApplicability(r ApplicabilityRequest) error {
	if !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || !textOK(r.Substitution, 4000) {
		return RuleError("Подтвердите проверку переноса и описание подстановки.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Создана формальная проверка применения", r.Target, "operator", func(d *Data) error {
		target := d.entity(r.Target)
		if target == nil || target.FormalGoal == nil || target.Status == "accepted" || target.Status == "refuted" {
			return RuleError("Нужна открытая формальная цель.")
		}
		for _, l := range d.Library {
			if l.ID != r.Library {
				continue
			}
			if l.Lemma == target.ID || l.Status != "ready" || !libraryMatches(d, l) {
				return RuleError("Лемма недоступна для применения.")
			}
			for _, a := range d.Applications {
				if a.Lemma == l.Lemma && a.Target == target.ID {
					return RuleError("Применение уже предложено.")
				}
			}
			id := identifier("A")
			goal := *target.FormalGoal
			// The obligation keeps the exact target proposition; annotations cannot weaken it.
			goal.Source = "import " + l.Module + "\n" + goal.Source
			obligation := newEntity(id, "Перенос леммы: "+target.Title, "application", target.Study, target.Statement, target.Assumptions)
			obligation.FormalGoal = &goal
			obligation.Dependencies = []string{l.Lemma}
			d.Entities = append(d.Entities, obligation)
			target = d.entity(r.Target)
			target.Dependencies = append(target.Dependencies, id)
			target.Revision++
			target.Proof, target.ProofAuthor, target.ProofAttempt, target.ProofVerification, target.ResearchResult = "", "", "", "", ""
			target.DependencyRevisions = nil
			d.Applications = append(d.Applications, Application{ID: id, Lemma: l.Lemma, LemmaRevision: l.LemmaRevision, Target: target.ID, State: "candidate"})
			d.WorkLinks = append(d.WorkLinks, [2]string{target.ID, id}, [2]string{id, l.Lemma})
			d.addTask(id, "Проверить перенос леммы", "formalize", "Докажи точную формальную цель с помощью принятой леммы "+l.Goal.Candidate+". Предложенная оператором подстановка не проверена: "+r.Substitution)
			return nil
		}
		return RuleError("Лемма не найдена.")
	})
}
