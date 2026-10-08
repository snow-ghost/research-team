package researchweb

import (
	"context"
	"github.com/snow-ghost/research-team/internal/leancheck"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type LibraryEntry struct {
	ID            string                  `json:"id"`
	Lemma         string                  `json:"lemma"`
	LemmaRevision int                     `json:"lemma_revision"`
	Result        string                  `json:"result"`
	Module        string                  `json:"module"`
	Source        string                  `json:"source"`
	Goal          leancheck.Goal          `json:"goal"`
	Candidate     string                  `json:"candidate"`
	Environment   leancheck.Config        `json:"environment"`
	Status        string                  `json:"status"`
	Report        *leancheck.ModuleReport `json:"report,omitempty"`
	CreatedAt     time.Time               `json:"created_at"`
}
type PublishRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Lemma            string `json:"lemma"`
}

func libraryMatches(d *Data, l LibraryEntry) bool {
	if l.Report != nil && l.Report.Status == "ready" && len(l.Report.Artifacts) == 0 {
		return false
	}
	e := d.entity(l.Lemma)
	if e == nil || e.Revision != l.LemmaRevision || d.effective(e.ID, map[string]bool{}) != "accepted" || e.ResearchResult != l.Result {
		return false
	}
	for _, r := range d.Results {
		if r.ID == l.Result {
			return resultMatches(d, r)
		}
	}
	return false
}
func (s *Service) PublishLemma(r PublishRequest) error {
	if s.Options.Lean == nil {
		return RuleError("Для сборки библиотеки нужна закрепленная среда Lean.")
	}
	if r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) {
		return RuleError("Нужны снимок и идентификатор команды.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Лемма назначена для библиотеки", r.Lemma, "operator", func(d *Data) error {
		e := d.entity(r.Lemma)
		if e == nil || d.effective(e.ID, map[string]bool{}) != "accepted" || e.ResearchResult == "" || !hasVerifiedProof(d, e) {
			return RuleError("Нужно принятое утверждение со связанной записью проверок.")
		}
		for _, l := range d.Library {
			if l.Lemma == e.ID && l.LemmaRevision == e.Revision && l.Status != "failed" && l.Status != "stale" && l.Status != "interrupted" {
				return RuleError("Эта версия уже находится в библиотеке.")
			}
		}
		v := proofVerification(d, e)
		if v == nil || v.Report.AuditSHA256 != leancheck.AuditDigest() {
			return RuleError("Нужна новая проверка Lean актуальной программой аудита.")
		}
		source, err := leancheck.ModuleSource(v.Goal, v.Source)
		if err != nil {
			return err
		}
		id := identifier("library")
		env := *s.Options.Lean
		if v.Environment != nil {
			env = *v.Environment
		}
		d.Library = append(d.Library, LibraryEntry{ID: id, Lemma: e.ID, LemmaRevision: e.Revision, Result: e.ResearchResult, Module: "ResearchLemma_" + strings.TrimPrefix(id, "library-"), Source: source, Goal: v.Goal, Candidate: v.Source, Environment: env, Status: "queued", CreatedAt: time.Now().UTC()})
		return nil
	})
}
func (s *Service) startLibraryBuilds() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.running) >= s.Options.Config.MaxParallel {
		return
	}
	view, err := s.Store.Read()
	if err != nil || view.Maintenance != nil {
		return
	}
	for _, l := range view.Library {
		if l.Status != "queued" {
			continue
		}
		err = s.Store.Change(0, "", "", "Начата сборка леммы", l.ID, "checker", func(d *Data) error {
			for i := range d.Library {
				if d.Library[i].ID == l.ID && d.Library[i].Status == "queued" {
					d.Library[i].Status = "running"
					return nil
				}
			}
			return ErrConflict
		})
		if err != nil {
			continue
		}
		ctx, cancel := context.WithCancel(s.ctx)
		s.running["library:"+l.ID] = cancel
		s.wg.Add(1)
		go s.buildLibrary(ctx, l)
		break
	}
}
func (s *Service) buildLibrary(ctx context.Context, l LibraryEntry) {
	defer s.wg.Done()
	dir := filepath.Join(s.Options.Config.DataDir, "library", l.ID)
	err := os.MkdirAll(dir, 0700)
	var report leancheck.ModuleReport
	if err == nil {
		report, err = (leancheck.DockerChecker{Config: l.Environment}).BuildModule(ctx, l.Goal, l.Candidate, l.Module, dir)
	}
	if err != nil {
		report.Status = "failed"
	}
	if report.Status == "ready" && (report.Module != l.Module || report.SourceSHA256 != leancheck.Digest(l.Source) || report.EnvironmentSHA256 != leancheck.Digest(l.Environment)) {
		report.Status = "failed"
		report.Diagnostics = "Module report integrity mismatch"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel := s.running["library:"+l.ID]; cancel != nil {
		cancel()
	}
	delete(s.running, "library:"+l.ID)
	err = s.Store.Change(0, "", "", "Завершена сборка леммы", l.ID, "checker", func(d *Data) error {
		for i := range d.Library {
			if d.Library[i].ID == l.ID {
				if d.Library[i].Status == "interrupted" {
					report.Status = "interrupted"
				}
				d.Library[i].Report = &report
				d.Library[i].Status = report.Status
				return nil
			}
		}
		return ErrConflict
	})
	if err != nil {
		s.cancel()
	}
}
func (s *Service) libraryEnvironment(d *Data, e *Entity) (*leancheck.Config, map[string]string) {
	if s.Options.Lean == nil {
		return nil, nil
	}
	env := *s.Options.Lean
	env.Libraries = append([]string{}, env.Libraries...)
	pins := map[string]string{}
	seen := map[string]bool{}
	var visit func(string)
	visit = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		for _, l := range d.Library {
			if l.Lemma == id && l.Status == "ready" && l.Report != nil && libraryMatches(d, l) {
				env.Libraries = append(env.Libraries, filepath.Join(s.Options.Config.DataDir, "library", l.ID, "checked"))
				pins[l.ID] = l.Report.ArtifactSHA256
			}
		}
		if dep := d.entity(id); dep != nil {
			for _, id := range dep.Dependencies {
				visit(id)
			}
		}
	}
	for _, id := range e.Dependencies {
		visit(id)
	}
	return &env, pins
}
