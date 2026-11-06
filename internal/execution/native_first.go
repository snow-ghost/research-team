package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

type NativeAttempt struct {
	Kind         string `json:"kind"`
	ID           string `json:"id"`
	Source       string `json:"source"`
	Status       string `json:"status"`
	Phase        string `json:"phase"`
	Diagnostics  string `json:"diagnostics,omitempty"`
	SourceSHA256 string `json:"source_sha256"`
}

type NativeReport struct {
	GoalSHA256          string           `json:"goal_sha256"`
	SourceSHA256        string           `json:"source_sha256"`
	Trials              int              `json:"trials"`
	LibraryApplications int              `json:"library_applications"`
	Attempts            []NativeAttempt  `json:"attempts,omitempty"`
	Verification        leancheck.Report `json:"verification"`
	ModelFallback       bool             `json:"model_fallback"`
}

type nativeLibraryMatch struct {
	Status   string               `json:"status"`
	Source   string               `json:"source"`
	Theorem  string               `json:"theorem"`
	Binders  []leancheck.Binder   `json:"binders"`
	Bindings []leancheck.Argument `json:"bindings"`
}

type nativeLibrarySearch struct {
	Matches []nativeLibraryMatch `json:"matches"`
}

type nativeFirstExecutor struct {
	profile  Profile
	checker  leancheck.Checker
	fallback Executor
}

func NewNativeFirstExecutor(profile Profile, checker leancheck.Checker, fallback Executor) Executor {
	return &nativeFirstExecutor{profile: profile, checker: checker, fallback: fallback}
}

func nativeAttempt(kind, id, source string, checked leancheck.Report) NativeAttempt {
	return NativeAttempt{Kind: kind, ID: id, Source: Preview(source, 32000), Status: checked.Status,
		Phase: checked.Phase, Diagnostics: Preview(checked.Diagnostics, 4000), SourceSHA256: checked.SourceSHA256}
}

func nativeFallbackTask(task Task, report *NativeReport, matches []nativeLibraryMatch) Task {
	var packet map[string]any
	if json.Unmarshal([]byte(task.Context), &packet) != nil {
		return task
	}
	packet["native"] = report
	hints := []map[string]any{}
	for _, match := range matches {
		if (match.Status != "verified" && match.Status != "obligations") || match.Theorem == "" {
			continue
		}
		hints = append(hints, map[string]any{
			"theorem": match.Theorem, "source": Preview(match.Source, 16000),
			"binders": match.Binders, "bindings": match.Bindings,
		})
	}
	if len(hints) > 0 {
		packet["native_application_hint"] = map[string]any{
			"matches":     hints,
			"instruction": "Apply a library theorem using the listed binders and named arguments. Keep an equality as an equality; do not use it as a function.",
		}
	}
	encoded, err := json.Marshal(packet)
	if err != nil {
		return task
	}
	task.Context = string(encoded)
	return task
}

