package researchweb

import "github.com/snow-ghost/research-team/internal/proofreview"

func validAttemptReview(a Attempt, r ReviewReport) bool {
	return proofreview.Validate(r) == nil && (a.ReviewRubric == "" || a.ReviewRubric == r.Rubric)
}

func reviewFinding(id string, e *Entity, attempt string, f ReviewFinding) Finding {
	return Finding{ID: id, Target: e.ID, Text: f.Text, SourceTextSHA256: hash(f.Text), Severity: f.Severity, State: "open", Revision: e.Revision, ReviewAttempt: attempt, Category: f.Category, Evidence: f.Evidence, Location: f.Location}
}
