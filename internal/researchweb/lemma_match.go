package researchweb

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

type NativeLemmaMatch struct {
	Libraries        []string               `json:"libraries,omitempty"`
	Chain            *leancheck.ChainReport `json:"chain,omitempty"`
	CatalogRequest   string                 `json:"catalog_request,omitempty"`
	ID               string                 `json:"id"`
	Target           string                 `json:"target"`
	TargetRevision   int                    `json:"target_revision"`
	TargetGoalSHA256 string                 `json:"target_goal_sha256"`
	Library          string                 `json:"library"`
	Goal             leancheck.Goal         `json:"goal"`
	Report           leancheck.MatchReport  `json:"report"`
	Application      string                 `json:"application,omitempty"`
	CreatedAt        time.Time              `json:"created_at"`
}

type MatchLemmaRequest struct {
	CatalogRequest   string `json:"catalog_request,omitempty"`
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Target           string `json:"target"`
	Library          string `json:"library"`
}

type ApplyMatchRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Confirm          bool   `json:"confirm"`
}

func matchCurrent(d *Data, m NativeLemmaMatch) bool {
	e := d.entity(m.Target)
	matcher := leancheck.MatcherDigest()
	if m.Chain != nil {
		matcher = leancheck.ChainDigest()
	}
	if e == nil || e.Revision != m.TargetRevision || e.FormalGoal == nil || leancheck.Digest(*e.FormalGoal) != m.TargetGoalSHA256 || m.Report.MatcherSHA256 != matcher || m.Report.GoalSHA256 != leancheck.Digest(m.Goal) {
		return false
	}
	if m.Chain != nil {
		pins, ok := chainLibraryPins(d, m.Libraries)
		return ok && m.Chain.ProcedureSHA256 == leancheck.ChainDigest() && hash(pins) == m.Report.ArtifactSHA256
	}
	for _, l := range d.Library {
		if l.ID == m.Library && l.Status == "ready" && l.Report != nil && l.Report.ArtifactSHA256 == m.Report.ArtifactSHA256 && libraryMatches(d, l) {
			return true
		}
	}
	return false
}

func (s *Service) MatchLemma(ctx context.Context, r MatchLemmaRequest) error {
	if r.CatalogRequest != "" && !requestPattern.MatchString(r.CatalogRequest) {
		return ErrLimit
	}
	if s.Options.Lean == nil || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) {
		return RuleError("Нужны закрепленная среда Lean и снимок.")
	}
	v, err := s.Store.Read()
	if err != nil {
		return err
	}
	if v.Revision != r.ExpectedRevision {
		return ErrConflict
	}
	e := v.entity(r.Target)
	if e == nil || e.FormalGoal == nil || e.Status == "accepted" || e.Status == "refuted" {
		return RuleError("Нужна открытая формальная цель.")
	}
	var library *LibraryEntry
	for i := range v.Library {
		l := &v.Library[i]
		if l.ID == r.Library && l.Status == "ready" && l.Report != nil && l.Report.AuditSHA256 == leancheck.AuditDigest() && l.Lemma != e.ID && libraryMatches(&v.Data, *l) {
			library = l
			break
		}
	}
	if library == nil {
		return RuleError("Нужен действующий проверенный модуль леммы.")
	}
	s.mu.Lock()
	if v.Maintenance != nil || len(s.running) >= s.Options.Config.MaxParallel || s.ctx.Err() != nil {
		s.mu.Unlock()
		return RuleError("Нативная проверка недоступна во время обслуживания или занятых проверок.")
	}
	id := identifier("match")
	job := "match:" + id
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
	env, pins := s.libraryEnvironment(&v.Data, e)
	if _, found := pins[library.ID]; !found {
		env.Libraries = append(env.Libraries, filepath.Join(s.Options.Config.DataDir, "library", library.ID, "checked"))
	}
	g := *e.FormalGoal
	if !strings.Contains(g.Source, "import "+library.Module+"\n") {
		g.Source = "import " + library.Module + "\n" + g.Source
	}
	dir := filepath.Join(s.Options.Config.DataDir, "inspections", id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	matcher := s.Options.Matcher
	if matcher == nil {
		matcher = leancheck.DockerChecker{Config: *env}
	}
	report, matchErr := matcher.MatchModule(run, g, filepath.Join(s.Options.Config.DataDir, "library", library.ID, "checked"), *library.Report, library.Goal.Candidate, dir)
	if matchErr != nil {
		report.Status = "failed"
		if report.Diagnostics == "" {
			report.Diagnostics = "Нативный подбор не завершен; исходные сведения проверки сохранены."
		}
	}
	body, _ := json.Marshal(report)
	if err := atomicFile(filepath.Join(dir, "report.json"), body); err != nil {
		return err
	}
	m := NativeLemmaMatch{CatalogRequest: r.CatalogRequest, ID: id, Target: e.ID, TargetRevision: e.Revision, TargetGoalSHA256: leancheck.Digest(*e.FormalGoal), Library: library.ID, Goal: g, Report: report, CreatedAt: time.Now().UTC()}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Подобрана подстановка средствами Lean", e.ID, "checker", func(d *Data) error {
		if !matchCurrent(d, m) {
			return ErrConflict
		}
		d.LemmaMatches = append(d.LemmaMatches, m)
		return nil
	})
}