func (e *nativeFirstExecutor) Run(ctx context.Context, task Task) (Result, error) {
	base, _, err := prepare(e.profile, task)
	if err != nil {
		return base, err
	}
	var packet struct {
		ToolGoals     map[string]leancheck.Goal `json:"tool_goals"`
		LibrarySearch nativeLibrarySearch       `json:"library_search"`
		Entities      []struct {
			FormalGoal *leancheck.Goal `json:"formal_goal"`
		} `json:"entities"`
		Team struct {
			Role     string `json:"role"`
			Refuting bool   `json:"refuting"`
		} `json:"team_materials"`
	}
	if e.checker == nil || e.fallback == nil || json.Unmarshal([]byte(task.Context), &packet) != nil {
		return e.fallback.Run(ctx, task)
	}
	if packet.Team.Role != "" && packet.Team.Role != "proof" && packet.Team.Role != "formalize" {
		return e.fallback.Run(ctx, task)
	}
	goal, ok := packet.ToolGoals["check_lean"]
	outcome := "proof"
	if packet.Team.Refuting {
		goal, ok = packet.ToolGoals["check_refutation"]
		outcome = "refutation"
	}
	if !ok {
		for _, entity := range packet.Entities {
			if entity.FormalGoal == nil {
				continue
			}
			goal = *entity.FormalGoal
			if packet.Team.Refuting {
				goal = leancheck.Goal{Source: goal.Source + "\nnamespace ResearchRefutation\ndef Statement : Prop := Not (" + goal.Declaration + ")\nend ResearchRefutation\n", Declaration: "ResearchRefutation.Statement", Candidate: "ResearchRefutation.candidate"}
			}
			ok = true
			break
		}
	}
	if !ok {
		return e.fallback.Run(ctx, task)
	}
	trials, err := leancheck.TacticTrials(goal)
	if err != nil {
		return e.fallback.Run(ctx, task)
	}
	work, err := os.MkdirTemp("", "research-native-first-")
	if err != nil {
		return e.fallback.Run(ctx, task)
	}
	defer os.RemoveAll(work)
	report := &NativeReport{GoalSHA256: leancheck.Digest(goal)}
	nativeEvents := []Event{}
	if !packet.Team.Refuting {
		for i, match := range packet.LibrarySearch.Matches {
			if match.Status != "verified" || match.Theorem == "" {
				continue
			}
			source := match.Source
			if source == "" {
				var err error
				source, err = leancheck.ApplicationSource(goal, match.Theorem, match.Binders, match.Bindings)
				if err != nil {
					continue
				}
			}
			trialDir := filepath.Join(work, fmt.Sprintf("library-%d", i))
			if err := os.Mkdir(trialDir, 0700); err != nil {
				return e.fallback.Run(ctx, task)
			}
			checked, _ := e.checker.Check(ctx, goal, source, trialDir)
			report.Trials++
			report.LibraryApplications++
			report.Verification = checked
			report.SourceSHA256 = checked.SourceSHA256
			report.Attempts = append(report.Attempts, nativeAttempt("library", match.Theorem, source, checked))
			nativeEvents = append(nativeEvents, Event{Type: "native_library_check", Step: report.Trials, Status: checked.Status})
			if checked.Status != "verified" || checked.GoalSHA256 != leancheck.Digest(goal) || checked.SourceSHA256 != leancheck.Digest(source) || checked.AuditSHA256 != leancheck.AuditDigest() {
				continue
			}
			candidate, _ := json.Marshal(map[string]string{
				"outcome": outcome, "source": source,
				"evidence": "Native library application; no model call",
			})
			base.Status = "candidate"
			base.Candidate = string(candidate)
			base.RemoteOutcome = "not_started"
			base.Usage = &Usage{Source: "native"}
			base.Events = append(nativeEvents, Event{Type: "candidate"})
			base.Native = report
			return base, nil
		}
	}
	for i, trial := range trials {
		trialDir := filepath.Join(work, fmt.Sprintf("trial-%d", i))
		if err := os.Mkdir(trialDir, 0700); err != nil {
			return e.fallback.Run(ctx, task)
		}
		checked, _ := e.checker.Check(ctx, goal, trial.Source, trialDir)
		report.Trials++
		report.Verification = checked
		report.SourceSHA256 = checked.SourceSHA256
		report.Attempts = append(report.Attempts, nativeAttempt("tactic", trial.ID, trial.Source, checked))
		nativeEvents = append(nativeEvents, Event{Type: "native_check", Step: i + 1, Status: checked.Status})
		if checked.Status != "verified" || checked.GoalSHA256 != leancheck.Digest(goal) || checked.SourceSHA256 != leancheck.Digest(trial.Source) || checked.AuditSHA256 != leancheck.AuditDigest() {
			continue
		}
		candidate, _ := json.Marshal(map[string]string{
			"outcome": outcome, "source": trial.Source,
			"evidence": "Native tactic portfolio; no model call",
		})
		base.Status = "candidate"
		base.Candidate = string(candidate)
		base.RemoteOutcome = "not_started"
		base.Usage = &Usage{Source: "native"}
		base.Events = append(nativeEvents, Event{Type: "candidate"})
		base.Native = report
		return base, nil
	}
	result, runErr := e.fallback.Run(ctx, nativeFallbackTask(task, report, packet.LibrarySearch.Matches))
	report.ModelFallback = true
	result.Native = report
	result.Events = append(nativeEvents, result.Events...)
	return result, runErr
}
