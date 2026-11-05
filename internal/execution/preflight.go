package execution

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

type CandidateRule struct {
	Candidate   string `json:"candidate"`
	Declaration string `json:"declaration"`
	Theorem     string `json:"theorem"`
	Outcome     string `json:"outcome"`
}

type PreflightAttempt struct {
	Candidate   string       `json:"candidate"`
	Status      string       `json:"status"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	Usage       *Usage       `json:"usage"`
}

type PreflightReport struct {
	Attempts      []PreflightAttempt `json:"attempts"`
	ExecutorCalls int                `json:"executor_calls"`
	RepairUsed    bool               `json:"repair_used"`
}

func copyUsage(usage *Usage) *Usage {
	if usage == nil {
		return nil
	}
	copy := *usage
	return &copy
}

func combinedUsage(first, second *Usage) *Usage {
	if first == nil && second == nil {
		return nil
	}
	out := &Usage{Source: "preflight_combined", Incomplete: first == nil || second == nil}
	for _, usage := range []*Usage{first, second} {
		if usage == nil {
			continue
		}
		out.InputTokens += usage.InputTokens
		out.OutputTokens += usage.OutputTokens
		out.Incomplete = out.Incomplete || usage.Incomplete
	}
	return out
}

func RunCandidatePreflight(ctx context.Context, executor Executor, task Task, rule CandidateRule, repair bool) (Result, error) {
	result, runErr := executor.Run(ctx, task)
	report := &PreflightReport{ExecutorCalls: 1, Attempts: []PreflightAttempt{}}
	validationErr := ValidateCandidate(result.Candidate, rule)
	record := func(candidate string, usage *Usage, err error) {
		status := "accepted"
		if err != nil {
			status = "rejected"
		}
		report.Attempts = append(report.Attempts, PreflightAttempt{Candidate: candidate, Status: status, Usage: copyUsage(usage)})
	}
	record(result.Candidate, result.Usage, validationErr)
	if validationErr == nil {
		result.Preflight = report
		return result, runErr
	}
	result.Status = "failed"
	result.Diagnostics = append(result.Diagnostics, Diagnostic{Source: "agent", Stage: "preflight", Kind: "preflight_rejected", Message: validationErr.Error()})
	if !repair || runErr != nil || result.Candidate == "" {
		result.Preflight = report
		return result, runErr
	}
	report.RepairUsed = true
	report.ExecutorCalls++
	repairTask := task
	repairTask.AttemptID += "-preflight-repair"
	packet, _ := json.Marshal(struct {
		PreviousAnswer string        `json:"previous_answer"`
		Error          string        `json:"error"`
		Contract       CandidateRule `json:"contract"`
		Instruction    string        `json:"instruction"`
	}{result.Candidate, validationErr.Error(), rule, "Return one corrected JSON object only. Do not add fields, imports, tactics or outcomes beyond the contract."})
	repairTask.Context = strings.TrimSpace(task.Context) + "\n\nPreflight repair request; this is the only permitted repair:\n" + string(packet)
	repaired, repairErr := executor.Run(ctx, repairTask)
	repairValidationErr := ValidateCandidate(repaired.Candidate, rule)
	record(repaired.Candidate, repaired.Usage, repairValidationErr)
	repaired.Events = append(result.Events, repaired.Events...)
	repaired.Diagnostics = append(result.Diagnostics, repaired.Diagnostics...)
	repaired.Usage = combinedUsage(result.Usage, repaired.Usage)
	if repaired.Native == nil {
		repaired.Native = result.Native
	}
	if repairValidationErr != nil {
		repaired.Status = "failed"
		repaired.Diagnostics = append(repaired.Diagnostics, Diagnostic{Source: "agent", Stage: "preflight", Kind: "preflight_rejected", Message: repairValidationErr.Error()})
	}
	repaired.Preflight = report
	return repaired, repairErr
}

var tacticPattern = regexp.MustCompile(`\b(?:intro|exact|omega|simp|norm_num|unfold|decide|apply|refine|rw|simpa|aesop|linarith)\b`)

func ValidateCandidate(raw string, rule CandidateRule) error {
	var answer map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &answer); err != nil {
		return errors.New("candidate must be one JSON object")
	}
	if len(answer) != 3 || answer["outcome"] == nil || answer["source"] == nil || answer["evidence"] == nil {
		return errors.New("candidate requires exactly outcome, source and evidence")
	}
	var outcome, source, evidence string
	if json.Unmarshal(answer["outcome"], &outcome) != nil || json.Unmarshal(answer["source"], &source) != nil || json.Unmarshal(answer["evidence"], &evidence) != nil {
		return errors.New("candidate fields must be strings")
	}
	expectedOutcome := rule.Outcome
	if expectedOutcome == "" {
		expectedOutcome = "proof"
	}
	if outcome != expectedOutcome {
		return errors.New("unexpected candidate outcome")
	}
	if !strings.HasPrefix(source, "import Goal\n") {
		return errors.New("source must start with import Goal")
	}
	if strings.Contains(source, "\nimport ") {
		return errors.New("only import Goal is allowed")
	}
	for _, token := range []string{"Mathlib", "Proof.", "Qed.", "sorry", "native_decide", "admit", "axiom", "```"} {
		if strings.Contains(source, token) {
			return errors.New("forbidden source token")
		}
	}
	signature := "theorem " + rule.Candidate + " : " + rule.Declaration + " := by"
	if rule.Candidate == "" || rule.Declaration == "" || rule.Theorem == "" || !strings.Contains(source, signature) {
		return errors.New("missing exact theorem signature or library theorem")
	}
	if !strings.Contains(source, rule.Theorem) {
		return errors.New("library theorem is not used")
	}
	if expectedOutcome == "proof" && !strings.Contains(source, "intro ") {
		return errors.New("variables must be introduced before exact")
	}
	allowed := map[string]bool{"intro": true, "exact": true, "omega": true}
	if expectedOutcome == "refutation" {
		allowed = map[string]bool{"unfold": true, "decide": true}
	}
	for _, tactic := range tacticPattern.FindAllString(source, -1) {
		if !allowed[tactic] {
			return errors.New("tactic is not allowed")
		}
	}
	if len(strings.Fields(evidence)) > 40 {
		return errors.New("evidence exceeds 40 words")
	}
	return nil
}
