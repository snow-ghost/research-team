package researchweb

import (
	"strings"
	"time"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

type ProofBinding struct {
	Verification string `json:"verification"`
	GoalSHA256   string `json:"goal_sha256"`
	SourceSHA256 string `json:"source_sha256"`
}

func reviewMaterials(d Data, a Attempt) any {
	if a.ProofBinding == nil {
		return nil
	}
	v := d.verification(a.ProofBinding.Verification)
	if v == nil || v.Target != a.Target || !verifiedReportMatches(*v) || v.Report.GoalSHA256 != a.ProofBinding.GoalSHA256 || v.Report.SourceSHA256 != a.ProofBinding.SourceSHA256 {
		return nil
	}
	author := v.Author
	if source := d.attempt(v.Attempt); author == "" && source != nil {
		author = "executor:" + source.Profile
	}
	return map[string]any{"proof_binding": a.ProofBinding, "goal": v.Goal, "source": v.Source, "report": v.Report, "author": author, "origin": v.Origin, "purpose": v.Purpose, "original_goal_sha256": v.OriginalGoalSHA256}
}

type ReviewReport struct {
	Summary  string          `json:"summary"`
	Findings []ReviewFinding `json:"findings"`
}
type ReviewFinding struct {
	Severity string `json:"severity"`
	Text     string `json:"text"`
}
type CounterReport struct {
	RefutationSource string `json:"refutation_source,omitempty"`
	Outcome          string `json:"outcome"`
	Evidence         string `json:"evidence"`
}
type ResearchResult struct {
	FindingIDs          []string       `json:"finding_ids,omitempty"`
	ID                  string         `json:"id"`
	Target              string         `json:"target"`
	TargetRevision      int            `json:"target_revision"`
	Binding             ProofBinding   `json:"binding"`
	Author              string         `json:"author"`
	EnvironmentSHA256   string         `json:"environment_sha256"`
	ReviewAttempt       string         `json:"review_attempt"`
	CounterAttempt      string         `json:"counter_attempt"`
	ReviewResultSHA256  string         `json:"review_result_sha256"`
	CounterResultSHA256 string         `json:"counter_result_sha256"`
	ReviewBindingMethod string         `json:"review_binding_method"`
	Review              ReviewReport   `json:"review"`
	Counter             CounterReport  `json:"counter"`
	DependencyRevisions map[string]int `json:"dependency_revisions,omitempty"`
	Status              string         `json:"status"`
	CreatedAt           time.Time      `json:"created_at"`
}
type ResultRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Target           string `json:"target"`
	ReviewAttempt    string `json:"review_attempt"`
	CounterAttempt   string `json:"counter_attempt"`
	AuditLegacyInput bool   `json:"audit_legacy_input,omitempty"`
}

