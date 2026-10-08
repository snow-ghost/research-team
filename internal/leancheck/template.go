package leancheck

import "fmt"

// The statement is imported, never redeclared by the candidate.
func CandidateTemplate(g Goal) (string, error) {
	if err := g.Validate(); err != nil {
		return "", err
	}
	return fmt.Sprintf("import Goal\n\ntheorem %s : %s := by\n  -- Supply a proof of the imported statement.\n", g.Candidate, g.Declaration), nil
}
