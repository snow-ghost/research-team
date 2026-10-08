package researchweb

import (
	"github.com/snow-ghost/research-team/internal/leancheck"
	"strings"
	"time"
)

type RefutationReviewRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	ReviewAttempt    string `json:"review_attempt"`
}
type RefutationAcceptanceRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Confirm          bool   `json:"confirm"`
	Note             string `json:"note"`
}

func (s *Service) SubmitRefutation(target string, r ProofSourceRequest) error {
	if s.Options.Checker == nil || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || !textOK(r.Source, 64000) || !textOK(r.Author, 300) || !textOK(r.Basis, 4000) {
		return RuleError("Нужны проверяющая служба, исходник отрицания, автор и основание.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(struct {
		Target string
		ProofSourceRequest
	}{target, r}), "Зарегистрировано доказательство отрицания", target, "operator", func(d *Data) error {
		e := d.entity(target)
		if e == nil || e.FormalGoal == nil || e.Status == "accepted" || e.Status == "refuted" {
			return RuleError("Нужна открытая закрепленная цель.")
		}
		v := Verification{ID: identifier("verify"), Purpose: "refutation", OriginalGoalSHA256: leancheck.Digest(*e.FormalGoal), Origin: "submitted", Author: strings.TrimSpace(r.Author), Basis: strings.TrimSpace(r.Basis), SubmittedRevision: e.Revision, Target: e.ID, TargetRevision: e.Revision, Goal: refutationGoal(*e.FormalGoal), Source: r.Source, Status: "queued", CreatedAt: time.Now().UTC()}
		v.Environment, v.LibraryPins = s.libraryEnvironment(d, e)
		d.Verifications = append(d.Verifications, v)
		return nil
	})
}

func (s *Service) queueRefutation(d *Data, attempt string) (string, error) {
	a := d.attempt(attempt)
	if a == nil || a.Status != "candidate" || d.task(a.TaskID) == nil {
		return "", RuleError("Нужна завершенная попытка проверки контрпримера.")
	}
	formalizer := a.Role == "formalize" && d.team(a.TeamID) != nil && d.team(a.TeamID).Refuting
	if d.task(a.TaskID).Kind != "counterexample" && !formalizer {
		return "", RuleError("Нужно задание контрпримеров или формализация отрицания.")
	}
	e := d.entity(a.Target)
	if e == nil || e.FormalGoal == nil || e.Revision != a.TargetRevision {
		return "", RuleError("Изменилась формальная цель.")
	}
	result, err := s.readResult(*a)
	if err != nil {
		return "", err
	}
	for _, v := range d.Verifications {
		if v.Attempt == attempt && v.Purpose == "refutation" && (v.Status == "queued" || v.Status == "running") {
			return "", RuleError("Проверка отрицания уже выполняется.")
		}
	}
	var report CounterReport
	if formalizer {
		report.RefutationSource, err = leancheck.ExtractSource(result.Candidate)
		if err != nil {
			return "", err
		}
	} else {
		if decodeAgentReport(result.Candidate, &report) != nil || report.Outcome != "counterexample_candidate" || !textOK(report.Evidence, 4000) || !textOK(report.RefutationSource, 64000) {
			return "", RuleError("Нужны описание контрпримера и исходник доказательства отрицания.")
		}
	}
	id := identifier("verify")
	v := Verification{ID: id, Attempt: a.ID, Target: a.Target, TargetRevision: a.TargetRevision, Purpose: "refutation", OriginalGoalSHA256: leancheck.Digest(*e.FormalGoal), Goal: refutationGoal(*e.FormalGoal), Source: report.RefutationSource, Status: "queued", CreatedAt: time.Now().UTC()}
	v.Environment, v.LibraryPins = s.libraryEnvironment(d, e)
	d.Verifications = append(d.Verifications, v)
	return id, nil
}

