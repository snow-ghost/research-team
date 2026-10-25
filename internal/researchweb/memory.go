package researchweb

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

type MemoryEntry struct {
	ScopeSHA256      string    `json:"scope_sha256,omitempty"`
	ConditionsSHA256 string    `json:"conditions_sha256,omitempty"`
	Method           string    `json:"method,omitempty"`
	FailureClass     string    `json:"failure_class,omitempty"`
	Branch           string    `json:"branch,omitempty"`
	Match            string    `json:"match,omitempty"`
	EvidenceSHA256   string    `json:"evidence_sha256,omitempty"`
	Obligations      []string  `json:"obligations,omitempty"`
	ID               string    `json:"id"`
	Target           string    `json:"target"`
	TargetRevision   int       `json:"target_revision"`
	GoalSHA256       string    `json:"goal_sha256,omitempty"`
	Kind             string    `json:"kind"`
	Content          string    `json:"content"`
	Conditions       string    `json:"conditions"`
	Attempt          string    `json:"attempt,omitempty"`
	Check            string    `json:"check,omitempty"`
	Verification     string    `json:"verification,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}
type MemoryRequest struct {
	Method           string `json:"method,omitempty"`
	Branch           string `json:"branch,omitempty"`
	Match            string `json:"match,omitempty"`
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
	Method         string   `json:"method,omitempty"`
	FailureClass   string   `json:"failure_class,omitempty"`
	AutoReuse      bool     `json:"auto_reuse"`
	EvidenceSHA256 string   `json:"evidence_sha256,omitempty"`
	Obligations    []string `json:"obligations,omitempty"`
	ID             string   `json:"id"`
	Target         string   `json:"target"`
	Study          string   `json:"study"`
	Revision       int      `json:"revision"`
	Kind           string   `json:"kind"`
	Content        string   `json:"content"`
	Conditions     string   `json:"conditions"`
	Trust          string   `json:"trust"`
	Module         string   `json:"module,omitempty"`
	GoalSHA256     string   `json:"goal_sha256,omitempty"`
}

func (s *Service) AddMemory(r MemoryRequest) error {
	if r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || !textOK(r.Content, 4000) || len(r.Conditions) > 4000 {
		return RuleError("Нужны содержание памяти и снимок.")
	}
	if r.Kind != "note" && r.Kind != "failed_method" && r.Kind != "counterexample" && r.Kind != "unmet_assumption" {
		return RuleError("Неизвестный вид памяти.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Записан материал памяти", r.Target, "operator", func(d *Data) error {
		e := d.entity(r.Target)
		if e == nil {
			return RuleError("Цель не найдена.")
		}
		if r.Method != "" {
			if _, ok := researchMethod(r.Method); !ok {
				return RuleError("Неизвестный прием памяти.")
			}
		}
		if r.Branch != "" {
			found := false
			for _, b := range d.Branches {
				if b.ID == r.Branch && b.Target == e.ID && e.FormalGoal != nil && b.GoalSHA256 == leancheck.Digest(*e.FormalGoal) && (r.Method == "" || b.Method == r.Method) {
					found = true
					r.Method = b.Method
				}
			}
			if !found {
				return RuleError("Ветвь памяти не относится к текущей цели и приему.")
			}
		}
		failureClass, evidence := "", ""
		obligations := []string{}
		if r.Kind != "note" && r.Conditions == "" {
			r.Conditions = e.Assumptions
		}
		if r.Kind == "unmet_assumption" {
			for _, m := range d.LemmaMatches {
				if m.ID == r.Match && m.Target == e.ID && matchCurrent(d, m) && m.Report.Status == "obligations" {
					for _, p := range m.Report.Missing {
						if p.Kind == "premise" && p.ClosedType != "" {
							obligations = append(obligations, p.ClosedType)
						}
					}
					evidence = hash(m.Report)
				}
			}
			if len(obligations) == 0 {
				return RuleError("Нужен действующий подбор с явными недоказанными предпосылками.")
			}
			failureClass = "unmet_assumption"
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
			failureClass = failedAttemptClass(*a)
			evidence = a.ResultSHA256
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
				failureClass, evidence = "native_check_failed", hash(c)
			} else if a.Status == "candidate" {
				return RuleError("Успешный кандидат сам по себе не является неудачным методом.")
			}
		}
		m := MemoryEntry{ScopeSHA256: memoryScope(*e), ConditionsSHA256: hash(r.Conditions), Method: r.Method, Branch: r.Branch, Match: r.Match, FailureClass: failureClass, EvidenceSHA256: evidence, Obligations: obligations, ID: identifier("memory"), Target: e.ID, TargetRevision: e.Revision, Kind: r.Kind, Content: r.Content, Conditions: r.Conditions, Attempt: r.Attempt, Check: r.Check, Verification: r.Verification, CreatedAt: time.Now().UTC()}
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
	if m.ScopeSHA256 != "" && (m.ScopeSHA256 != memoryScope(*e) || m.ConditionsSHA256 != hash(m.Conditions)) {
		return "stale"
	}
	if m.Kind == "unmet_assumption" {
		for _, x := range d.LemmaMatches {
			if x.ID == m.Match && matchCurrent(d, x) && hash(x.Report) == m.EvidenceSHA256 {
				return "recorded_obligation"
			}
		}
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
		if m.ScopeSHA256 == "" {
			return "legacy_unscoped"
		}
		if a := d.attempt(m.Attempt); a == nil || a.Target != m.Target || a.TargetRevision != m.TargetRevision || active(a.Status) || m.Check == "" && a.ResultSHA256 != m.EvidenceSHA256 {
			return "stale"
		}
		return "recorded_failure"
	}
	return "unverified_note"
}

func memoryForTarget(d Data, target string) []MemoryHit {
	hits := []MemoryHit{}
	for _, m := range d.Memory {
		if m.Target != target || memoryEntryTrust(&d, m) == "stale" || m.Kind != "note" && !memoryReusable(&d, m) {
			continue
		}
		e := d.entity(target)
		hits = append(hits, memoryHit(&d, m, e.Study))
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
		hits = append(hits, memoryHit(&v.Data, m, e.Study))
	}
	if len(hits) > 100 {
		hits = hits[:100]
	}
	return hits, nil
}
