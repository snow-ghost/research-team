package researchweb

import (
	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
	"path/filepath"
)

func validateTeamStrategy(r TeamRequest) error {
	if r.Strategy != "" && r.Strategy != "fixed" && r.Strategy != "adaptive" {
		return RuleError("Неизвестный порядок работы команды.")
	}
	if r.Strategy == "adaptive" && !r.RequireLean {
		return RuleError("Адаптивный цикл требует закрепленной цели Lean.")
	}
	if len(r.Methods) > len(researchMethods) {
		return RuleError("Слишком много методов.")
	}
	seen := map[string]bool{}
	for _, id := range r.Methods {
		if _, ok := researchMethod(id); !ok || seen[id] {
			return RuleError("Нужны различные методы из каталога.")
		}
		seen[id] = true
	}
	return nil
}

func prepareGoalFiles(d Data, a Attempt, dir string, files []FileDigest) ([]FileDigest, error) {
	g := attemptFormalGoal(&d, a)
	if g == nil {
		return files, nil
	}
	template, err := leancheck.CandidateTemplate(*g)
	if err != nil {
		return nil, err
	}
	for _, material := range []struct{ name, content string }{{"Goal.lean", g.Source}, {"Candidate.template.lean", template}} {
		path := filepath.Join(dir, material.name)
		if err := atomicFile(path, []byte(material.content)); err != nil {
			return nil, err
		}
		digest, err := fileSHA(path)
		if err != nil {
			return nil, err
		}
		item := FileDigest{Path: material.name, SHA256: digest, Bytes: len(material.content)}
		found := false
		for i := range files {
			if files[i].Path == material.name {
				files[i], found = item, true
			}
		}
		if !found {
			files = append(files, item)
		}
	}
	return files, nil
}

func attemptFormalGoal(d *Data, a Attempt) *leancheck.Goal {
	e := d.entity(a.Target)
	if e == nil || e.FormalGoal == nil {
		return nil
	}
	g := *e.FormalGoal
	if team := d.team(a.TeamID); team != nil && team.Refuting && a.Role == "formalize" {
		g = refutationGoal(g)
	}
	return &g
}

func candidateTemplateFor(d Data, a Attempt) string {
	if g := attemptFormalGoal(&d, a); g != nil {
		template, _ := leancheck.CandidateTemplate(*g)
		return template
	}
	return ""
}

func attemptToolGoals(d Data, a Attempt, p execution.Profile) map[string]leancheck.Goal {
	goals := toolGoals(d.entity(a.Target), p)
	if g := attemptFormalGoal(&d, a); g != nil {
		if _, ok := goals["check_lean"]; ok {
			goals["check_lean"] = *g
		}
	}
	return goals
}
