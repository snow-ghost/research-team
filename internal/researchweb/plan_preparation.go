package researchweb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

type PlanPreparation struct {
	ID           string                     `json:"id"`
	Status       string                     `json:"status"`
	ReportSHA256 string                     `json:"report_sha256"`
	GoalsSHA256  string                     `json:"goals_sha256"`
	Report       *leancheck.GoalPreparation `json:"report,omitempty"`
	Diagnostics  string                     `json:"diagnostics,omitempty"`
}
type PreparedPlanDefinitions struct {
	Proposal string                    `json:"proposal"`
	CheckID  string                    `json:"check_id"`
	Report   leancheck.GoalPreparation `json:"report"`
}
type PlanRevisionRequest struct {
	ExpectedRevision int                 `json:"expected_revision"`
	RequestID        string              `json:"request_id"`
	Report           DecompositionReport `json:"report"`
	Note             string              `json:"note"`
	Confirm          bool                `json:"confirm"`
}

func (d *Data) proposal(id string) *Decomposition {
	for i := range d.Proposals {
		if d.Proposals[i].ID == id {
			return &d.Proposals[i]
		}
	}
	return nil
}
func planGoals(d *Data, p *Decomposition) ([]leancheck.Goal, error) {
	e := d.entity(p.Target)
	if e == nil || e.FormalGoal == nil || e.Revision != p.TargetRevision {
		return nil, RuleError("Цель плана изменилась.")
	}
	goals := []leancheck.Goal{*e.FormalGoal}
	for _, c := range append(append([]ProposedClaim{}, p.Report.Claims...), p.Report.Coverage) {
		if c.FormalGoal == nil || c.FormalGoal.Validate() != nil {
			return nil, RuleError("Всем частям и покрытию нужны формальные цели.")
		}
		goals = append(goals, *c.FormalGoal)
	}
	return goals, nil
}
func (s *Service) RequestPlanPreparation(id string, r WorkflowCommand) error {
	if s.Options.Preparer == nil || !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || !textOK(r.Note, 4000) {
		return RuleError("Подтвердите местную проверку целей и укажите основание.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(struct {
		ID      string
		Request WorkflowCommand
	}{id, r}), "Назначена проверка целей и общих определений", id, "operator", func(d *Data) error {
		p := d.proposal(id)
		if p == nil || p.Status != "proposed" || d.Maintenance != nil {
			return RuleError("Нет действующего предложения вне обслуживания.")
		}
		if p.Preparation != nil && (p.Preparation.Status == "queued" || p.Preparation.Status == "running" || p.Preparation.Status == "ready") {
			return RuleError("Проверка уже назначена или готова.")
		}
		goals, err := planGoals(d, p)
		if err != nil {
			return err
		}
		p.Preparation = &PlanPreparation{ID: identifier("preparation"), Status: "queued", ReportSHA256: hash(p.Report), GoalsSHA256: leancheck.Digest(goals)}
		return nil
	})
}
func (s *Service) RevisePlan(id string, r PlanRevisionRequest) error {
	if !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || !textOK(r.Note, 4000) || !textOK(r.Report.Summary, 4000) || len(r.Report.Claims) < 1 || len(r.Report.Claims) > 6 || validateStrategies(r.Report) != nil {
		return RuleError("Подтвердите исправленный план и основание изменения.")
	}
	for i, c := range append(append([]ProposedClaim{}, r.Report.Claims...), r.Report.Coverage) {
		if !textOK(c.Title, 300) || !textOK(c.Statement, 16000) || !textOK(c.Assumptions, 8000) || c.FormalGoal == nil || c.FormalGoal.Validate() != nil {
			return RuleError("Нужны полные постановки исправленного плана.")
		}
		seen := map[int]bool{}
		for _, dep := range c.DependsOn {
			if dep < 0 || dep >= i || seen[dep] {
				return RuleError("Некорректные зависимости исправленного плана.")
			}
			seen[dep] = true
		}
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(struct {
		ID      string
		Request PlanRevisionRequest
	}{id, r}), "Сохранена отдельная редакция плана", id, "operator", func(d *Data) error {
		p := d.proposal(id)
		if p == nil || p.Status != "proposed" || p.Preparation != nil && (p.Preparation.Status == "queued" || p.Preparation.Status == "running") {
			return RuleError("Сначала завершите проверку действующего предложения.")
		}
		if _, err := planGoals(d, p); err != nil {
			return err
		}
		next := *p
		next.ID = identifier("proposal")
		next.Parent, next.Author, next.Note = id, "operator", r.Note
		next.Report = r.Report
		next.Preparation = nil
		next.CreatedAt = time.Now().UTC()
		if st := d.study(d.entity(p.Target).Study); st != nil && st.Workflow != nil && st.Workflow.Proposal == id {
			if st.Workflow.Status != "awaiting_plan" {
				return RuleError("Полный цикл не ожидает редакции плана.")
			}
			next.Workflow = st.Workflow.ID
			st.Workflow.Proposal = next.ID
		}
		p.Status = "superseded"
		d.Proposals = append(d.Proposals, next)
		return nil
	})
}
func (s *Service) planPreparationMatches(d *Data, p *Decomposition) error {
	goals, err := planGoals(d, p)
	if err != nil {
		return err
	}
	c := p.Preparation
	if c == nil || c.Status != "ready" || c.Report == nil || c.ReportSHA256 != hash(p.Report) || c.GoalsSHA256 != leancheck.Digest(goals) || c.Report.Status != "ready" || c.Report.GoalsSHA256 != c.GoalsSHA256 {
		return RuleError("Сначала проверьте компиляцию всех целей и совместные импорты актуальной редакции.")
	}
	if s.Options.Lean != nil {
		if c.Report.EnvironmentSHA256 != leancheck.Digest(*s.Options.Lean) {
			return RuleError("Среда подготовки изменилась.")
		}
		if err := leancheck.VerifyPreparedDefinitions(filepath.Join(s.Options.Config.DataDir, "preparations", c.ID), *c.Report, goals); err != nil {
			return RuleError("Артефакты общих определений недействительны: " + err.Error())
		}
	}
	return nil
}
func (s *Service) startPlanPreparations() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Options.Preparer == nil || len(s.running) >= s.Options.Config.MaxParallel {
		return
	}
	v, err := s.Store.Read()
	if err != nil || v.Maintenance != nil {
		return
	}
	for _, p := range v.Proposals {
		if p.Preparation == nil || p.Preparation.Status != "queued" {
			continue
		}
		goals, err := planGoals(&v.Data, &p)
		if err != nil {
			_ = s.Store.Change(0, "", "", "Проверка плана устарела", p.ID, "checker", func(d *Data) error {
				current := d.proposal(p.ID)
				if current != nil && current.Preparation != nil && current.Preparation.Status == "queued" {
					current.Preparation.Status = "stale"
				}
				return nil
			})
			continue
		}
		check := *p.Preparation
		if err := s.Store.Change(0, "", "", "Начата проверка целей плана", p.ID, "checker", func(d *Data) error {
			current := d.proposal(p.ID)
			if current == nil || current.Preparation == nil || current.Preparation.ID != check.ID || current.Preparation.Status != "queued" {
				return ErrConflict
			}
			current.Preparation.Status = "running"
			return nil
		}); err != nil {
			continue
		}
		ctx, cancel := context.WithCancel(s.ctx)
		s.running["preparation:"+check.ID] = cancel
		s.wg.Add(1)
		go s.preparePlan(ctx, p, goals)
		return
	}
}
func (s *Service) preparePlan(ctx context.Context, p Decomposition, goals []leancheck.Goal) {
	defer s.wg.Done()
	check := p.Preparation
	dir := filepath.Join(s.Options.Config.DataDir, "preparations", check.ID)
	var report leancheck.GoalPreparation
	err := os.MkdirAll(dir, 0700)
	if err == nil {
		report, err = s.Options.Preparer.PrepareGoals(ctx, goals, dir)
	}
	status := report.Status
	diagnostics := report.Diagnostics
	if err != nil || status != "ready" {
		status = "failed"
		if diagnostics == "" && err != nil {
			diagnostics = err.Error()
		}
	}
	if report.GoalsSHA256 != check.GoalsSHA256 || report.Version != leancheck.PreparationVersion {
		status = "failed"
		diagnostics = "Preparation report binding mismatch"
	}
	if ctx.Err() != nil {
		status = "interrupted"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel := s.running["preparation:"+check.ID]; cancel != nil {
		cancel()
	}
	delete(s.running, "preparation:"+check.ID)
	_ = s.Store.Change(0, "", "", "Завершена проверка целей плана", p.ID, "checker", func(d *Data) error {
		current := d.proposal(p.ID)
		if current == nil || current.Preparation == nil || current.Preparation.ID != check.ID {
			return ErrConflict
		}
		c := current.Preparation
		if c.Status == "interrupted" {
			status = "interrupted"
		}
		c.Status, c.Diagnostics, c.Report = status, diagnostics, &report
		if current.TargetRevision != p.TargetRevision || hash(current.Report) != check.ReportSHA256 {
			c.Status = "stale"
		}
		return nil
	})
}
func (s *Service) verifyDefinitions(d *Data, target string) error {
	seenStudies, seenEntities := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if seenEntities[id] {
			return nil
		}
		seenEntities[id] = true
		e := d.entity(id)
		if e == nil {
			return nil
		}
		if !seenStudies[e.Study] {
			seenStudies[e.Study] = true
			if err := s.verifyStudyDefinitions(d, e.Study); err != nil {
				return err
			}
		}
		for _, dep := range e.Dependencies {
			if err := visit(dep); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(target)
}

func (s *Service) verifyStudyDefinitions(d *Data, study string) error {
	st := d.study(study)
	if st == nil || st.Definitions == nil {
		return nil
	}
	defs := st.Definitions
	goals := []leancheck.Goal{}
	for _, m := range defs.Report.Definitions {
		goals = append(goals, m.Goal)
	}
	if s.Options.Lean == nil || defs.Report.EnvironmentSHA256 != leancheck.Digest(*s.Options.Lean) {
		return RuleError("Среда общих определений изменилась.")
	}
	if err := leancheck.VerifyPreparedDefinitions(filepath.Join(s.Options.Config.DataDir, "preparations", defs.CheckID), defs.Report, goals); err != nil {
		return fmt.Errorf("definition artifacts: %w", err)
	}
	return nil
}