func (s *Service) StartRefutation(r VerifyRequest) error {
	if s.Options.Checker == nil || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || r.Source != "" {
		return RuleError("Выберите попытку; подмена исходника не допускается.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Назначена проверка отрицания", r.Attempt, "operator", func(d *Data) error { _, err := s.queueRefutation(d, r.Attempt); return err })
}

func (s *Service) bindRefutationReview(d *Data, id, reviewID string) error {
	v := d.verification(id)
	if v == nil || v.Purpose != "refutation" || !verifiedReportMatches(*v) || !verificationMatches(d, *v) {
		return RuleError("Нужна действительная проверка отрицания текущей цели.")
	}
	e := d.entity(v.Target)
	review := d.attempt(reviewID)
	if review == nil || review.Target != e.ID || review.TargetRevision != e.Revision || review.Status != "candidate" || d.task(review.TaskID) == nil || d.task(review.TaskID).Kind != "review" || review.ProofBinding == nil || *review.ProofBinding != (ProofBinding{v.ID, v.Report.GoalSHA256, v.Report.SourceSHA256}) || v.Author == "executor:"+review.Profile {
		return RuleError("Нужна отдельная точная рецензия отрицания.")
	}
	if author := d.attempt(v.Attempt); author != nil && (review.Profile == author.Profile || review.ReviewOf != author.ID) {
		return RuleError("Рецензент совпадает с автором отрицания.")
	}
	input, err := s.readInput(*review)
	if err != nil {
		return err
	}
	old := input.attempt(review.ID)
	if old == nil || old.ProofBinding == nil || *old.ProofBinding != *review.ProofBinding {
		return RuleError("Изменилась привязка рецензии.")
	}
	result, err := s.readResult(*review)
	if err != nil {
		return err
	}
	var report ReviewReport
	if !s.safeResultContent(*review, result) || decodeAgentReport(result.Candidate, &report) != nil || !textOK(report.Summary, 4000) || report.Findings == nil || len(report.Findings) > 50 {
		return RuleError("Некорректная рецензия отрицания.")
	}
	for _, f := range report.Findings {
		if !slicesContainsSeverity(f.Severity) || !textOK(f.Text, 4000) {
			return RuleError("Некорректное замечание.")
		}
		exists := false
		for _, old := range d.Findings {
			if old.Target == e.ID && old.Severity == f.Severity && old.ReviewAttempt == review.ID && findingTextMatches(old, f.Text) {
				exists = true
			}
		}
		if !exists {
			d.Findings = append(d.Findings, Finding{ID: identifier("F"), Target: e.ID, Text: f.Text, SourceTextSHA256: hash(f.Text), Severity: f.Severity, State: "open", Revision: e.Revision, ReviewAttempt: review.ID})
		}
	}
	e.RefutationVerification, e.RefutationReview = v.ID, review.ID
	e.Status = "in_review"
	e.Counterexample = report.Summary
	return nil
}

func (s *Service) BindRefutationReview(id string, r RefutationReviewRequest) error {
	if r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) {
		return RuleError("Нужны снимок и идентификатор команды.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(struct {
		ID string
		RefutationReviewRequest
	}{id, r}), "Связана рецензия отрицания", id, "operator", func(d *Data) error { return s.bindRefutationReview(d, id, r.ReviewAttempt) })
}

func (s *Service) AcceptRefutation(id string, r RefutationAcceptanceRequest) error {
	if !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || !textOK(r.Note, 4000) {
		return RuleError("Подтвердите решение и укажите основание.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(struct {
		ID string
		RefutationAcceptanceRequest
	}{id, r}), "Принято проверенное отрицание", id, "operator", func(d *Data) error {
		v := d.verification(id)
		if v == nil || v.Purpose != "refutation" || !verifiedReportMatches(*v) || !verificationMatches(d, *v) {
			return RuleError("Нужна действительная проверка отрицания.")
		}
		if v.Report.AuditSHA256 != leancheck.AuditDigest() {
			return RuleError("Нужна новая проверка Lean актуальной программой аудита.")
		}
		e := d.entity(v.Target)
		if v.Author == "operator" {
			return RuleError("Автор отрицания не может принимать собственный результат.")
		}
		if e.Status != "in_review" || e.RefutationVerification != v.ID || e.RefutationReview == "" {
			return RuleError("Сначала свяжите точную рецензию.")
		}
		if err := s.bindRefutationReview(d, id, e.RefutationReview); err != nil {
			return err
		}
		for _, f := range d.Findings {
			if f.Target == e.ID && f.State != "resolved" && f.Severity != "editorial" {
				return RuleError("Есть незакрытые существенные замечания.")
			}
		}
		e.Status = "refuted"
		e.Revision++
		v.TargetRevision = e.Revision
		e.ReviewedBy = "operator"
		e.ReviewReason = r.Note
		return nil
	})
}
