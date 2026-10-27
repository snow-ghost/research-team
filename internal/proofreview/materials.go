package proofreview

import (
	"errors"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

const InputSchema = "proof-review-input@1"
const InputInstructions = Instructions + ` Вход proof-review-input@1 разделяет original_goal и claim_under_review. Проверяй только claim_under_review и переданный source. claim_kind=refutation означает доказательство отрицания исходной цели; не требуй доказательства самой опровергаемой цели. Исходник исходной цели дан в original_goal.source. Не подменяй математическое замечание общим требованием повторить уже выполненную проверку службы.`

type Input struct {
	Schema           string           `json:"schema"`
	ClaimKind        string           `json:"claim_kind"`
	Statement        string           `json:"statement"`
	OriginalGoal     leancheck.Goal   `json:"original_goal"`
	ClaimUnderReview leancheck.Goal   `json:"claim_under_review"`
	Source           string           `json:"source"`
	Report           leancheck.Report `json:"report"`
	GoalSHA256       string           `json:"goal_sha256"`
	SourceSHA256     string           `json:"source_sha256"`
	Author           string           `json:"author"`
	DigestBasis      string           `json:"digest_basis"`
}

func NewInput(original, claim leancheck.Goal, source string, report leancheck.Report, kind, statement, author string) (Input, error) {
	i := Input{Schema: InputSchema, ClaimKind: kind, Statement: statement, OriginalGoal: original, ClaimUnderReview: claim, Source: source, Report: report, GoalSHA256: leancheck.Digest(claim), SourceSHA256: leancheck.Digest(source), Author: author, DigestBasis: "sha256 of JSON value, not raw file bytes"}
	if original.Validate() != nil || claim.Validate() != nil || source == "" || len(source) > 64000 || report.Status != "verified" || report.GoalSHA256 != i.GoalSHA256 || report.SourceSHA256 != i.SourceSHA256 {
		return Input{}, errors.New("review requires the exact checked claim and source")
	}
	expected := original
	if kind == "refutation" {
		expected = leancheck.Goal{Source: original.Source + "\nnamespace ResearchRefutation\ndef Statement : Prop := Not (" + original.Declaration + ")\nend ResearchRefutation\n", Declaration: "ResearchRefutation.Statement", Candidate: "ResearchRefutation.candidate"}
	} else if kind != "proof" {
		return Input{}, errors.New("unknown review claim kind")
	}
	if leancheck.Digest(expected) != leancheck.Digest(claim) {
		return Input{}, errors.New("review claim does not match the original goal or its exact negation")
	}
	return i, nil
}
