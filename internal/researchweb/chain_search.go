package researchweb

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

type ChainSearchRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Target           string `json:"target"`
	Query            string `json:"query,omitempty"`
	Limit            int    `json:"limit"`
	Depth            int    `json:"depth"`
	MaxTrials        int    `json:"max_trials"`
	TimeoutSeconds   int    `json:"timeout_seconds"`
	Confirm          bool   `json:"confirm"`
}

func chainLibraryPins(d *Data, ids []string) (map[string]string, bool) {
	pins := map[string]string{}
	if len(ids) < 1 || len(ids) > 8 {
		return pins, false
	}
	for _, id := range ids {
		if _, seen := pins[id]; seen {
			return pins, false
		}
		for _, l := range d.Library {
			if l.ID == id && l.Status == "ready" && l.Report != nil && l.Report.AuditSHA256 == leancheck.AuditDigest() && libraryMatches(d, l) {
				pins[id] = l.Report.ArtifactSHA256
			}
		}
		if _, ok := pins[id]; !ok {
			return pins, false
		}
	}
	return pins, true
}

func (s *Service) SearchLemmaChains(ctx context.Context, r ChainSearchRequest) (CatalogSearchResult, error) {
	out := CatalogSearchResult{Status: "complete", Matches: []NativeLemmaMatch{}}
	if !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || r.Limit < 1 || r.Limit > 8 || r.Depth < 1 || r.Depth > 3 || r.MaxTrials < 1 || r.MaxTrials > 32 || r.TimeoutSeconds < 1 || r.TimeoutSeconds > 120 || len(r.Query) > 200 || s.Options.Lean == nil {
		return out, ErrLimit
	}
	v, err := s.Store.Read()
	if err != nil {
		return out, err
	}
	group := hash([]any{"chain-search", r})
	for _, m := range v.LemmaMatches {
		if m.CatalogRequest == group {
			if m.Chain == nil || !matchCurrent(&v.Data, m) {
				return out, ErrConflict
			}
			out.Matches = append(out.Matches, m)
			out.Revision = v.Revision
			out.Status = "recorded"
			return out, nil
		}
	}
	if v.Revision != r.ExpectedRevision {
		return out, ErrConflict
	}
	e := v.entity(r.Target)
	if e == nil || e.FormalGoal == nil || e.Status == "accepted" || e.Status == "refuted" || studyClosed(&v.Data, e.Study) || studySearchSuspended(&v.Data, e.Study) {
		return out, RuleError("Нужна открытая формальная цель.")
	}
	ids := catalogCandidates(&v.Data, r.Target, r.Query)
	out.Candidates = len(ids)
	if len(ids) == 0 {
		out.Revision = v.Revision
		return out, nil
	}
	ids = ids[:min(len(ids), r.Limit)]
	pins, ok := chainLibraryPins(&v.Data, ids)
	if !ok {
		return out, ErrConflict
	}
	env, _ := s.libraryEnvironment(&v.Data, e)
	goal := *e.FormalGoal
	candidates := []string{}
	for _, id := range ids {
		for _, l := range v.Library {
			if l.ID == id {
				dir := filepath.Join(s.Options.Config.DataDir, "library", l.ID, "checked")
				if err := leancheck.VerifyModuleArtifacts(dir, *l.Report); err != nil {
					return out, err
				}
				env.Libraries = append(env.Libraries, dir)
				if !strings.Contains(goal.Source, "import "+l.Module+"\n") {
					goal.Source = "import " + l.Module + "\n" + goal.Source
				}
				candidates = append(candidates, l.Goal.Candidate)
			}
		}
	}
	s.mu.Lock()
	if v.Maintenance != nil || len(s.running) >= s.Options.Config.MaxParallel || s.ctx.Err() != nil {
		s.mu.Unlock()
		return out, RuleError("Нативная проверка временно недоступна.")
	}
	id := identifier("match")
	job := "chain:" + id
	run, cancel := context.WithTimeout(s.ctx, time.Duration(r.TimeoutSeconds)*time.Second)
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
	checker := s.Options.ChainChecker
	if checker == nil {
		checker = leancheck.DockerChecker{Config: *env}
	}
	dir := filepath.Join(s.Options.Config.DataDir, "inspections", id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return out, err
	}
	chain, searchErr := leancheck.SearchChains(run, checker, goal, candidates, r.Depth, r.MaxTrials, dir)
	report := leancheck.MatchReport{Status: chain.Status, Bindings: []leancheck.Argument{}, Missing: []leancheck.MissingArgument{}, Source: chain.Source, Verification: chain.Verification, GoalSHA256: leancheck.Digest(goal), ArtifactSHA256: hash(pins), EnvironmentSHA256: leancheck.Digest(*env), MatcherSHA256: leancheck.ChainDigest()}
	if searchErr != nil {
		report.Status = "failed"
		report.Diagnostics = "Поиск прерван или проверка среды не завершена; подробности сохранены в попытках Lean."
	}
	m := NativeLemmaMatch{ID: id, CatalogRequest: group, Target: e.ID, TargetRevision: e.Revision, TargetGoalSHA256: leancheck.Digest(*e.FormalGoal), Library: ids[0], Libraries: ids, Goal: goal, Report: report, Chain: &chain, CreatedAt: time.Now().UTC()}
	body, _ := json.Marshal(m)
	if err := atomicFile(filepath.Join(dir, "report.json"), body); err != nil {
		return out, err
	}
	err = s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Выполнен ограниченный поиск цепочки лемм", e.ID, "checker", func(d *Data) error {
		if !matchCurrent(d, m) {
			return ErrConflict
		}
		d.LemmaMatches = append(d.LemmaMatches, m)
		return nil
	})
	if err != nil {
		return out, err
	}
	current, err := s.Store.Read()
	if err != nil {
		return out, err
	}
	out.Revision, out.Status, out.Matches = current.Revision, report.Status, []NativeLemmaMatch{m}
	return out, nil
}
