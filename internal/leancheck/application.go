package leancheck

import (
	"errors"
	"regexp"
	"strings"
)

type Argument struct {
	Name string `json:"name"`
	Term string `json:"term"`
}

// Arguments are proposals. Only elaboration and kernel audit establish applicability.
func ApplicationSource(g Goal, candidate string, binders []Binder, args []Argument) (string, error) {
	template, err := CandidateTemplate(g)
	if err != nil || !declaration.MatchString(candidate) || len(args) > 16 {
		return "", errors.New("invalid application goal or candidate")
	}
	allowed := map[string]bool{}
	for _, b := range binders {
		allowed[b.Name] = true
	}
	seen := map[string]bool{}
	source := template + "  exact " + candidate
	for _, a := range args {
		if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(a.Name) || !allowed[a.Name] || seen[a.Name] || strings.TrimSpace(a.Term) == "" || len(a.Term) > 1000 || strings.ContainsAny(a.Term, "\n\r\x00") {
			return "", errors.New("invalid proposed parameter binding")
		}
		seen[a.Name] = true
		source += " (" + a.Name + " := " + a.Term + ")"
	}
	return source + "\n", nil
}
