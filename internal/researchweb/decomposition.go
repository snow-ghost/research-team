package researchweb

import (
	"github.com/snow-ghost/research-team/internal/leancheck"
	"time"
)

type ProposedClaim struct {
	DependsOn   []int           `json:"depends_on,omitempty"`
	Title       string          `json:"title"`
	Statement   string          `json:"statement"`
	Assumptions string          `json:"assumptions"`
	FormalGoal  *leancheck.Goal `json:"formal_goal,omitempty"`
}
type DecompositionReport struct {
	Summary  string          `json:"summary"`
	Claims   []ProposedClaim `json:"claims"`
	Coverage ProposedClaim   `json:"coverage"`
}
type Decomposition struct {
	ID             string              `json:"id"`
	Target         string              `json:"target"`
	TargetRevision int                 `json:"target_revision"`
	Attempt        string              `json:"attempt"`
	ResultSHA256   string              `json:"result_sha256"`
	Report         DecompositionReport `json:"report"`
	Status         string              `json:"status"`
	Children       []string            `json:"children,omitempty"`
	CreatedAt      time.Time           `json:"created_at"`
}
type ProposalRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Attempt          string `json:"attempt"`
	Confirm          bool   `json:"confirm,omitempty"`
}

func (s *Service) ImportDecomposition(r ProposalRequest) error {
	if r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) {
		return RuleError("Нужны снимок и идентификатор команды.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Записано предложение разбиения", r.Attempt, "operator", func(d *Data) error {
		a := d.attempt(r.Attempt)
		if a == nil || a.Status != "candidate" || d.task(a.TaskID) == nil || d.task(a.TaskID).Kind != "decompose" {
			return RuleError("Нужна завершенная попытка предложения разбиения.")
		}
		e := d.entity(a.Target)
		if e == nil || e.Revision != a.TargetRevision || e.Status == "accepted" || e.Status == "refuted" {
			return RuleError("Нужна текущая открытая версия.")
		}
		result, err := s.readResult(*a)
		if err != nil {
			return err
		}
		if !s.safeResultContent(*a, result) {
			return RuleError("Небезопасный формат результата.")
		}
		var report DecompositionReport
		if decodeAgentReport(result.Candidate, &report) != nil || !textOK(report.Summary, 4000) || len(report.Claims) < 1 || len(report.Claims) > 6 {
			return RuleError("Нужны пояснение, от одной до шести лемм и обязательство покрытия.")
		}
		for _, c := range append(append([]ProposedClaim{}, report.Claims...), report.Coverage) {
			if !textOK(c.Title, 300) || !textOK(c.Statement, 16000) || !textOK(c.Assumptions, 8000) {
				return RuleError("Нужны название, утверждение и предпосылки каждой части.")
			}
			if c.FormalGoal != nil {
				if err := c.FormalGoal.Validate(); err != nil {
					return err
				}
			}
		}
		for i, c := range report.Claims {
			seen := map[int]bool{}
			for _, dep := range c.DependsOn {
				if dep < 0 || dep >= i || seen[dep] {
					return RuleError("Часть может зависеть только от предыдущих частей без повторов.")
				}
				seen[dep] = true
			}
		}
		d.Proposals = append(d.Proposals, Decomposition{ID: identifier("proposal"), Target: e.ID, TargetRevision: e.Revision, Attempt: a.ID, ResultSHA256: a.ResultSHA256, Report: report, Status: "proposed", CreatedAt: time.Now().UTC()})
		return nil
	})
}
func (s *Service) ApplyDecomposition(id string, r ProposalRequest) error {
	if !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) {
		return RuleError("Подтвердите утверждения, предпосылки и формальные цели разбиения.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(struct {
		ID string
		ProposalRequest
	}{id, r}), "Подтверждено разбиение", id, "operator", func(d *Data) error {
		var p *Decomposition
		for i := range d.Proposals {
			if d.Proposals[i].ID == id {
				p = &d.Proposals[i]
				break
			}
		}
		if p == nil || p.Status != "proposed" {
			return RuleError("Предложение недоступно.")
		}
		e := d.entity(p.Target)
		a := d.attempt(p.Attempt)
		if e == nil || e.Revision != p.TargetRevision || e.Status == "accepted" || e.Status == "refuted" || a == nil || a.ResultSHA256 != p.ResultSHA256 {
			return RuleError("Предложение устарело.")
		}
		result, err := s.readResult(*a)
		if err != nil {
			return err
		}
		if !s.safeResultContent(*a, result) {
			return RuleError("Некорректный источник предложения.")
		}
		study := e.Study
		ids := []string{}
		for _, c := range p.Report.Claims {
			id := identifier("L")
			child := newEntity(id, c.Title, "lemma", study, c.Statement, c.Assumptions)
			child.FormalGoal = c.FormalGoal
			child.Author = "executor:" + a.Profile
			for _, dep := range c.DependsOn {
				child.Dependencies = append(child.Dependencies, ids[dep])
				d.WorkLinks = append(d.WorkLinks, [2]string{id, ids[dep]})
			}
			d.Entities = append(d.Entities, child)
			ids = append(ids, id)
		}
		c := p.Report.Coverage
		cover := newEntity(identifier("O"), c.Title, "obligation", study, c.Statement, c.Assumptions)
		cover.FormalGoal = c.FormalGoal
		cover.Dependencies = append([]string{}, ids...)
		cover.Author = "executor:" + a.Profile
		d.Entities = append(d.Entities, cover)
		e = d.entity(p.Target)
		e.Dependencies = append(e.Dependencies, cover.ID)
		e.Revision++
		e.Status = "open"
		e.Proof = ""
		e.ProofAuthor = ""
		e.ProofAttempt = ""
		e.ProofVerification = ""
		e.ResearchResult = ""
		e.DependencyRevisions = nil
		for _, id := range ids {
			d.WorkLinks = append(d.WorkLinks, [2]string{cover.ID, id})
		}
		d.WorkLinks = append(d.WorkLinks, [2]string{e.ID, cover.ID})
		p.Children = append(ids, cover.ID)
		p.Status = "applied"
		return nil
	})
}
