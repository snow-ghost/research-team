package researchweb

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

type RepresentationRequest struct {
	ExpectedRevision int            `json:"expected_revision"`
	RequestID        string         `json:"request_id"`
	Target           string         `json:"target"`
	Library          string         `json:"library"`
	Direct           leancheck.Goal `json:"direct"`
	Bridge           leancheck.Goal `json:"bridge"`
	BridgeSource     string         `json:"bridge_source"`
	BridgeAuthor     string         `json:"bridge_author"`
	Depth            int            `json:"depth"`
	TimeoutSeconds   int            `json:"timeout_seconds"`
	Confirm          bool           `json:"confirm"`
}

func (s *Service) CompareRepresentation(ctx context.Context, r RepresentationRequest) (NativeLemmaMatch, error) {
	var out NativeLemmaMatch
	if !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || s.Options.Lean == nil || r.Depth < 1 || r.Depth > 6 || r.TimeoutSeconds < 1 || r.TimeoutSeconds > 120 || !textOK(r.BridgeAuthor, 300) || len(r.BridgeSource) > 64000 {
		return out, RuleError("Нужны подтвержденные цели, автор эквивалентности и пределы проверки.")
	}
	v, err := s.Store.Read()
	if err != nil {
		return out, err
	}
	group := hash([]any{"representation", r})
	for _, m := range v.LemmaMatches {
		if m.CatalogRequest == group {
			if !matchCurrent(&v.Data, m) {
				return out, ErrConflict
			}
			return m, nil
		}
	}
	if v.Revision != r.ExpectedRevision {
		return out, ErrConflict
	}
	e := v.entity(r.Target)
	if e == nil || e.FormalGoal == nil || e.Status == "accepted" || e.Status == "refuted" || studyClosed(&v.Data, e.Study) || e.FormalGoal.Source != r.Direct.Source || e.FormalGoal.Source != r.Bridge.Source {
		return out, RuleError("Нужны открытая цель и определения в одном закрепленном исходнике.")
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
		return out, RuleError("Нужна действующая принятая лемма.")
	}
	env, pins := s.libraryEnvironment(&v.Data, e)
	artifact := filepath.Join(s.Options.Config.DataDir, "library", library.ID, "checked")
	if err := leancheck.VerifyModuleArtifacts(artifact, *library.Report); err != nil {
		return out, err
	}
	if _, ok := pins[library.ID]; !ok {
		env.Libraries = append(env.Libraries, artifact)
	}
	s.mu.Lock()
	if v.Maintenance != nil || len(s.running) >= s.Options.Config.MaxParallel || s.ctx.Err() != nil {
		s.mu.Unlock()
		return out, RuleError("Местная проверка временно недоступна.")
	}
	id := identifier("match")
	job := "representation:" + id
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
	report, err := leancheck.CompareRepresentation(run, checker, *e.FormalGoal, r.Direct, r.Bridge, r.BridgeSource, r.BridgeAuthor, library.Goal.Candidate, r.Depth, r.TimeoutSeconds, dir)
	if err != nil {
		return out, err
	}
	status := "obligations"
	source := ""
	var checked *leancheck.Report
	if report.Transferred != nil {
		source = report.Transferred.Source
		checked = &report.Transferred.Report
		if report.Transferred.Status == "verified" {
			status = "verified"
		} else {
			status = "candidate_failed"
		}
	}
	out = NativeLemmaMatch{ID: id, CatalogRequest: group, Target: e.ID, TargetRevision: e.Revision, TargetGoalSHA256: leancheck.Digest(*e.FormalGoal), Library: library.ID, Goal: *e.FormalGoal, Representation: &report, CreatedAt: time.Now().UTC(), Report: leancheck.MatchReport{Status: status, Source: source, Verification: checked, Bindings: []leancheck.Argument{}, Missing: []leancheck.MissingArgument{}, GoalSHA256: leancheck.Digest(*e.FormalGoal), ArtifactSHA256: library.Report.ArtifactSHA256, EnvironmentSHA256: leancheck.Digest(*env), MatcherSHA256: leancheck.RepresentationDigest()}}
	if status == "obligations" {
		out.Report.Status = "no_match"
	}
	body, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return out, err
	}
	if err := atomicFile(filepath.Join(dir, "report.json"), body); err != nil {
		return out, err
	}
	if err := s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Проверена смена представления цели", e.ID, "checker", func(d *Data) error {
		if !matchCurrent(d, out) {
			return ErrConflict
		}
		d.LemmaMatches = append(d.LemmaMatches, out)
		return nil
	}); err != nil {
		return out, err
	}
	return out, nil
}
