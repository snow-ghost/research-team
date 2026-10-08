package researchweb

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

type Verification struct {
	Purpose             string            `json:"purpose,omitempty"`
	OriginalGoalSHA256  string            `json:"original_goal_sha256,omitempty"`
	Environment         *leancheck.Config `json:"environment,omitempty"`
	LibraryPins         map[string]string `json:"library_pins,omitempty"`
	ID                  string            `json:"id"`
	Target              string            `json:"target"`
	TargetRevision      int               `json:"target_revision"`
	Attempt             string            `json:"attempt,omitempty"`
	Origin              string            `json:"origin,omitempty"`
	Author              string            `json:"author,omitempty"`
	Basis               string            `json:"basis,omitempty"`
	SubmittedRevision   int               `json:"submitted_revision,omitempty"`
	DependencyRevisions map[string]int    `json:"dependency_revisions,omitempty"`
	Status              string            `json:"status"`
	Goal                leancheck.Goal    `json:"goal"`
	Source              string            `json:"source"`
	Report              *leancheck.Report `json:"report,omitempty"`
	CreatedAt           time.Time         `json:"created_at"`
	FinishedAt          *time.Time        `json:"finished_at,omitempty"`
}
type FormalGoalRequest struct {
	Libraries        []string       `json:"libraries,omitempty"`
	ExpectedRevision int            `json:"expected_revision"`
	RequestID        string         `json:"request_id"`
	Goal             leancheck.Goal `json:"goal"`
}
type VerifyRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Attempt          string `json:"attempt"`
	Source           string `json:"source,omitempty"`
}

type ProofSourceRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Source           string `json:"source"`
	Author           string `json:"author"`
	Basis            string `json:"basis"`
}

type AttachVerificationRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
}

// Submitted sources have declared provenance, not a synthetic executor attempt.
func (s *Service) SubmitProofSource(target string, r ProofSourceRequest) error {
	r.Author, r.Basis = strings.TrimSpace(r.Author), strings.TrimSpace(r.Basis)
	if s.Options.Checker == nil {
		return RuleError("Проверка Lean не настроена.")
	}
	if r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) {
		return RuleError("Нужны снимок и идентификатор команды.")
	}
	if !textOK(r.Source, 64000) || !textOK(r.Author, 300) || !textOK(r.Basis, 4000) {
		return RuleError("Нужны исходный текст до 64000 байт, автор и описание происхождения.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(struct {
		Target string
		ProofSourceRequest
	}{target, r}), "Зарегистрирован исходный текст доказательства", target, "operator", func(d *Data) error {
		e := d.entity(target)
		if e == nil || e.FormalGoal == nil || e.Status == "accepted" || e.Status == "refuted" {
			return RuleError("Нужна открытая версия с закрепленной формальной целью.")
		}
		pins := map[string]int{}
		for _, dep := range e.Dependencies {
			current := d.entity(dep)
			if current == nil {
				return RuleError("Основание не найдено.")
			}
			pins[dep] = current.Revision
		}
		d.Verifications = append(d.Verifications, Verification{
			ID: identifier("verify"), Target: target, TargetRevision: e.Revision, SubmittedRevision: e.Revision,
			Origin: "submitted", Author: r.Author, Basis: r.Basis, DependencyRevisions: pins,
			Status: "queued", Goal: *e.FormalGoal, Source: r.Source, CreatedAt: time.Now().UTC(),
		})
		d.Verifications[len(d.Verifications)-1].Environment, d.Verifications[len(d.Verifications)-1].LibraryPins = s.libraryEnvironment(d, e)
		return nil
	})
}

func (s *Service) AttachVerification(id string, r AttachVerificationRequest) error {
	if r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) {
		return RuleError("Нужны снимок и идентификатор команды.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(struct {
		ID string
		AttachVerificationRequest
	}{id, r}), "Проверенный исходный текст направлен на приемку", id, "operator", func(d *Data) error {
		v := d.verification(id)
		if v == nil || v.Purpose == "refutation" || v.Origin != "submitted" || !verifiedReportMatches(*v) || !verificationMatches(d, *v) {
			return RuleError("Нужен зарегистрированный исходный текст с успешной проверкой текущей цели.")
		}
		e := d.entity(v.Target)
		if e.Status == "accepted" || e.Status == "refuted" {
			return RuleError("Принятая версия не изменяется.")
		}
		if len(v.DependencyRevisions) != len(e.Dependencies) {
			return RuleError("Основания изменились после регистрации исходного текста.")
		}
		for _, dep := range e.Dependencies {
			current := d.entity(dep)
			if current == nil || v.DependencyRevisions[dep] != current.Revision {
				return RuleError("Основания изменились после регистрации исходного текста.")
			}
		}
		e.Proof, e.ProofAuthor, e.ProofAttempt = v.Source, v.Author, ""
		e.ProofVerification = v.ID
		e.DependencyRevisions = v.DependencyRevisions
		e.ReviewReason, e.ReviewedBy = "", ""
		e.Status = "in_review"
		e.Revision++
		v.TargetRevision = e.Revision
		return nil
	})
}