func proofVerification(d *Data, e *Entity) *Verification {
	var legacy *Verification
	for i := range d.Verifications {
		v := &d.Verifications[i]
		if verificationMatches(d, *v) && verifiedReportMatches(*v) &&
			((e.ProofVerification != "" && v.ID == e.ProofVerification) || (e.ProofAttempt != "" && v.Attempt == e.ProofAttempt)) {
			if v.Report.AuditSHA256 == leancheck.AuditDigest() {
				return v
			}
			if legacy == nil {
				legacy = v
			}
		}
	}
	return legacy
}
func (s *Service) BindResult(r ResultRequest) error {
	if r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) {
		return RuleError("Нужны снимок и идентификатор команды.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Связаны материалы результата", r.Target, "operator", func(d *Data) error { return s.bindResult(d, r) })
}
func (s *Service) bindResult(d *Data, r ResultRequest) error {
	e := d.entity(r.Target)
	if e == nil || !hasVerifiedProof(d, e) {
		return RuleError("Нужно прикрепленное доказательство с действующей проверкой Lean.")
	}
	v := proofVerification(d, e)
	binding := ProofBinding{v.ID, v.Report.GoalSHA256, v.Report.SourceSHA256}
	review, counter := d.attempt(r.ReviewAttempt), d.attempt(r.CounterAttempt)
	if review == nil || counter == nil || review.Target != e.ID || counter.Target != e.ID || review.Status != "candidate" || counter.Status != "candidate" {
		return RuleError("Нужны завершенные рецензия и проверка контрпримеров этого утверждения.")
	}
	if d.task(review.TaskID) == nil || d.task(review.TaskID).Kind != "review" || d.task(counter.TaskID) == nil || d.task(counter.TaskID).Kind != "counterexample" {
		return RuleError("Не совпадают виды заданий проверки.")
	}
	original := v.SubmittedRevision
	if a := d.attempt(e.ProofAttempt); a != nil {
		original = a.TargetRevision
		if review.Profile == a.Profile || review.ReviewOf != a.ID {
			return RuleError("Нужна отдельная рецензия именно этого кандидата.")
		}
	}
	if original < 1 || review.TargetRevision != original || counter.TargetRevision != original || e.ProofAuthor == "executor:"+review.Profile {
		return RuleError("Не совпадают исходные версии или автор и рецензент.")
	}
	method := "typed_binding"
	if review.ProofBinding != nil {
		input, err := s.readInput(*review)
		if err != nil {
			return err
		}
		old := input.attempt(review.ID)
		if old == nil || old.ProofBinding == nil || *old.ProofBinding != *review.ProofBinding {
			return RuleError("Изменилась привязка исходной рецензии.")
		}
	}
	if review.ProofBinding == nil || *review.ProofBinding != binding {
		if !r.AuditLegacyInput || review.ProofBinding != nil {
			return RuleError("Рецензия не привязана к точному файлу и цели.")
		}
		input, err := s.readInput(*review)
		if err != nil {
			return err
		}
		task := input.task(review.TaskID)
		if task == nil || !strings.Contains(task.Objective, v.Source) || !strings.Contains(task.Objective, binding.GoalSHA256) || !strings.Contains(task.Objective, binding.SourceSHA256) {
			return RuleError("Исходное задание не содержит точный файл и контрольные суммы.")
		}
		method = "audited_legacy_input"
	}
	rr, err := s.readResult(*review)
	if err != nil {
		return err
	}
	cr, err := s.readResult(*counter)
	if err != nil {
		return err
	}
	if !s.safeResultContent(*review, rr) || !s.safeResultContent(*counter, cr) {
		return RuleError("Материалы получены до разделения потоков Coddy.")
	}
	var report ReviewReport
	var checked CounterReport
	if decodeAgentReport(rr.Candidate, &report) != nil || !textOK(report.Summary, 4000) || report.Findings == nil || len(report.Findings) > 50 {
		return RuleError("Некорректная рецензия.")
	}
	for _, f := range report.Findings {
		if !slicesContainsSeverity(f.Severity) || !textOK(f.Text, 4000) {
			return RuleError("Некорректное замечание.")
		}
	}
	if decodeAgentReport(cr.Candidate, &checked) != nil || !textOK(checked.Evidence, 4000) || (checked.Outcome != "none_found" && checked.Outcome != "inconclusive" && checked.Outcome != "counterexample_candidate") {
		return RuleError("Некорректный отчет контрпримеров.")
	}
	if input, err := s.readInput(*counter); err != nil {
		return err
	} else if old := input.entity(e.ID); old == nil || old.FormalGoal == nil || leancheck.Digest(*old.FormalGoal) != binding.GoalSHA256 {
		return RuleError("Контрпримеры проверялись для другой формальной цели.")
	}
	id := identifier("result")
	findingIDs := []string{}
	for _, f := range report.Findings {
		found := ""
		for i := range d.Findings {
			old := &d.Findings[i]
			if old.Target == e.ID && findingTextMatches(*old, f.Text) && old.Severity == f.Severity && (old.ReviewAttempt == review.ID || old.ReviewAttempt == "") {
				old.ReviewAttempt = review.ID
				found = old.ID
				break
			}
		}
		if found == "" {
			found = identifier("F")
			d.Findings = append(d.Findings, Finding{ID: found, Target: e.ID, Text: f.Text, SourceTextSHA256: hash(f.Text), Severity: f.Severity, State: "open", Revision: e.Revision, ReviewAttempt: review.ID})
		}
		findingIDs = append(findingIDs, found)
	}
	d.Results = append(d.Results, ResearchResult{ID: id, Target: e.ID, TargetRevision: e.Revision, Binding: binding, Author: e.ProofAuthor, EnvironmentSHA256: v.Report.EnvironmentSHA256,
		ReviewAttempt: review.ID, CounterAttempt: counter.ID, ReviewResultSHA256: review.ResultSHA256, CounterResultSHA256: counter.ResultSHA256, ReviewBindingMethod: method,
		Review: report, Counter: checked, DependencyRevisions: e.DependencyRevisions, Status: "ready", CreatedAt: time.Now().UTC()})
	d.Results[len(d.Results)-1].FindingIDs = findingIDs
	e.ResearchResult = id
	return nil
}
func resultMatches(d *Data, r ResearchResult) bool {
	e := d.entity(r.Target)
	if e == nil || e.Status == "challenged" || e.Status == "refuted" || e.ResearchResult != r.ID || e.Revision != r.TargetRevision || e.ProofAuthor != r.Author || !hasVerifiedProof(d, e) {
		return false
	}
	v := proofVerification(d, e)
	if v.ID != r.Binding.Verification || v.Report.GoalSHA256 != r.Binding.GoalSHA256 || v.Report.SourceSHA256 != r.Binding.SourceSHA256 || v.Report.EnvironmentSHA256 != r.EnvironmentSHA256 {
		return false
	}
	for dep, rev := range r.DependencyRevisions {
		if x := d.entity(dep); x == nil || x.Revision != rev || d.effective(dep, map[string]bool{}) != "accepted" {
			return false
		}
	}
	ra, ca := d.attempt(r.ReviewAttempt), d.attempt(r.CounterAttempt)
	return ra != nil && ca != nil && ra.ResultSHA256 == r.ReviewResultSHA256 && ca.ResultSHA256 == r.CounterResultSHA256
}
func refreshEvidence(d *Data) {
	for i := range d.Proposals {
		p := &d.Proposals[i]
		if p.Status == "proposed" {
			e := d.entity(p.Target)
			if e == nil || e.Revision != p.TargetRevision || e.Status == "accepted" || e.Status == "refuted" {
				p.Status = "stale"
			}
		}
	}
	for i := range d.Results {
		r := &d.Results[i]
		if !resultMatches(d, *r) {
			r.Status = "stale"
			continue
		}
		r.Status = "ready"
		if r.Counter.Outcome != "none_found" || d.entity(r.Target).Status == "needs_changes" {
			r.Status = "needs_changes"
		}
		if len(r.FindingIDs) != len(r.Review.Findings) {
			r.Status = "needs_changes"
		}
		for _, id := range r.FindingIDs {
			found := false
			for _, f := range d.Findings {
				if f.ID == id && f.ReviewAttempt == r.ReviewAttempt {
					found = true
					if f.Severity != "editorial" && f.State != "resolved" {
						r.Status = "needs_changes"
					}
				}
			}
			if !found {
				r.Status = "needs_changes"
			}
		}
		for _, f := range d.Findings {
			if f.Target == r.Target && f.State != "resolved" && f.Severity != "editorial" {
				r.Status = "needs_changes"
			}
		}
		if d.entity(r.Target).Status == "accepted" && r.Status == "ready" {
			r.Status = "accepted"
		}
	}
	for i := range d.Library {
		l := &d.Library[i]
		if !libraryMatches(d, *l) {
			l.Status = "stale"
		}
	}
}
