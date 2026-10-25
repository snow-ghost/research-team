package researchweb

import (
	"slices"
	"time"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

type BranchDecision struct {
	ID              string    `json:"id"`
	Kind            string    `json:"kind"`
	Note            string    `json:"note"`
	NoteKind        string    `json:"note_kind"`
	TargetRevision  int       `json:"target_revision"`
	GoalSHA256      string    `json:"goal_sha256"`
	EvidenceAttempt string    `json:"evidence_attempt,omitempty"`
	EvidenceSHA256  string    `json:"evidence_sha256,omitempty"`
	Replacement     string    `json:"replacement,omitempty"`
	Outcome         string    `json:"outcome"`
	CreatedAt       time.Time `json:"created_at"`
}

type BranchDecisionRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Kind             string `json:"kind"`
	Method           string `json:"method,omitempty"`
	Note             string `json:"note"`
	NoteKind         string `json:"note_kind,omitempty"`
	EvidenceAttempt  string `json:"evidence_attempt,omitempty"`
	Confirm          bool   `json:"confirm"`
}

func (s *Service) DecideBranch(id string, r BranchDecisionRequest) error {
	if r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || !textOK(r.Note, 4000) || !r.Confirm || !slices.Contains([]string{"continue", "change_method", "split", "stop"}, r.Kind) {
		return RuleError("Подтвердите решение по ветви и основание.")
	}
	if r.NoteKind == "" {
		r.NoteKind = "unspecified"
	}
	if !slices.Contains([]string{"operational", "mathematical_hint", "unspecified"}, r.NoteKind) {
		return RuleError("Неизвестный вид основания решения.")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.Store.Change(r.ExpectedRevision, r.RequestID, hash(struct {
		ID      string
		Request BranchDecisionRequest
	}{id, r}), "Решение по исследовательской ветви", id, "operator", func(d *Data) error {
		index := -1
		for i := range d.Branches {
			if d.Branches[i].ID == id {
				index = i
				break
			}
		}
		if index < 0 {
			return RuleError("Ветвь не найдена.")
		}
		b := d.Branches[index]
		e := d.entity(b.Target)
		if e == nil || e.FormalGoal == nil || b.Team == "" && e.Revision != b.TargetRevision || leancheck.Digest(*e.FormalGoal) != b.GoalSHA256 || studyClosed(d, b.Study) || !branchLibrariesMatch(d, b) || slices.Contains([]string{"completed", "invalidated", "abandoned", "cancelled"}, b.Status) {
			return RuleError("Нужна действующая открытая версия ветви.")
		}
		if len(b.Decisions) >= 100 {
			return ErrLimit
		}
		decision := BranchDecision{ID: r.RequestID, Kind: r.Kind, Note: r.Note, NoteKind: r.NoteKind, TargetRevision: e.Revision, GoalSHA256: b.GoalSHA256, EvidenceAttempt: r.EvidenceAttempt, CreatedAt: time.Now().UTC()}
		if r.EvidenceAttempt != "" {
			a := d.attempt(r.EvidenceAttempt)
			if a == nil || a.Target != e.ID || a.TargetRevision != e.Revision || active(a.Status) {
				return RuleError("Основание должно ссылаться на завершенную попытку этой версии цели.")
			}
			decision.EvidenceSHA256 = a.ResultSHA256
		}
		teamCommand := TeamCommand{RequestID: r.RequestID, Kind: "pause", Note: r.Note, NoteKind: r.NoteKind, Confirm: true}
		switch r.Kind {
		case "continue":
			if studySearchSuspended(d, b.Study) {
				return RuleError("Готовый результат ожидает решения о приемке.")
			}
			if b.Team != "" {
				d.Branches[index].Held = false
				team := d.team(b.Team)
				if team == nil {
					return ErrConflict
				}
				if team.Status != "running" {
					teamCommand.Kind = "resume"
					if err := s.controlTeamData(d, b.Team, teamCommand); err != nil {
						return err
					}
				}
				b.Status = team.Status
			} else {
				b.Status = "ready"
			}
			b.Held = false
			decision.Outcome = "continued"
		case "split":
			if b.Team != "" {
				if err := s.controlTeamData(d, b.Team, teamCommand); err != nil {
					return err
				}
			}
			b.Held = true
			b.Status = "paused"
			decision.Outcome = "awaiting_separate_plan"
		case "stop", "change_method":
			if b.Team != "" {
				teamCommand.Kind = "stop"
				if err := s.controlTeamData(d, b.Team, teamCommand); err != nil {
					return err
				}
			}
			b.Held = true
			b.Status = "abandoned"
			decision.Outcome = "stopped"
			if r.Kind == "change_method" {
				method, ok := researchMethod(r.Method)
				if !ok || r.Method == b.Method {
					return RuleError("Выберите другой исследовательский прием.")
				}
				next := b
				next.ID, next.Parent, next.Plan, next.Team = identifier("branch"), b.ID, "", ""
				next.Method, next.Rationale, next.Status, next.Reason = r.Method, r.Note, "ready", "Новая ветвь после явной смены приема."
				next.Held = false
				next.Decisions = nil
				next.CreatedAt = time.Now().UTC()
				next.TargetRevision = e.Revision
				next.Configuration.Methods = []string{r.Method}
				next.Basis = &MethodBasis{Source: "operator", Method: r.Method, Procedure: method.Procedure, Rationale: r.Note, ConfirmedAt: next.CreatedAt}
				decision.Replacement = next.ID
				decision.Outcome = "replacement_created"
				d.Branches = append(d.Branches, next)
			}
		}
		b.Reason = r.Note
		b.Decisions = append(b.Decisions, decision)
		d.Branches[index] = b
		return nil
	})
	if err == nil {
		s.cancelRequested()
	}
	return err
}