func (d *Data) verification(id string) *Verification {
	for i := range d.Verifications {
		if d.Verifications[i].ID == id {
			return &d.Verifications[i]
		}
	}
	return nil
}
func (s *Service) SetFormalGoal(target string, r FormalGoalRequest) error {
	if r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || len(r.Libraries) > 16 {
		return RuleError("Нужны снимок и идентификатор команды.")
	}
	if err := r.Goal.Validate(); err != nil {
		return err
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Закреплена формальная цель", target, "operator", func(d *Data) error {
		e := d.entity(target)
		if e == nil || e.Status == "accepted" || e.Status == "refuted" {
			return RuleError("Нужна открытая версия утверждения.")
		}
		addedDependency := false
		for _, id := range r.Libraries {
			var selected *LibraryEntry
			for i := range d.Library {
				if d.Library[i].ID == id {
					selected = &d.Library[i]
					break
				}
			}
			if selected == nil || selected.Status != "ready" || selected.Lemma == e.ID || !libraryMatches(d, *selected) {
				return RuleError("Нужен действующий модуль принятой леммы.")
			}
			found := false
			for _, dep := range e.Dependencies {
				if dep == selected.Lemma {
					found = true
				}
			}
			if !found {
				e.Dependencies = append(e.Dependencies, selected.Lemma)
				addedDependency = true
			}
		}
		if !addedDependency && e.FormalGoal != nil && leancheck.Digest(*e.FormalGoal) == leancheck.Digest(r.Goal) {
			return nil
		}
		e.FormalGoal = &r.Goal
		e.RefutationVerification, e.RefutationReview = "", ""
		e.Revision++
		e.Status = "open"
		e.Proof, e.ProofAuthor, e.ProofAttempt = "", "", ""
		e.ProofVerification = ""
		e.DependencyRevisions = nil
		for i := range d.Verifications {
			v := &d.Verifications[i]
			if v.Target == target && v.Status != "running" {
				v.Status = "stale"
			}
		}
		return nil
	})
}
func (s *Service) StartVerification(r VerifyRequest) error {
	if s.Options.Checker == nil {
		return RuleError("Проверка Lean не настроена.")
	}
	if r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) {
		return RuleError("Нужны снимок и идентификатор команды.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Назначена проверка Lean", r.Attempt, "operator", func(d *Data) error {
		_, err := s.queueVerification(d, r.Attempt, r.Source)
		return err
	})
}
func (s *Service) queueVerification(d *Data, id, source string) (string, error) {
	a := d.attempt(id)
	if a == nil || a.Status != "candidate" {
		return "", RuleError("Нужна завершенная попытка с кандидатом.")
	}
	e := d.entity(a.Target)
	if e == nil || (e.Revision != a.TargetRevision && !(e.ProofAttempt == a.ID && e.Revision == a.TargetRevision+1)) || e.FormalGoal == nil {
		return "", RuleError("Нужна закрепленная формальная цель текущей версии.")
	}
	originalSource := ""
	{
		result, err := s.readResult(*a)
		if err != nil {
			return "", err
		}
		originalSource, err = leancheck.ExtractSource(result.Candidate)
		if err != nil {
			return "", RuleError("В результате нужен один завершенный блок Lean.")
		}
	}
	if source != "" && source != originalSource {
		return "", RuleError("Измененный исходный текст требует новой попытки.")
	}
	source = originalSource
	if len(source) == 0 || len(source) > 65536 {
		return "", RuleError("Нужен исходный текст кандидата до 64 КиБ.")
	}
	for _, v := range d.Verifications {
		if v.Attempt == id && (v.Status == "queued" || v.Status == "running") {
			return "", RuleError("Проверка уже выполняется.")
		}
	}
	verification := Verification{ID: identifier("verify"), Target: e.ID, TargetRevision: e.Revision, Attempt: id,
		Status: "queued", Goal: *e.FormalGoal, Source: source, CreatedAt: time.Now().UTC()}
	verification.Environment, verification.LibraryPins = s.libraryEnvironment(d, e)
	d.Verifications = append(d.Verifications, verification)
	return verification.ID, nil
}
func (s *Service) startChecks() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.running) >= s.Options.Config.MaxParallel || s.Options.Checker == nil {
		return
	}
	view, err := s.Store.Read()
	if err != nil || view.Maintenance != nil {
		return
	}
	for _, v := range view.Verifications {
		if v.Status != "queued" {
			continue
		}
		err = s.Store.Change(0, "", "", "Начата проверка Lean", v.ID, "checker", func(d *Data) error {
			current := d.verification(v.ID)
			if current == nil || current.Status != "queued" {
				return ErrConflict
			}
			current.Status = "running"
			return nil
		})
		if err != nil {
			continue
		}
		ctx, cancel := context.WithCancel(s.ctx)
		s.running["verify:"+v.ID] = cancel
		s.wg.Add(1)
		go s.checkProof(ctx, v)
		break
	}
}
func (s *Service) checkProof(ctx context.Context, v Verification) {
	defer s.wg.Done()
	dir := filepath.Join(s.Options.Config.DataDir, "verifications", v.ID)
	var report leancheck.Report
	err := os.MkdirAll(dir, 0700)
	if err == nil && len(v.LibraryPins) > 0 {
		view, readErr := s.Store.Read()
		err = readErr
		if err == nil {
			for id, pin := range v.LibraryPins {
				found := false
				for _, l := range view.Library {
					if l.ID == id && l.Report != nil && l.Report.ArtifactSHA256 == pin && l.Status == "ready" && libraryMatches(&view.Data, l) {
						found = true
						err = leancheck.VerifyModuleArtifacts(filepath.Join(s.Options.Config.DataDir, "library", id, "checked"), *l.Report)
					}
				}
				if !found {
					err = RuleError("Модуль библиотеки устарел.")
				}
				if err != nil {
					break
				}
			}
		}
	}
	if err == nil {
		checker := s.Options.Checker
		if v.Environment != nil {
			checker = leancheck.DockerChecker{Config: *v.Environment}
		}
		report, err = checker.Check(ctx, v.Goal, v.Source, dir)
	}
	if err != nil {
		report.Status = "failed"
	}
	expectedEnv := s.Options.Lean
	if v.Environment != nil {
		expectedEnv = v.Environment
	}
	if report.Status == "verified" && (report.GoalSHA256 != leancheck.Digest(v.Goal) || report.SourceSHA256 != leancheck.Digest(v.Source) ||
		(expectedEnv != nil && report.EnvironmentSHA256 != leancheck.Digest(*expectedEnv))) {
		report.Status, report.Phase = "failed", "report_integrity"
	}
	if ctx.Err() != nil {
		report.Status = "interrupted"
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel := s.running["verify:"+v.ID]; cancel != nil {
		cancel()
	}
	delete(s.running, "verify:"+v.ID)
	err = s.Store.Change(0, "", "", "Завершена проверка Lean", v.ID, "checker", func(d *Data) error {
		current := d.verification(v.ID)
		if current == nil {
			return errors.New("verification missing")
		}
		current.Report = &report
		if current.Status == "interrupted" {
			report.Status = "interrupted"
		}
		current.Status = report.Status
		current.FinishedAt = &now
		if !verificationMatches(d, *current) {
			current.Status = "stale"
		}
		return nil
	})
	if err != nil {
		s.cancel()
	}
}
func verificationMatches(d *Data, v Verification) bool {
	e := d.entity(v.Target)
	if e == nil || e.Revision != v.TargetRevision || e.FormalGoal == nil {
		return false
	}
	expected := *e.FormalGoal
	if v.Purpose == "refutation" {
		if v.OriginalGoalSHA256 != leancheck.Digest(expected) {
			return false
		}
		expected = refutationGoal(expected)
	}
	if leancheck.Digest(expected) != leancheck.Digest(v.Goal) {
		return false
	}
	for id, pin := range v.LibraryPins {
		found := false
		for _, l := range d.Library {
			if l.ID == id && l.Status == "ready" && l.Report != nil && l.Report.ArtifactSHA256 == pin && libraryMatches(d, l) {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func hasVerifiedProof(d *Data, e *Entity) bool {
	for _, v := range d.Verifications {
		if v.Purpose == "refutation" {
			continue
		}
		matches := e.ProofAttempt != "" && v.Attempt == e.ProofAttempt
		if e.ProofVerification != "" {
			matches = v.ID == e.ProofVerification && v.Origin == "submitted" && v.Attempt == "" && e.ProofAttempt == "" && e.Proof == v.Source && e.ProofAuthor == v.Author
		}
		if matches && verifiedReportMatches(v) && verificationMatches(d, v) {
			return true
		}
	}
	return false
}

func verifiedReportMatches(v Verification) bool {
	return v.Status == "verified" && v.Report != nil && v.Report.Status == "verified" &&
		v.Report.GoalSHA256 == leancheck.Digest(v.Goal) && v.Report.SourceSHA256 == leancheck.Digest(v.Source)
}
