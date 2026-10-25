package researchweb

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

type ReuseTrialRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Target           string `json:"target"`
	Library          string `json:"library"`
	Depth            int    `json:"depth"`
	TimeoutSeconds   int    `json:"timeout_seconds"`
	Confirm          bool   `json:"confirm"`
}

func (s *Service) CompareLemmaReuse(ctx context.Context, r ReuseTrialRequest) (NativeLemmaMatch, error) {
	var out NativeLemmaMatch
	if !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || r.Depth < 1 || r.Depth > 6 || r.TimeoutSeconds < 1 || r.TimeoutSeconds > 120 || s.Options.Lean == nil {
		return out, RuleError("Нужны новая цель, принятый модуль и ограничение двух местных проверок.")
	}
	v, err := s.Store.Read()
	if err != nil {
		return out, err
	}
	group := hash([]any{"reuse-trial", r})
	for _, m := range v.LemmaMatches {
		if m.CatalogRequest == group {
			if m.Reuse == nil || !matchCurrent(&v.Data, m) {
				return out, ErrConflict
			}
			return m, nil
		}
	}
	if v.Revision != r.ExpectedRevision {
		return out, ErrConflict
	}
	e := v.entity(r.Target)
	if e == nil || e.FormalGoal == nil || e.Status == "accepted" || e.Status == "refuted" || studyClosed(&v.Data, e.Study) {
		return out, RuleError("Нужна открытая формальная цель другого утверждения.")
	}
	var library *LibraryEntry
	for i := range v.Library {
		l := &v.Library[i]
		if l.ID == r.Library && l.Lemma != e.ID && l.Status == "ready" && l.Report != nil && l.Report.AuditSHA256 == leancheck.AuditDigest() && libraryMatches(&v.Data, *l) {
			library = l
			break
		}
	}
	if library == nil {
		return out, RuleError("Нужен действующий принятый модуль.")
	}
	if leancheck.Digest(*e.FormalGoal) == leancheck.Digest(library.Goal) {
		return out, RuleError("Испытание переноса требует другой формальной постановки.")
	}
	artifact := filepath.Join(s.Options.Config.DataDir, "library", library.ID, "checked")
	if err := leancheck.VerifyModuleArtifacts(artifact, *library.Report); err != nil {
		return out, err
	}
	env, _ := s.libraryEnvironment(&v.Data, e)
	env.Libraries = append(env.Libraries, artifact)
	s.mu.Lock()
	if v.Maintenance != nil || len(s.running) >= s.Options.Config.MaxParallel || s.ctx.Err() != nil {
		s.mu.Unlock()
		return out, RuleError("Местная проверка временно недоступна.")
	}
	id := identifier("match")
	job := "reuse:" + id
	run, cancel := context.WithCancel(s.ctx)
	stop := context.AfterFunc(ctx, cancel)
	s.running[job] = cancel
	s.wg.Add(1)
	s.mu.Unlock()
	defer func() {
		stop()
		cancel()
		s.mu.Lock()
		delete(s.running, job)
		s.mu.Unlock()
		s.wg.Done()
	}()
	dir := filepath.Join(s.Options.Config.DataDir, "inspections", id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return out, err
	}
	checker := s.Options.ChainChecker
	if checker == nil {
		checker = leancheck.DockerChecker{Config: *env}
	}
	report, err := leancheck.CompareReuse(run, checker, *e.FormalGoal, library.Module, library.Goal.Candidate, r.Depth, r.TimeoutSeconds, dir)
	if err != nil {
		return out, err
	}
	with := report.With
	status := with.Status
	if status == "not_proved" {
		status = "no_match"
	}
	out = NativeLemmaMatch{ID: id, CatalogRequest: group, Target: e.ID, TargetRevision: e.Revision, TargetGoalSHA256: leancheck.Digest(*e.FormalGoal), Library: library.ID, Goal: with.Goal, Reuse: &report, CreatedAt: time.Now().UTC(), Report: leancheck.MatchReport{Bindings: []leancheck.Argument{}, Missing: []leancheck.MissingArgument{}, Status: status, Source: with.Source, Verification: &with.Report, GoalSHA256: leancheck.Digest(with.Goal), ArtifactSHA256: library.Report.ArtifactSHA256, EnvironmentSHA256: leancheck.Digest(*env), MatcherSHA256: leancheck.ReuseDigest()}}
	body, _ := json.MarshalIndent(out, "", "  ")
	if err := atomicFile(filepath.Join(dir, "report.json"), body); err != nil {
		return out, err
	}
	err = s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Сравнено повторное использование принятой леммы", e.ID, "checker", func(d *Data) error {
		if !matchCurrent(d, out) {
			return ErrConflict
		}
		d.LemmaMatches = append(d.LemmaMatches, out)
		return nil
	})
	return out, err
}
