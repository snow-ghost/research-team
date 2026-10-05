package researchweb

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

type MemoryEntry struct {
	ID             string    `json:"id"`
	Target         string    `json:"target"`
	TargetRevision int       `json:"target_revision"`
	GoalSHA256     string    `json:"goal_sha256,omitempty"`
	Kind           string    `json:"kind"`
	Content        string    `json:"content"`
	Conditions     string    `json:"conditions"`
	Attempt        string    `json:"attempt,omitempty"`
	Check          string    `json:"check,omitempty"`
	Verification   string    `json:"verification,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}
type MemoryRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Target           string `json:"target"`
	Kind             string `json:"kind"`
	Content          string `json:"content"`
	Conditions       string `json:"conditions"`
	Attempt          string `json:"attempt,omitempty"`
	Check            string `json:"check,omitempty"`
	Verification     string `json:"verification,omitempty"`
}
type MemoryHit struct {
	ID         string `json:"id"`
	Target     string `json:"target"`
	Study      string `json:"study"`
	Revision   int    `json:"revision"`
	Kind       string `json:"kind"`
	Content    string `json:"content"`
	Conditions string `json:"conditions"`
	Trust      string `json:"trust"`
	Module     string `json:"module,omitempty"`
	GoalSHA256 string `json:"goal_sha256,omitempty"`
}

func (s *Service) AddMemory(r MemoryRequest) error {
	if r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || !textOK(r.Content, 4000) || len(r.Conditions) > 4000 {
		return RuleError("Нужны содержание памяти и снимок.")
	}
	if r.Kind != "note" && r.Kind != "failed_method" && r.Kind != "counterexample" {
		return RuleError("Неизвестный вид памяти.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Записан материал памяти", r.Target, "operator", func(d *Data) error {
		e := d.entity(r.Target)
		if e == nil {
			return RuleError("Цель не найдена.")
		}
		if r.Kind == "counterexample" {
			v := d.verification(r.Verification)
			if v == nil || v.Target != e.ID || v.Purpose != "refutation" || !verificationMatches(d, *v) || !verifiedReportMatches(*v) || e.Status != "refuted" || e.RefutationVerification != v.ID {
				return RuleError("Для контрпримера нужно принятое проверенное отрицание этой версии.")
			}
		}
		if r.Kind == "failed_method" {
			a := d.attempt(r.Attempt)
			if a == nil || a.Target != e.ID || a.TargetRevision != e.Revision || active(a.Status) {
				return RuleError("Нужна завершенная попытка текущей версии.")
			}
			if r.Check != "" {
				if !requestPattern.MatchString(r.Check) {
					return RuleError("Недопустимый идентификатор проверки.")
				}
				body, err := readBoundedFile(filepath.Join(s.Options.Config.DataDir, "attempts", a.ID, "toolchecks", r.Check, "report.json"), 128000)
				if err != nil {
					return err
				}
				var c IntermediateCheck
				if json.Unmarshal(body, &c) != nil || e.FormalGoal == nil {
					return RuleError("Некорректная запись промежуточной проверки.")
				}
				expected := *e.FormalGoal
				if c.Purpose == "refutation" {
					expected = refutationGoal(expected)
				} else if c.Purpose != "proof" {
					return RuleError("Неизвестное назначение промежуточной проверки.")
				}
				if c.ID != r.Check || c.Attempt != a.ID || c.Target != e.ID || c.TargetRevision != e.Revision || c.Report.Status != "failed" || c.Report.SourceSHA256 != leancheck.Digest(c.Source) || c.Report.GoalSHA256 != leancheck.Digest(expected) || leancheck.Digest(c.Goal) != leancheck.Digest(expected) {
					return RuleError("Нужна подтвержденная запись неудачной промежуточной проверки.")
				}
			} else if a.Status == "candidate" {
				return RuleError("Успешный кандидат сам по себе не является неудачным методом.")
			}
		}
		m := MemoryEntry{ID: identifier("memory"), Target: e.ID, TargetRevision: e.Revision, Kind: r.Kind, Content: r.Content, Conditions: r.Conditions, Attempt: r.Attempt, Check: r.Check, Verification: r.Verification, CreatedAt: time.Now().UTC()}
		if e.FormalGoal != nil {
			m.GoalSHA256 = leancheck.Digest(*e.FormalGoal)
		}
		d.Memory = append(d.Memory, m)
		return nil
	})
}

func memoryEntryTrust(d *Data, m MemoryEntry) string {
	e := d.entity(m.Target)
	if e == nil || e.Revision != m.TargetRevision || (e.FormalGoal != nil && leancheck.Digest(*e.FormalGoal) != m.GoalSHA256) {
		return "stale"
	}
	if m.Kind == "counterexample" {
		v := d.verification(m.Verification)
		if v == nil || v.Purpose != "refutation" || v.Target != e.ID || e.RefutationVerification != v.ID || e.Status != "refuted" || !verifiedReportMatches(*v) || !verificationMatches(d, *v) {
			return "stale"
		}
		return "verified_refutation"
	}
	if m.Kind == "failed_method" {
		return "recorded_failure"
	}
	return "unverified_note"
}

func memoryForTarget(d Data, target string) []MemoryHit {
	hits := []MemoryHit{}
	for _, m := range d.Memory {
		if m.Target != target || memoryEntryTrust(&d, m) == "stale" {
			continue
		}
		e := d.entity(target)
		hits = append(hits, MemoryHit{ID: m.ID, Target: target, Study: e.Study, Revision: m.TargetRevision, Kind: m.Kind, Content: m.Content, Conditions: m.Conditions, Trust: memoryEntryTrust(&d, m), GoalSHA256: m.GoalSHA256})
		if len(hits) == 12 {
			break
		}
	}
	return hits
}

func (s *Service) SearchMemory(query, study string, other bool) ([]MemoryHit, error) {
	if len(query) > 300 {
		return nil, RuleError("Запрос поиска слишком длинный.")
	}
	v, err := s.Store.Read()
	if err != nil {
		return nil, err
	}
	if study != "" && v.study(study) == nil {
		return nil, RuleError("Исследование не найдено.")
	}
	words := strings.Fields(strings.ToLower(query))
	if len(words) > 12 {
		return nil, RuleError("Используйте до двенадцати слов.")
	}
	matches := func(text string) bool {
		text = strings.ToLower(text)
		for _, w := range words {
			if !strings.Contains(text, w) {
				return false
			}
		}
		return true
	}
	hits := []MemoryHit{}
	for _, l := range v.Library {
		e := v.entity(l.Lemma)
		if e == nil || l.Status != "ready" || !libraryMatches(&v.Data, l) || v.effective(e.ID, map[string]bool{}) != "accepted" || (study != "" && e.Study != study && !other) || !matches(e.Title+" "+e.Statement+" "+e.Assumptions+" "+l.Goal.Source) {
			continue
		}
		hits = append(hits, MemoryHit{ID: l.ID, Target: e.ID, Study: e.Study, Revision: e.Revision, Kind: "lemma", Content: e.Statement, Conditions: e.Assumptions, Trust: "accepted_lemma", Module: l.Module, GoalSHA256: leancheck.Digest(l.Goal)})
	}
	for _, m := range v.Memory {
		e := v.entity(m.Target)
		if e == nil || (study != "" && e.Study != study) || !matches(m.Content+" "+m.Conditions+" "+e.Title) {
			continue
		}
		hits = append(hits, MemoryHit{ID: m.ID, Target: e.ID, Study: e.Study, Revision: m.TargetRevision, Kind: m.Kind, Content: m.Content, Conditions: m.Conditions, Trust: memoryEntryTrust(&v.Data, m), GoalSHA256: m.GoalSHA256})
	}
	if len(hits) > 100 {
		hits = hits[:100]
	}
	return hits, nil
}
