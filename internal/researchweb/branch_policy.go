package researchweb

import (
	"slices"
	"time"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

type BranchPolicy struct {
	Enabled          bool      `json:"enabled"`
	Mode             string    `json:"mode"`
	Methods          []string  `json:"methods"`
	FailureThreshold int       `json:"failure_threshold"`
	MaxDecisions     int       `json:"max_decisions"`
	Used             int       `json:"used"`
	Tried            []string  `json:"tried,omitempty"`
	SHA256           string    `json:"sha256"`
	ApprovedAt       time.Time `json:"approved_at"`
}
type BranchPolicyRequest struct {
	ExpectedRevision int          `json:"expected_revision"`
	RequestID        string       `json:"request_id"`
	Policy           BranchPolicy `json:"policy"`
	Confirm          bool         `json:"confirm"`
}
type BranchRecommendation struct {
	Branch       string   `json:"branch"`
	Kind         string   `json:"kind"`
	Method       string   `json:"method,omitempty"`
	Reason       string   `json:"reason"`
	Evidence     []string `json:"evidence"`
	Attempt      string   `json:"attempt,omitempty"`
	Executable   bool     `json:"executable"`
	PolicySHA256 string   `json:"policy_sha256,omitempty"`
}

func (s *Service) SetBranchPolicy(id string, r BranchPolicyRequest) error {
	p := r.Policy
	if r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || !r.Confirm || !slices.Contains([]string{"observe", "execute"}, p.Mode) || p.MaxDecisions < 1 || p.MaxDecisions > 5 || p.FailureThreshold < 1 || p.FailureThreshold > 5 || len(p.Methods) < 1 || len(p.Methods) > 6 {
		return RuleError("Подтвердите ограниченные правила выбора ветви.")
	}
	seen := map[string]bool{}
	for _, m := range p.Methods {
		if _, ok := researchMethod(m); !ok || seen[m] {
			return RuleError("Нужны разные известные приемы.")
		}
		seen[m] = true
	}
	p.Used = 0
	p.Tried = nil
	p.ApprovedAt = time.Now().UTC()
	p.SHA256 = hash(struct {
		Version        int
		Enabled        bool
		Mode           string
		Methods        []string
		Threshold, Max int
	}{1, p.Enabled, p.Mode, p.Methods, p.FailureThreshold, p.MaxDecisions})
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Утверждены правила выбора ветви", id, "operator", func(d *Data) error {
		for i := range d.Branches {
			b := &d.Branches[i]
			if b.ID != id {
				continue
			}
			e := d.entity(b.Target)
			if e == nil || e.FormalGoal == nil || leancheck.Digest(*e.FormalGoal) != b.GoalSHA256 || slices.Contains([]string{"completed", "invalidated", "abandoned", "cancelled"}, b.Status) || studyClosed(d, b.Study) {
				return RuleError("Нужна действующая открытая ветвь.")
			}
			study := d.study(b.Study)
			if p.Enabled && p.Mode == "execute" && (study == nil || study.Budget == nil || study.Budget.MaxAttempts < 1 || study.Budget.MaxModelRequests < 1) {
				return RuleError("Автоматическим действиям нужен конечный общий бюджет попыток и запросов.")
			}
			p.Tried = []string{b.Method}
			b.Policy = &p
			return nil
		}
		return RuleError("Ветвь не найдена.")
	})
}