func (s *Service) ApplyLemmaMatch(id string, r ApplyMatchRequest) error {
	if !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) {
		return RuleError("Подтвердите создание обязательств применения.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(struct {
		ID      string
		Request ApplyMatchRequest
	}{id, r}), "Созданы обязательства подобранной леммы", id, "operator", func(d *Data) error {
		index := -1
		for i := range d.LemmaMatches {
			if d.LemmaMatches[i].ID == id {
				index = i
				break
			}
		}
		if index < 0 {
			return RuleError("Подстановка не найдена.")
		}
		m := d.LemmaMatches[index]
		if m.Application != "" || !matchCurrent(d, m) || (m.Report.Status != "verified" && m.Report.Status != "obligations") {
			return RuleError("Подстановка устарела, уже применена или требует ручного уточнения.")
		}
		if m.Report.Status == "verified" && (m.Report.Verification == nil || m.Report.Verification.Status != "verified" || m.Report.Verification.AuditSHA256 != leancheck.AuditDigest() || m.Report.Verification.GoalSHA256 != leancheck.Digest(m.Goal) || m.Report.Verification.SourceSHA256 != leancheck.Digest(m.Report.Source)) {
			return RuleError("Отсутствует проверка подобранного терма ядром Lean.")
		}
		if len(m.Report.Missing) > 16 {
			return ErrLimit
		}
		for _, missing := range m.Report.Missing {
			if missing.Kind != "premise" || missing.ClosedType == "" || len(missing.ClosedType) > 16000 {
				return RuleError("Остались неопределенные параметры; сначала уточните представление цели.")
			}
		}
		var l LibraryEntry
		for _, entry := range d.Library {
			if entry.ID == m.Library {
				l = entry
			}
		}
		target := d.entity(m.Target)
		appID := identifier("A")
		app := newEntity(appID, "Перенос леммы: "+target.Title, "application", target.Study, target.Statement, target.Assumptions)
		app.FormalGoal = &m.Goal
		app.Dependencies = []string{l.Lemma}
		if m.Chain != nil {
			app.Dependencies = nil
			for _, id := range m.Libraries {
				for _, entry := range d.Library {
					if entry.ID == id {
						app.Dependencies = append(app.Dependencies, entry.Lemma)
					}
				}
			}
		}
		for i, missing := range m.Report.Missing {
			obID := identifier("O")
			name := fmt.Sprintf("ResearchApplicationPremise_%s_%d", strings.TrimPrefix(appID, "A-"), i)
			g := leancheck.Goal{Source: m.Goal.Source + "\ndef " + name + " : Prop := " + missing.ClosedType + "\n", Declaration: name, Candidate: name + "_candidate"}
			if err := g.Validate(); err != nil {
				return err
			}
			o := newEntity(obID, "Предпосылка применения: "+missing.Name, "obligation", target.Study, missing.ClosedType, "Условие в точном контексте закрепленной цели; результат подбора типов еще не является доказательством.")
			o.FormalGoal = &g
			o.Dependencies = []string{l.Lemma}
			d.Entities = append(d.Entities, o)
			app.Dependencies = append(app.Dependencies, obID)
			d.WorkLinks = append(d.WorkLinks, [2]string{appID, obID})
			d.addTask(obID, "Доказать недостающую предпосылку", "proof", missing.ClosedType)
		}
		d.Entities = append(d.Entities, app)
		target = d.entity(m.Target)
		target.Dependencies = append(target.Dependencies, appID)
		target.Revision++
		target.Proof = ""
		target.ProofAuthor = ""
		target.ProofAttempt = ""
		target.ProofVerification = ""
		target.ResearchResult = ""
		target.DependencyRevisions = nil
		target.Status = "open"
		d.Applications = append(d.Applications, Application{ID: appID, Lemma: l.Lemma, LemmaRevision: l.LemmaRevision, Target: target.ID, State: "candidate"})
		d.WorkLinks = append(d.WorkLinks, [2]string{target.ID, appID}, [2]string{appID, l.Lemma})
		d.LemmaMatches[index].Application = appID
		d.addTask(appID, "Проверить подобранное применение", "formalize", "Докажи точную цель, используя опубликованную лемму и доказанные предпосылки. Приемка выполняется отдельно.")
		if m.Report.Status == "verified" {
			e := d.entity(appID)
			env, pins := s.libraryEnvironment(d, e)
			d.Verifications = append(d.Verifications, Verification{ID: identifier("verify"), Target: appID, TargetRevision: e.Revision, SubmittedRevision: e.Revision, Origin: "submitted", Author: "procedure:lean-type-matcher@1", Basis: "Нативный подбор " + m.ID + "; отчет " + hash(m.Report), Goal: m.Goal, Source: m.Report.Source, Environment: env, LibraryPins: pins, DependencyRevisions: map[string]int{l.Lemma: l.LemmaRevision}, Status: "queued", CreatedAt: time.Now().UTC()})
		}
		return nil
	})
}
