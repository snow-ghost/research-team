package researchweb

import (
	"time"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

type TacticRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	MaxTrials        int    `json:"max_trials"`
	Confirm          bool   `json:"confirm"`
}

func (s *Service) QueueTactics(target string, r TacticRequest) error {
	if s.Options.Checker == nil || !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || r.MaxTrials < 1 || r.MaxTrials > 5 {
		return RuleError("Подтвердите от одной до пяти местных проверок Lean.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(struct {
		Target string
		TacticRequest
	}{target, r}), "Назначены отдельные приемы Lean", target, "operator", func(d *Data) error {
		e := d.entity(target)
		if d.Maintenance != nil || e == nil || e.FormalGoal == nil || e.Status == "accepted" || e.Status == "refuted" {
			return RuleError("Нужна открытая формальная цель вне обслуживания.")
		}
		for _, v := range d.Verifications {
			if v.Target == target && (v.Status == "queued" || v.Status == "running") {
				return RuleError("У цели уже есть ожидающие проверки Lean.")
			}
		}
		trials, err := leancheck.TacticTrials(*e.FormalGoal)
		if err != nil {
			return err
		}
		env, pins := s.libraryEnvironment(d, e)
		if env != nil && env.TimeoutSeconds > 60 {
			env.TimeoutSeconds = 60
		}
		deps := map[string]int{}
		for _, dep := range e.Dependencies {
			x := d.entity(dep)
			if x == nil {
				return RuleError("Основание не найдено.")
			}
			deps[dep] = x.Revision
		}
		for _, trial := range trials[:r.MaxTrials] {
			d.Verifications = append(d.Verifications, Verification{ID: identifier("verify"), Target: e.ID, TargetRevision: e.Revision, SubmittedRevision: e.Revision, Origin: "submitted", Author: "procedure:" + leancheck.PortfolioVersion, Basis: "Отдельный прием " + trial.ID + "; процедура " + hash(trials), Goal: *e.FormalGoal, Source: trial.Source, Environment: env, LibraryPins: pins, DependencyRevisions: deps, Status: "queued", CreatedAt: time.Now().UTC()})
		}
		return nil
	})
}