func (s *Service) recommendBranch(d *Data, b ResearchBranch) BranchRecommendation {
	r := BranchRecommendation{Branch: b.ID, Kind: "continue", Reason: "Нет новых оснований для смены ветви.", Evidence: []string{}}
	p := b.Policy
	if p == nil || !p.Enabled {
		r.Kind = "disabled"
		r.Reason = "Правила не включены."
		return r
	}
	r.PolicySHA256 = p.SHA256
	e := d.entity(b.Target)
	if e == nil || e.FormalGoal == nil || leancheck.Digest(*e.FormalGoal) != b.GoalSHA256 || !branchLibrariesMatch(d, b) || studyClosed(d, b.Study) || slices.Contains([]string{"completed", "invalidated", "abandoned", "cancelled"}, b.Status) {
		r.Kind = "stale"
		r.Reason = "Ветвь закрыта или ее основания изменились."
		return r
	}
	if b.Held || p.Used >= p.MaxDecisions || d.Maintenance != nil || studySearchSuspended(d, b.Study) {
		r.Kind = "hold"
		r.Reason = "Удержание, обслуживание, готовый результат или предел решений."
		return r
	}
	for _, a := range d.Attempts {
		target := d.entity(a.Target)
		if target != nil && target.Study == b.Study && active(a.Status) {
			r.Kind = "wait"
			r.Reason = "Есть незавершенная попытка исследования."
			return r
		}
	}
	budget := s.observeBudget(d, b.Study)
	study := d.study(b.Study)
	if budget.UnknownUsage > 0 {
		r.Kind = "needs_operator"
		r.Reason = "Есть неизвестный расход; новый поиск автоматически не назначается."
		return r
	}
	if study != nil && study.Budget != nil && (budget.DeadlineReached || study.Budget.MaxAttempts > 0 && budget.Attempts >= study.Budget.MaxAttempts || study.Budget.MaxModelRequests > 0 && budget.ChargedModelRequests >= study.Budget.MaxModelRequests || study.Budget.MaxOutputTokens > 0 && budget.ChargedTokens >= study.Budget.MaxOutputTokens) {
		r.Kind = "stop"
		r.Reason = "Исчерпан общий бюджет исследования."
	}
	if r.Kind == "continue" {
		for _, v := range d.Verifications {
			if v.Target == e.ID && v.Purpose != "refutation" && verificationMatches(d, v) && verifiedReportMatches(v) {
				r.Kind = "hold"
				r.Reason = "Исходник уже проверен; нужна рецензия или приемка, а не новый поиск."
				return r
			}
			if v.Target == e.ID && v.Purpose == "refutation" && verificationMatches(d, v) && verifiedReportMatches(v) {
				r.Kind = "stop"
				r.Reason = "Проверено отрицание цели; требуется отдельная рецензия и приемка."
				r.Evidence = append(r.Evidence, v.ID)
			}
		}
	}
	if r.Kind == "continue" && b.Status == "blocked" {
		for _, m := range d.Memory {
			if m.Target == e.ID && m.Kind == "unmet_assumption" && memoryReusable(d, m) {
				r.Kind = "split"
				r.Reason = "Есть явная недоказанная предпосылка; требуется отдельный план."
				r.Evidence = append(r.Evidence, m.ID)
			}
		}
	}
	if r.Kind == "continue" && b.Status == "blocked" && b.Team != "" {
		failures := []Attempt{}
		for _, a := range d.Attempts {
			if a.TeamID != b.Team || a.Target != e.ID || a.TargetRevision != e.Revision || a.Role == "counterexample" || a.Role == "counter" {
				continue
			}
			if a.Status == "candidate" {
				failures = nil
				continue
			}
			if slices.Contains([]string{"failed", "timed_out", "limit_reached"}, a.Status) && a.RemoteOutcome != "unknown" && a.RemoteOutcome != "not_started" {
				failures = append(failures, a)
			}
		}
		if len(failures) >= p.FailureThreshold {
			for _, a := range failures {
				r.Evidence = append(r.Evidence, a.ID)
			}
			r.Attempt = failures[len(failures)-1].ID
			for _, m := range p.Methods {
				if m != b.Method && !slices.Contains(p.Tried, m) {
					r.Kind = "change_method"
					r.Method = m
					r.Reason = "Достигнут утвержденный предел технических неуспехов; выбран следующий разрешенный прием. Это не вывод о невозможности доказательства."
					break
				}
			}
			if r.Kind == "continue" {
				r.Kind = "stop"
				r.Reason = "Разрешенные приемы исчерпаны; требуется решение оператора."
			}
		}
	}
	r.Executable = p.Mode == "execute" && !d.Paused && slices.Contains([]string{"change_method", "split", "stop"}, r.Kind)
	return r
}

func (s *Service) BranchRecommendations(study string) ([]BranchRecommendation, error) {
	v, err := s.Store.Read()
	if err != nil {
		return nil, err
	}
	out := []BranchRecommendation{}
	for _, b := range v.Branches {
		if study == "" || b.Study == study {
			out = append(out, s.recommendBranch(&v.Data, b))
		}
	}
	return out, nil
}

func (s *Service) advanceBranchPolicies() {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := s.Store.Read()
	if err != nil || v.Paused || v.Maintenance != nil {
		return
	}
	for _, b := range v.Branches {
		choice := s.recommendBranch(&v.Data, b)
		if !choice.Executable {
			continue
		}
		id := "policy-" + hash([]any{b.ID, choice.PolicySHA256, b.Policy.Used, choice})[:48]
		r := BranchDecisionRequest{ExpectedRevision: v.Revision, RequestID: id, Kind: choice.Kind, Method: choice.Method, Note: choice.Reason, NoteKind: "operational", EvidenceAttempt: choice.Attempt, Confirm: true}
		err = s.Store.Change(v.Revision, id, hash(r), "Решение по утвержденным правилам ветви", b.ID, "policy", func(d *Data) error {
			for i := range d.Branches {
				current := &d.Branches[i]
				if current.ID != b.ID {
					continue
				}
				next := s.recommendBranch(d, *current)
				if !next.Executable || hash(next) != hash(choice) {
					return ErrConflict
				}
				current.Policy.Used++
				if choice.Method != "" {
					current.Policy.Tried = append(append([]string{}, current.Policy.Tried...), choice.Method)
				}
				return s.decideBranchData(d, b.ID, r, "policy", choice.PolicySHA256)
			}
			return ErrConflict
		})
		if err == nil {
			s.cancelRequested()
		}
		return
	}
}
