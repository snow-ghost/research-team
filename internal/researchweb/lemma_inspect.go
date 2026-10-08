package researchweb

import (
	"context"
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

type InspectLemmaRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Library          string `json:"library"`
	Domain           string `json:"domain"`
}

func (s *Service) InspectLemma(ctx context.Context, r InspectLemmaRequest) error {
	if s.Options.Lean == nil || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || !textOK(r.Domain, 200) {
		return RuleError("Нужны закрепленная среда Lean, область и снимок.")
	}
	v, err := s.Store.Read()
	if err != nil {
		return err
	}
	if v.Revision != r.ExpectedRevision {
		return ErrConflict
	}
	s.mu.Lock()
	if v.Maintenance != nil || len(s.running) >= s.Options.Config.MaxParallel || s.ctx.Err() != nil {
		s.mu.Unlock()
		return RuleError("Нативная проверка недоступна во время обслуживания или занятых проверок.")
	}
	job := "inspect:" + identifier("job")
	runContext, cancel := context.WithCancel(s.ctx)
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
	ctx = runContext
	for _, l := range v.Library {
		if l.ID != r.Library || l.Status != "ready" || l.Report == nil || !libraryMatches(&v.Data, l) {
			continue
		}
		env, _ := s.libraryEnvironment(&v.Data, v.entity(l.Lemma))
		dir := filepath.Join(s.Options.Config.DataDir, "inspections", identifier("inspect"))
		report, inspectErr := (leancheck.DockerChecker{Config: *env}).InspectModule(ctx, l.Goal, filepath.Join(s.Options.Config.DataDir, "library", l.ID, "checked"), *l.Report, dir)
		body, _ := json.Marshal(report)
		if err := atomicFile(filepath.Join(dir, "report.json"), body); err != nil {
			return err
		}
		if inspectErr != nil {
			return RuleError("Извлечение типов Lean не завершено; отчет сохранен.")
		}
		return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Извлечена структура леммы средствами Lean", l.ID, "checker", func(d *Data) error {
			if !libraryMatches(d, l) || report.GoalSHA256 != leancheck.Digest(l.Goal) || report.ArtifactSHA256 != l.Report.ArtifactSHA256 {
				return ErrConflict
			}
			sig := LemmaSignature{ID: identifier("signature"), Library: l.ID, GoalSHA256: report.GoalSHA256, Domain: r.Domain, Conclusion: report.Conclusion, Native: &report, CreatedAt: time.Now().UTC()}
			for _, binder := range report.Binders {
				if binder.Kind == "premise" {
					sig.Conditions = append(sig.Conditions, binder.Type)
				} else {
					sig.Parameters = append(sig.Parameters, LemmaParameter{Name: binder.Name, Type: binder.Type})
				}
			}
			d.LemmaSignatures = append(d.LemmaSignatures, sig)
			return nil
		})
	}
	return RuleError("Нужна действующая собранная лемма.")
}
