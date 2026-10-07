package researchweb

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
)

type IntermediateCheck struct {
	ID             string           `json:"id"`
	Attempt        string           `json:"attempt"`
	Target         string           `json:"target"`
	TargetRevision int              `json:"target_revision"`
	Purpose        string           `json:"purpose"`
	Goal           leancheck.Goal   `json:"goal"`
	Source         string           `json:"source"`
	Report         leancheck.Report `json:"report"`
}

func leanSourceArgument(raw string) (string, error) {
	var v struct {
		Source string `json:"source"`
	}
	if len(raw) > 128000 || decodeJSON([]byte(raw), &v) != nil || !textOK(v.Source, 64000) {
		return "", RuleError("Нужен только исходник кандидата до 64000 байт.")
	}
	return v.Source, nil
}

func refutationGoal(g leancheck.Goal) leancheck.Goal {
	return leancheck.Goal{Source: g.Source + "\nnamespace ResearchRefutation\ndef Statement : Prop := Not (" + g.Declaration + ")\nend ResearchRefutation\n", Declaration: "ResearchRefutation.Statement", Candidate: "ResearchRefutation.candidate"}
}

func (s *Service) leanToolContext(ctx context.Context, d Data, a Attempt) (context.Context, error) {
	p := attemptProfile(s, a)
	tools := []execution.RuntimeTool{}
	checks := 0
	var checkMu sync.Mutex
	for _, name := range leanToolNames(p) {
		if name == "read_file" {
			continue
		}
		e := d.entity(a.Target)
		if s.Options.Checker == nil || e == nil || e.FormalGoal == nil {
			return ctx, RuleError("Инструмент Lean требует закрепленную цель и проверяющую службу.")
		}
		if name == "check_refutation" && d.task(a.TaskID).Kind != "counterexample" {
			return ctx, RuleError("Проверка отрицания доступна только заданию контрпримеров.")
		}
		goal := *e.FormalGoal
		purpose := "proof"
		if name == "check_refutation" {
			goal = refutationGoal(goal)
			purpose = "refutation"
		}
		environment, _ := s.libraryEnvironment(&d, e)
		checker := s.Options.Checker
		if environment != nil {
			pinned := *environment
			if pinned.TimeoutSeconds > 60 {
				pinned.TimeoutSeconds = 60
			}
			checker = leancheck.DockerChecker{Config: pinned}
		}
		toolName := name
		tools = append(tools, execution.RuntimeTool{Name: toolName, Description: "Check a Lean candidate against the server-pinned " + purpose + " goal. Only source is accepted; compilation runs in an isolated environment and does not accept the result.", Parameters: json.RawMessage(`{"type":"object","properties":{"source":{"type":"string"}},"required":["source"],"additionalProperties":false}`), Validate: func(raw string) error { _, err := leanSourceArgument(raw); return err }, Run: func(ctx context.Context, raw string) (string, error) {
			checkMu.Lock()
			defer checkMu.Unlock()
			if checks >= 4 {
				return `{"status":"denied","reason":"intermediate_check_limit"}`, execution.ErrLimit
			}
			checks++
			source, err := leanSourceArgument(raw)
			if err != nil {
				return "", err
			}
			check := IntermediateCheck{ID: identifier("probe"), Attempt: a.ID, Target: a.Target, TargetRevision: a.TargetRevision, Purpose: purpose, Goal: goal, Source: source}
			dir := filepath.Join(s.Options.Config.DataDir, "attempts", a.ID, "toolchecks", check.ID)
			if err := os.MkdirAll(dir, 0700); err != nil {
				return "", err
			}
			limited, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			check.Report, err = checker.Check(limited, goal, source, dir)
			if check.Report.Status == "verified" && (check.Report.GoalSHA256 != leancheck.Digest(goal) || check.Report.SourceSHA256 != leancheck.Digest(source)) {
				check.Report.Status = "failed"
				check.Report.Phase = "report_integrity"
			}
			body, _ := json.Marshal(check)
			if writeErr := atomicFile(filepath.Join(dir, "report.json"), body); writeErr != nil {
				return "", writeErr
			}
			response, _ := json.Marshal(map[string]any{"check": check.ID, "purpose": purpose, "report": check.Report, "accepted": false, "remaining_checks": 4 - checks})
			if len(response) > 60000 {
				return fmt.Sprintf(`{"check":%q,"status":"failed","reason":"report_limit"}`, check.ID), execution.ErrLimit
			}
			if limited.Err() != nil {
				return string(response), limited.Err()
			}
			return string(response), nil
		}})
	}
	return execution.WithTools(ctx, tools...), nil
}

func modelHasLeanTool(p execution.Profile) bool {
	for _, name := range leanToolNames(p) {
		if strings.HasPrefix(name, "check_") {
			return true
		}
	}
	return false
}

func leanToolNames(p execution.Profile) []string {
	if p.Model != nil {
		return p.Model.Tools
	}
	if p.External != nil && p.External.Provider == "coddy-agent" {
		return p.External.Tools
	}
	return nil
}

func toolGoals(e *Entity, p execution.Profile) map[string]leancheck.Goal {
	if e == nil || e.FormalGoal == nil {
		return nil
	}
	goals := map[string]leancheck.Goal{}
	for _, name := range leanToolNames(p) {
		if name == "check_lean" {
			goals[name] = *e.FormalGoal
		}
		if name == "check_refutation" {
			goals[name] = refutationGoal(*e.FormalGoal)
		}
	}
	return goals
}
