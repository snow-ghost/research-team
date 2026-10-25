package researchweb

import (
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

type MemoryRelation struct {
	ID                string    `json:"id"`
	Target            string    `json:"target"`
	TargetRevision    int       `json:"target_revision"`
	SourceRevision    int       `json:"source_revision"`
	SourceScopeSHA256 string    `json:"source_scope_sha256"`
	TargetScopeSHA256 string    `json:"target_scope_sha256"`
	Obligation        string    `json:"obligation"`
	GoalSHA256        string    `json:"goal_sha256"`
	Note              string    `json:"note"`
	Status            string    `json:"status"`
	AutoReuse         bool      `json:"auto_reuse"`
	CreatedAt         time.Time `json:"created_at"`
}
type MemorySuggestion struct {
	Memory      MemoryHit        `json:"memory"`
	Score       int              `json:"score"`
	Differences []string         `json:"differences"`
	Relations   []MemoryRelation `json:"relations"`
	Status      string           `json:"status"`
	AutoReuse   bool             `json:"auto_reuse"`
}
type MemoryRelationRequest struct {
	ExpectedRevision int            `json:"expected_revision"`
	RequestID        string         `json:"request_id"`
	Memory           string         `json:"memory"`
	Target           string         `json:"target"`
	Goal             leancheck.Goal `json:"goal"`
	Note             string         `json:"note"`
	Confirm          bool           `json:"confirm"`
}

func memoryWords(text string) map[string]bool {
	out := map[string]bool{}
	for _, word := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len([]rune(word)) >= 3 {
			out[word] = true
		}
	}
	return out
}

func (s *Service) MemorySuggestions(target string) ([]MemorySuggestion, error) {
	v, err := s.Store.Read()
	if err != nil {
		return nil, err
	}
	e := v.entity(target)
	if e == nil {
		return nil, RuleError("Цель не найдена.")
	}
	words := memoryWords(e.Title + " " + e.Statement + " " + e.Assumptions)
	out := []MemorySuggestion{}
	for _, m := range v.Memory {
		source := v.entity(m.Target)
		if source == nil || source.Study != e.Study || source.ID == e.ID {
			continue
		}
		score := 0
		for word := range memoryWords(source.Title + " " + m.Content + " " + m.Conditions + " " + source.Statement) {
			if words[word] {
				score++
			}
		}
		if score == 0 {
			continue
		}
		x := MemorySuggestion{Memory: memoryHit(&v.Data, m, e.Study), Score: score, Differences: []string{}, Relations: []MemoryRelation{}, Status: "requires_scope_check"}
		x.Memory.AutoReuse = false
		for _, field := range []struct {
			name string
			a, b any
		}{{"statement", source.Statement, e.Statement}, {"assumptions", m.Conditions, e.Assumptions}, {"formal_goal", source.FormalGoal, e.FormalGoal}, {"required_libraries", source.RequiredLibraries, e.RequiredLibraries}} {
			if hash(field.a) != hash(field.b) {
				x.Differences = append(x.Differences, field.name)
			}
		}
		for _, relation := range m.Relations {
			if relation.Target != target {
				continue
			}
			relation.Status = "open"
			o := v.entity(relation.Obligation)
			if relation.TargetRevision != e.Revision || relation.SourceRevision != source.Revision || relation.SourceScopeSHA256 != memoryScope(*source) || relation.TargetScopeSHA256 != memoryScope(*e) || memoryEntryTrust(&v.Data, m) == "stale" {
				relation.Status = "stale"
			} else if o == nil || o.FormalGoal == nil || leancheck.Digest(*o.FormalGoal) != relation.GoalSHA256 {
				relation.Status = "stale"
			} else if hasVerifiedProof(&v.Data, o) {
				relation.Status = "formal_relation_verified"
			}
			x.Relations = append(x.Relations, relation)
		}
		if x.Memory.Trust == "stale" || x.Memory.Trust == "legacy_unscoped" {
			x.Status = "historical_only"
		}
		out = append(out, x)
	}
	slices.SortFunc(out, func(a, b MemorySuggestion) int {
		if a.Score != b.Score {
			return b.Score - a.Score
		}
		return strings.Compare(a.Memory.ID, b.Memory.ID)
	})
	if len(out) > 20 {
		out = out[:20]
	}
	return out, nil
}

func (s *Service) CreateMemoryRelation(r MemoryRelationRequest) error {
	if r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || !r.Confirm || r.Goal.Validate() != nil || !textOK(r.Note, 4000) {
		return RuleError("Подтвердите отдельное обязательство проверки связи и его формальную цель.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Создано обязательство проверки применимости памяти", r.Target, "operator", func(d *Data) error {
		target := d.entity(r.Target)
		if target == nil || target.FormalGoal == nil || target.Status == "accepted" || target.Status == "refuted" || studyClosed(d, target.Study) {
			return RuleError("Нужна открытая формальная цель.")
		}
		for i := range d.Memory {
			m := &d.Memory[i]
			if m.ID != r.Memory {
				continue
			}
			source := d.entity(m.Target)
			if source == nil || source.Study != target.Study || source.ID == target.ID || memoryEntryTrust(d, *m) == "stale" || memoryEntryTrust(d, *m) == "legacy_unscoped" {
				return RuleError("Нужна действующая запись другого утверждения этого исследования.")
			}
			if len(m.Relations) >= 20 {
				return ErrLimit
			}
			id := identifier("O")
			o := newEntity(id, "Проверка условий применимости памяти", "obligation", target.Study, r.Note, "Формальную связь задал оператор. Ее доказательство не устанавливает логическую эквивалентность текстовых условий и не переносит технический исход попытки.")
			o.FormalGoal = &r.Goal
			relation := MemoryRelation{ID: identifier("relation"), Target: target.ID, TargetRevision: target.Revision, SourceRevision: source.Revision, SourceScopeSHA256: memoryScope(*source), TargetScopeSHA256: memoryScope(*target), Obligation: id, GoalSHA256: leancheck.Digest(r.Goal), Note: r.Note, Status: "open", CreatedAt: time.Now().UTC()}
			m.Relations = append(m.Relations, relation)
			d.Entities = append(d.Entities, o)
			d.WorkLinks = append(d.WorkLinks, [2]string{r.Target, id}, [2]string{id, m.Target})
			d.addTask(id, "Проверить формальную связь условий памяти", "proof", r.Note)
			return nil
		}
		return RuleError("Запись памяти не найдена.")
	})
}
