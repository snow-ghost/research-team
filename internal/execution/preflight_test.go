package execution

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCandidatePreflight(t *testing.T) {
	rule := CandidateRule{Candidate: "Candidate202", Declaration: "Research202", Theorem: "Wrapped4.sixUniqueFarOps_correct", Outcome: "proof"}
	valid := `{"outcome":"proof","source":"import Goal\ntheorem Candidate202 : Research202 := by\n  intro n hn a b c d e f x\n  exact Wrapped4.sixUniqueFarOps_correct hn a b c d e f x","evidence":"library application"}`
	if err := ValidateCandidate(valid, rule); err != nil {
		t.Fatal(err)
	}
	for name, source := range map[string]string{
		"mathlib":       "import Mathlib\ntheorem Candidate202 : Research202 := by\n  intro n hn a b c d e f x\n  exact Wrapped4.sixUniqueFarOps_length hn",
		"extra_import":  "import Goal\nimport Library\ntheorem Candidate202 : Research202 := by\n  intro n hn a b c d e f x\n  exact Wrapped4.sixUniqueFarOps_correct hn a b c d e f x",
		"missing_intro": "import Goal\ntheorem Candidate202 : Research202 := by\n  exact Wrapped4.sixUniqueFarOps_correct hn a b c d e f x",
		"coq":           "import Goal\ntheorem Candidate202 : Research202 := by\n  intro n hn a b c d e f x\n  exact Wrapped4.sixUniqueFarOps_correct hn a b c d e f x\nQed.",
		"wrong_theorem": "import Goal\ntheorem Candidate202 : Research202 := by\n  intro n hn a b c d e f x\n  exact Wrapped4.sixUniqueFarOps_length hn",
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateCandidate(`{"outcome":"proof","source":`+strconvQuote(source)+`,"evidence":"x"}`, rule); err == nil {
				t.Fatal("invalid candidate accepted")
			}
		})
	}
	if err := ValidateCandidate(`{"outcome":"proof","source":"import Goal\n","evidence":"x","extra":1}`, rule); err == nil {
		t.Fatal("extra field accepted")
	}
}

func strconvQuote(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

type preflightExecutor struct {
	results []Result
	errs    []error
	tasks   []Task
}

func (e *preflightExecutor) Run(_ context.Context, task Task) (Result, error) {
	e.tasks = append(e.tasks, task)
	index := len(e.tasks) - 1
	if index >= len(e.results) {
		index = len(e.results) - 1
	}
	var err error
	if index < len(e.errs) {
		err = e.errs[index]
	}
	return e.results[index], err
}

func TestRunCandidatePreflightAllowsOnlyOneRepair(t *testing.T) {
	rule := CandidateRule{Candidate: "Candidate202", Declaration: "Research202", Theorem: "Wrapped4.sixUniqueFarOps_correct", Outcome: "proof"}
	valid := `{"outcome":"proof","source":"import Goal\ntheorem Candidate202 : Research202 := by\n  intro n hn a b c d e f x\n  exact Wrapped4.sixUniqueFarOps_correct hn a b c d e f x","evidence":"library application"}`
	invalid := `{"outcome":"proof"}`
	task := Task{ID: "task", AttemptID: "attempt", Context: "original"}

	t.Run("accepted after one repair", func(t *testing.T) {
		executor := &preflightExecutor{results: []Result{
			{Status: "candidate", Candidate: invalid, Native: &NativeReport{Trials: 6}, Usage: &Usage{InputTokens: 3, OutputTokens: 4}, Events: []Event{{Type: "first"}}},
			{Status: "candidate", Candidate: valid, Usage: &Usage{InputTokens: 5, OutputTokens: 6}, Events: []Event{{Type: "second"}}},
		}}
		result, err := RunCandidatePreflight(context.Background(), executor, task, rule, true)
		if err != nil || result.Status != "candidate" || result.Candidate != valid {
			t.Fatalf("repair did not preserve the valid candidate: %+v %v", result, err)
		}
		if result.Native == nil || result.Native.Trials != 6 {
			t.Fatalf("native report was lost during repair: %+v", result.Native)
		}
		if len(executor.tasks) != 2 || executor.tasks[1].AttemptID != "attempt-preflight-repair" {
			t.Fatalf("invalid repair call: %+v", executor.tasks)
		}
		start := strings.Index(executor.tasks[1].Context, `{"previous_answer":`)
		var packet struct {
			PreviousAnswer string `json:"previous_answer"`
			Error          string `json:"error"`
		}
		if start < 0 || json.Unmarshal([]byte(executor.tasks[1].Context[start:]), &packet) != nil || packet.PreviousAnswer != invalid || !strings.Contains(packet.Error, "candidate requires exactly") {
			t.Fatalf("invalid repair packet: %+v", executor.tasks[1].Context)
		}
		if result.Preflight == nil || result.Preflight.ExecutorCalls != 2 || !result.Preflight.RepairUsed || len(result.Preflight.Attempts) != 2 || result.Preflight.Attempts[0].Candidate != invalid || result.Preflight.Attempts[1].Candidate != valid {
			t.Fatalf("candidate history was not preserved: %+v", result.Preflight)
		}
		if result.Usage == nil || result.Usage.InputTokens != 8 || result.Usage.OutputTokens != 10 || result.Usage.Incomplete || len(result.Events) != 2 {
			t.Fatalf("usage or events were not combined: %+v %+v", result.Usage, result.Events)
		}
	})

	t.Run("repair disabled", func(t *testing.T) {
		executor := &preflightExecutor{results: []Result{{Status: "candidate", Candidate: invalid}}}
		result, err := RunCandidatePreflight(context.Background(), executor, task, rule, false)
		if err != nil || result.Status != "failed" || len(executor.tasks) != 1 || result.Preflight.ExecutorCalls != 1 || result.Preflight.RepairUsed {
			t.Fatalf("unexpected repair: %+v %v", result, err)
		}
	})

	t.Run("second rejection is final", func(t *testing.T) {
		executor := &preflightExecutor{results: []Result{{Status: "candidate", Candidate: invalid}, {Status: "candidate", Candidate: invalid}}}
		result, err := RunCandidatePreflight(context.Background(), executor, task, rule, true)
		if err != nil || result.Status != "failed" || len(executor.tasks) != 2 || result.Preflight.ExecutorCalls != 2 || len(result.Preflight.Attempts) != 2 {
			t.Fatalf("repair was not bounded: %+v %v", result, err)
		}
	})

	t.Run("valid first response needs no repair", func(t *testing.T) {
		executor := &preflightExecutor{results: []Result{{Status: "candidate", Candidate: valid}}}
		result, err := RunCandidatePreflight(context.Background(), executor, task, rule, true)
		if err != nil || result.Status != "candidate" || len(executor.tasks) != 1 || result.Preflight.RepairUsed {
			t.Fatalf("valid response triggered repair: %+v %v", result, err)
		}
	})

	t.Run("transport failure is not repaired", func(t *testing.T) {
		executor := &preflightExecutor{results: []Result{{Status: "failed"}}, errs: []error{errors.New("transport")}}
		result, err := RunCandidatePreflight(context.Background(), executor, task, rule, true)
		if err == nil || result.Status != "failed" || len(executor.tasks) != 1 || result.Preflight.RepairUsed {
			t.Fatalf("transport failure triggered repair: %+v %v", result, err)
		}
	})
}
