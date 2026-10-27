package researchweb

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/snow-ghost/research-team/internal/proofreview"
)

func (s *Service) reviewMaterialsWithFiles(d Data, a Attempt) (*proofreview.Input, error) {
	if a.ProofBinding == nil {
		return nil, nil
	}
	input, ok := reviewMaterials(d, a).(proofreview.Input)
	if !ok {
		return nil, RuleError("Не удалось закрепить точный вход рецензии.")
	}
	input, err := s.attachReviewFiles(input, a.ProofBinding.Verification)
	return &input, err
}

func (s *Service) attachReviewFiles(input proofreview.Input, verification string) (proofreview.Input, error) {
	dir := filepath.Join(s.Options.Config.DataDir, "verifications", verification, "input")
	definitions, goalErr := readBoundedFile(filepath.Join(dir, "Goal.lean"), 65536)
	candidate, candidateErr := readBoundedFile(filepath.Join(dir, "Candidate.lean"), 65536)
	if errors.Is(goalErr, os.ErrNotExist) && errors.Is(candidateErr, os.ErrNotExist) {
		// Older imported checks and test checkers may not retain compiler inputs.
		return input, nil
	}
	if goalErr != nil || candidateErr != nil {
		return proofreview.Input{}, RuleError("Не удалось прочитать оба входных файла проверки Lean.")
	}
	bound, err := proofreview.WithCheckedFiles(input, string(definitions), string(candidate))
	if err != nil {
		return proofreview.Input{}, RuleError("Файлы проверки Lean не совпадают с закрепленной целью и исходником.")
	}
	return bound, nil
}
