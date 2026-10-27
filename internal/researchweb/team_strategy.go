package researchweb

import (
	"fmt"
	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
	"os"
	"path/filepath"
)

func validateTeamStrategy(r TeamRequest) error {
	if r.FormatRepairLimit < 0 || r.FormatRepairLimit > 2 {
		return RuleError("Допустимо от нуля до двух исправлений формата на команду.")
	}
	if r.Strategy != "" && r.Strategy != "fixed" && r.Strategy != "adaptive" && r.Strategy != "demand" {
		return RuleError("Неизвестный порядок работы команды.")
	}
	if (r.Strategy == "adaptive" || r.Strategy == "demand") && !r.RequireLean {
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
	materials := []struct{ name, content string }{{"Goal.lean", g.Source}, {"Candidate.template.lean", template}}
	e, task := d.entity(a.Target), d.task(a.TaskID)
	if e != nil && task != nil {
		original, readErr := readBoundedFile(filepath.Join(dir, "TASK.md"), 65536)
		if readErr != nil && !os.IsNotExist(readErr) {
			return nil, readErr
		}
		if readErr == nil {
			name := "ResearchBackground_" + bytesDigest(original) + ".md"
			existing, readErr := readBoundedFile(filepath.Join(dir, name), 65536)
			if readErr != nil && !os.IsNotExist(readErr) {
				return nil, readErr
			}
			if readErr == nil && string(existing) != string(original) {
				return nil, RuleError("Архив описания задания не соответствует исходнику.")
			}
			materials = append(materials, struct{ name, content string }{name, string(original)})
		}
		current := fmt.Sprintf("# Текущее задание\n\nОбъект: %s, версия %d.\nВид задания: %s.\n\n## Утверждение\n\n%s\n\n## Предпосылки\n\n%s\n\n## Задача исполнителя\n\n%s\n\n## Текущая цель Lean\n\nФайл: Goal.lean\nДекларация: %s\nКандидат: %s\n\nПроверяется это обязательство. Исходные описания исследования сохранены в файлах ResearchBackground_*.md; они не заменяют текущее задание и его цель.\n", e.ID, e.Revision, task.Kind, e.Statement, e.Assumptions, task.Objective, g.Declaration, g.Candidate)
		materials = append(materials, struct{ name, content string }{"TASK.md", current})
	}
	for _, material := range materials {
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
