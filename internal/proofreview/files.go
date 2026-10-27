package proofreview

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

const FileInputInstructions = InputInstructions + ` Поле checked_files разделяет фактический input/Goal.lean, переданный компилятору как модуль Goal, и input/Candidate.lean с доказательством. definitions.source должен совпадать с claim_under_review.source, candidate.source с source. raw_sha256 вычислен по байтам UTF-8 файла, отдельно от контрольных сумм JSON. Аудит проверяет происхождение целевой декларации из Goal; успешная компиляция без этого аудита недостаточна. Требование самодостаточности модуля определений не означает запрет import Goal в кандидате. Если область текстового требования неоднозначна, назови конкретную неоднозначность; не объявляй отсутствующим текст Goal, представленный в checked_files.definitions. Соответствие естественной постановки формальной цели по-прежнему проверяется отдельно.`

type CheckedFile struct {
	Name      string `json:"name"`
	Role      string `json:"role"`
	Source    string `json:"source"`
	RawSHA256 string `json:"raw_sha256"`
}

type CheckedFiles struct {
	Definitions       CheckedFile `json:"definitions"`
	Candidate         CheckedFile `json:"candidate"`
	GoalModule        string      `json:"goal_module"`
	GoalDeclaration   string      `json:"goal_declaration"`
	ProofDeclaration  string      `json:"proof_declaration"`
	AuditSHA256       string      `json:"audit_sha256"`
	GoalOriginChecked bool        `json:"goal_origin_checked"`
}

func RawSHA256(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}

// File evidence is attached only after reading the actual compiler inputs.
func WithCheckedFiles(input Input, definitions, candidate string) (Input, error) {
	if definitions != input.ClaimUnderReview.Source || candidate != input.Source || input.Report.Status != "verified" || input.Report.Phase != "complete" || input.Report.AuditSHA256 != leancheck.AuditDigest() || input.Report.GoalSHA256 != leancheck.Digest(input.ClaimUnderReview) || input.Report.SourceSHA256 != leancheck.Digest(candidate) {
		return Input{}, errors.New("checked files do not match the audited claim and candidate")
	}
	input.CheckedFiles = &CheckedFiles{
		Definitions: CheckedFile{Name: "Goal.lean", Role: "definitions", Source: definitions, RawSHA256: RawSHA256(definitions)},
		Candidate:   CheckedFile{Name: "Candidate.lean", Role: "proof", Source: candidate, RawSHA256: RawSHA256(candidate)},
		GoalModule:  "Goal", GoalDeclaration: input.ClaimUnderReview.Declaration, ProofDeclaration: input.ClaimUnderReview.Candidate,
		AuditSHA256: input.Report.AuditSHA256, GoalOriginChecked: true,
	}
	return input, nil
}
