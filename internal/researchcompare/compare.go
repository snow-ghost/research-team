package researchcompare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
)

type Case struct {
	Class string         `json:"class,omitempty"`
	ID    string         `json:"id"`
	Goal  leancheck.Goal `json:"goal"`
}
type Config struct {
	Version   int               `json:"version,omitempty"`
	Cases     []Case            `json:"cases"`
	Universal execution.Profile `json:"universal"`
	Proof     execution.Profile `json:"proof"`
	Counter   execution.Profile `json:"counter"`
	Formalize execution.Profile `json:"formalize"`
	Reviewer  execution.Profile `json:"reviewer"`
	Lean      leancheck.Config  `json:"lean"`
}
type Outcome struct {
	Outcome  string `json:"outcome"`
	Source   string `json:"source"`
	Evidence string `json:"evidence"`
}
type Run struct {
	Repetition          int               `json:"repetition,omitempty"`
	Class               string            `json:"class,omitempty"`
	TimeIncomplete      bool              `json:"time_incomplete,omitempty"`
	Case                string            `json:"case"`
	Mode                string            `json:"mode"`
	GoalSHA256          string            `json:"goal_sha256"`
	Status              string            `json:"status"`
	Outcome             string            `json:"outcome,omitempty"`
	RequestUpperBound   int               `json:"request_upper_bound"`
	MeasuredRequests    int               `json:"measured_requests"`
	UnknownRequestCount bool              `json:"unknown_request_count"`
	InputTokens         int64             `json:"input_tokens"`
	OutputTokens        int64             `json:"output_tokens"`
	UsageIncomplete     bool              `json:"usage_incomplete"`
	Seconds             float64           `json:"seconds"`
	Error               string            `json:"error,omitempty"`
	Profiles            map[string]string `json:"profiles"`
	Verification        *leancheck.Report `json:"verification,omitempty"`
	ReviewAccepted      bool              `json:"review_accepted"`
}
type Report struct {
	AuditSHA256         string      `json:"audit_sha256,omitempty"`
	ResumedAt           []time.Time `json:"resumed_at,omitempty"`
	Version             int         `json:"version"`
	StartedAt           time.Time   `json:"started_at"`
	ConfigurationSHA256 string      `json:"configuration_sha256"`
	MaxRequests         int         `json:"max_requests"`
	Runs                []Run       `json:"runs"`
}
type Runner struct {
	Resume  bool
	Build   func(execution.Profile, func(string) (string, bool)) (execution.Executor, error)
	Checker leancheck.Checker
	Lookup  func(string) (string, bool)
}

func (c Config) Validate() error {
	if c.Version != 0 && c.Version != 1 && c.Version != 2 {
		return errors.New("unsupported comparison version")
	}
	if len(c.Cases) != 6 {
		return errors.New("exactly six cases required")
	}
	seen := map[string]bool{}
	for _, item := range c.Cases {
		if item.ID == "" || strings.ContainsAny(item.ID, "/\\\x00") || seen[item.ID] || item.Goal.Validate() != nil {
			return errors.New("invalid comparison case")
		}
		seen[item.ID] = true
	}
	profiles := []execution.Profile{c.Universal, c.Proof, c.Counter, c.Formalize, c.Reviewer}
	model := ""
	endpoint := ""
	kind := c.Universal.Kind
	for i, p := range profiles {
		if err := p.Validate(); err != nil {
			return err
		}
		if p.Kind != kind {
			return errors.New("comparison must use one executor kind")
		}
		name, url := "", ""
		if p.External != nil {
			if p.External.Provider != "coddy-agent" || p.External.ExpectedVersion != "1.2.54" || p.External.ExecutionBoundary != "docker" {
				return errors.New("external comparison requires isolated pinned Coddy")
			}
			name, url = p.External.Model, p.External.BaseURL
		} else if p.Model != nil {
			name, url = p.Model.Model, p.Model.BaseURL
		} else {
			return errors.New("comparison model is missing")
		}
		if i == 0 {
			model = name
			endpoint = url
		}
		if name != model || url != endpoint || p.Limits.MaxOutputTokens != 32768 || p.Limits.TimeoutSeconds != 900 || p.Limits.MaxOutputBytes != 16<<20 {
			return errors.New("comparison profiles require equal model, endpoint and response limits")
		}
	}
	universal, formalize := 7, 5
	if kind == "model" {
		universal, formalize = 5, 3
	}
	if c.Universal.Limits.MaxSteps != universal || c.Proof.Limits.MaxSteps != 1 || c.Counter.Limits.MaxSteps != 1 || c.Formalize.Limits.MaxSteps != formalize || c.Reviewer.Limits.MaxSteps != 1 {
		return errors.New("arms require equal reserved request budgets")
	}
	if c.Reviewer.ID == c.Universal.ID || c.Reviewer.ID == c.Formalize.ID {
		return errors.New("common reviewer must be independent")
	}
	return c.Lean.Validate()
}

func RunComparison(ctx context.Context, c Config, directory string, r Runner) (Report, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return Report{}, err
	}
	directory = absolute
	if c.Version == 2 {
		return RunControlledComparison(ctx, c, directory, r)
	}
	report := Report{Version: 1, StartedAt: time.Now().UTC(), ConfigurationSHA256: leancheck.Digest(c), MaxRequests: 96, Runs: []Run{}}
	if err := c.Validate(); err != nil {
		return report, err
	}
	if r.Build == nil {
		r.Build = execution.Build
	}
	if r.Checker == nil {
		r.Checker = leancheck.DockerChecker{Config: c.Lean}
	}
	if r.Lookup == nil {
		r.Lookup = os.LookupEnv
	}
	if r.Resume {
		data, err := os.ReadFile(filepath.Join(directory, "report.json"))
		if err != nil || len(data) > 1<<20 || execution.DecodeAgentReport(string(data), &report) != nil || report.ConfigurationSHA256 != leancheck.Digest(c) || report.Version != 1 || report.MaxRequests != 96 {
			return report, errors.New("resume requires an intact report and unchanged configuration")
		}
		seen := map[string]bool{}
		for i := range report.Runs {
			run := &report.Runs[i]
			key := run.Case + ":" + run.Mode
			valid := false
			for _, item := range c.Cases {
				if item.ID == run.Case && leancheck.Digest(item.Goal) == run.GoalSHA256 {
					valid = true
				}
			}
			if !valid || seen[key] || (run.Mode != "single" && run.Mode != "team") || run.RequestUpperBound < 0 || run.RequestUpperBound > 8 {
				return report, errors.New("resume report identity mismatch")
			}
			seen[key] = true
			if run.Status == "running" {
				run.Status, run.Error = "failed", "interrupted_request_not_replayed"
				run.UnknownRequestCount, run.UsageIncomplete, run.TimeIncomplete = true, true, true
			}
		}
		report.ResumedAt = append(report.ResumedAt, time.Now().UTC())
	} else if err := os.Mkdir(directory, 0700); err != nil {
		return report, errors.New("comparison destination must be new; prior requests cannot be replayed")
	}
	write := func() error {
		body, _ := json.MarshalIndent(report, "", "  ")
		return os.WriteFile(filepath.Join(directory, "report.json"), body, 0600)
	}
	if err := write(); err != nil {
		return report, err
	}
	for i, item := range c.Cases {
		modes := []string{"single", "team"}
		if i%2 == 1 {
			modes = []string{"team", "single"}
		}
		for _, mode := range modes {
			if ctx.Err() != nil {
				return report, ctx.Err()
			}
			previous := false
			for _, run := range report.Runs {
				if run.Case == item.ID && run.Mode == mode {
					previous = true
				}
			}
			if previous {
				continue
			}
			run := Run{Case: item.ID, Mode: mode, GoalSHA256: leancheck.Digest(item.Goal), Status: "running", Profiles: map[string]string{}}
			report.Runs = append(report.Runs, run)
			if err := write(); err != nil {
				return report, err
			}
			index := len(report.Runs) - 1
			update := func(v Run) error { report.Runs[index] = v; return write() }
			err := r.run(ctx, c, item, mode, filepath.Join(directory, fmt.Sprintf("%02d-%s-%s", i, item.ID, mode)), &run, update)
			if err != nil {
				run.Status = "failed"
				run.Error = err.Error()
			}
			report.Runs[index] = run
			if err := write(); err != nil {
				return report, err
			}
		}
	}
	return report, nil
}

func negative(g leancheck.Goal) leancheck.Goal {
	return leancheck.Goal{Source: g.Source + "\nnamespace ResearchRefutation\ndef Statement : Prop := Not (" + g.Declaration + ")\nend ResearchRefutation\n", Declaration: "ResearchRefutation.Statement", Candidate: "ResearchRefutation.candidate"}
}

const outcomeFormat = `Return only JSON {"outcome":"proof|refutation|inconclusive","source":"complete Lean source importing Goal","evidence":"mathematical reasoning and checked cases"}. Use the exact candidate name from tool_goals. Do not change assumptions. No sorry, axiom or native_decide. A finite search without a counterexample is not a proof.`

func (r Runner) run(ctx context.Context, c Config, item Case, mode, dir string, run *Run, persist func(Run) error) (err error) {
	start := time.Now()
	defer func() { run.Seconds = time.Since(start).Seconds() }()
	if err = os.Mkdir(dir, 0700); err != nil {
		return errors.New("cannot create private comparison artifacts")
	}
	invoke := func(role string, p execution.Profile, objective string, previous any) (execution.Result, error) {
		work := filepath.Join(dir, role)
		if err := os.Mkdir(work, 0700); err != nil {
			return execution.Result{}, err
		}
		if err := os.WriteFile(filepath.Join(work, "Goal.lean"), []byte(item.Goal.Source), 0600); err != nil {
			return execution.Result{}, err
		}
		limit := 8
		if c.Version == 2 {
			limit = 6
			remaining := limit - run.RequestUpperBound
			if role != "review" {
				remaining--
			}
			if p.Limits.MaxSteps > remaining {
				p.Limits.MaxSteps = remaining
			}
			if p.Limits.MaxSteps < 1 {
				return execution.Result{}, errors.New("comparison request budget exhausted")
			}
		}
		run.RequestUpperBound += p.Limits.MaxSteps
		if run.RequestUpperBound > limit {
			return execution.Result{}, errors.New("comparison request budget exhausted")
		}
		run.Profiles[role] = leancheck.Digest(p)
		// Persist the reservation before the process can contact a model. Never retry this attempt.
		if err := persist(*run); err != nil {
			return execution.Result{}, err
		}
		positiveTemplate, _ := leancheck.CandidateTemplate(item.Goal)
		negativeTemplate, _ := leancheck.CandidateTemplate(negative(item.Goal))
		contextData, _ := json.Marshal(map[string]any{"goal": item.Goal, "candidate_templates": map[string]string{"proof": positiveTemplate, "refutation": negativeTemplate}, "tool_goals": map[string]leancheck.Goal{"check_lean": item.Goal, "check_refutation": negative(item.Goal)}, "previous_unverified": previous})
		task := execution.Task{ID: item.ID + "-" + mode + "-" + role, AttemptID: item.ID + "-" + mode + "-" + role, Snapshot: leancheck.Digest(string(contextData)), LeaseEpoch: 1, Workspace: work, Objective: objective, Context: string(contextData)}
		checks := 0
		handlers := []execution.RuntimeTool{}
		names := []string{}
		if p.External != nil {
			names = p.External.Tools
		} else if p.Model != nil {
			names = p.Model.Tools
		}
		for _, name := range names {
			goal := item.Goal
			if name == "check_refutation" {
				goal = negative(goal)
			}
			handlers = append(handlers, execution.RuntimeTool{Name: name, Description: "Check source against the immutable comparison goal; no acceptance.", Parameters: json.RawMessage(`{"type":"object","properties":{"source":{"type":"string"}},"required":["source"],"additionalProperties":false}`), Validate: func(raw string) error {
				var a struct {
					Source string `json:"source"`
				}
				d := json.NewDecoder(strings.NewReader(raw))
				d.DisallowUnknownFields()
				if d.Decode(&a) != nil || len(a.Source) == 0 || len(a.Source) > 64000 {
					return execution.ErrProtocol
				}
				return nil
			}, Run: func(ctx context.Context, raw string) (string, error) {
				checks++
				if checks > 4 {
					return "check limit", execution.ErrLimit
				}
				var a struct{ Source string }
				_ = json.Unmarshal([]byte(raw), &a)
				checkDir := filepath.Join(work, fmt.Sprintf("check-%d", checks))
				_ = os.Mkdir(checkDir, 0700)
				limited, cancel := context.WithTimeout(ctx, 60*time.Second)
				defer cancel()
				report, err := r.Checker.Check(limited, goal, a.Source, checkDir)
				if report.Status == "verified" && (report.GoalSHA256 != leancheck.Digest(goal) || report.SourceSHA256 != leancheck.Digest(a.Source)) {
					return "report integrity mismatch", execution.ErrProtocol
				}
				body, _ := json.Marshal(map[string]any{"report": report, "accepted": false})
				return string(body), err
			}})
		}
		executor, err := r.Build(p, r.Lookup)
		if err != nil {
			return execution.Result{}, errors.New("executor configuration rejected")
		}
		result, err := executor.Run(execution.WithTools(ctx, handlers...), task)
		body, _ := json.Marshal(result)
		if os.WriteFile(filepath.Join(work, "result.json"), body, 0600) != nil {
			return result, errors.New("cannot save comparison result")
		}
		measured := 0
		for _, e := range result.Events {
			if e.Type == "model_requested" {
				measured++
			}
		}
		run.MeasuredRequests += measured
		if c.Version == 2 && measured > 0 && measured <= p.Limits.MaxSteps {
			run.RequestUpperBound -= p.Limits.MaxSteps - measured
		}
		if measured == 0 {
			run.UnknownRequestCount = true
		}
		if result.Usage != nil {
			run.InputTokens += result.Usage.InputTokens
			run.OutputTokens += result.Usage.OutputTokens
			run.UsageIncomplete = run.UsageIncomplete || result.Usage.Incomplete
		} else {
			run.UsageIncomplete = true
		}
		if err != nil {
			return result, errors.New("executor failed; see private result diagnostics")
		}
		return result, nil
	}
	parse := func(text string) (Outcome, error) {
		var o Outcome
		if execution.DecodeAgentReport(text, &o) != nil || (o.Outcome != "proof" && o.Outcome != "refutation" && o.Outcome != "inconclusive") {
			return o, errors.New("invalid typed comparison outcome")
		}
		return o, nil
	}
	var selected Outcome
	if mode == "single" {
		res, e := invoke("universal", c.Universal, "Search counterexamples, choose a method, prove or refute, and formalize the exact goal. "+outcomeFormat, nil)
		if e != nil {
			return e
		}
		selected, err = parse(res.Candidate)
	} else {
		var proof execution.Result
		if mode != "adaptive" {
			var e error
			proof, e = invoke("proof", c.Proof, "Analyze and propose a proof of the exact goal, including assumptions and boundary cases. Return concise mathematical reasoning; no acceptance.", nil)
			if e != nil {
				return e
			}
		}
		counter, e := invoke("counter", c.Counter, "Independently search counterexamples. If found, give a formal refutation. Otherwise return inconclusive with evidence. "+outcomeFormat, nil)
		if e != nil {
			return e
		}
		candidate, e := parse(counter.Candidate)
		if e != nil {
			return e
		}
		if candidate.Outcome == "refutation" && c.Version != 2 {
			selected = candidate
		} else {
			if mode == "adaptive" && candidate.Outcome != "refutation" {
				proof, e = invoke("proof", c.Proof, "Search a proof of the exact goal. Use induction, representation change, decomposition or specialization when justified; specify obligations. Return reasoning, not acceptance.", counter.Candidate)
				if e != nil {
					return e
				}
			}
			formal, e := invoke("formalize", c.Formalize, "Formalize a proof or refutation. Treat prior reports as unverified and check assumptions. "+outcomeFormat, map[string]string{"proof": proof.Candidate, "counter": counter.Candidate})
			if e != nil {
				return e
			}
			selected, err = parse(formal.Candidate)
		}
	}
	if err != nil {
		return err
	}
	run.Outcome = selected.Outcome
	if selected.Outcome == "inconclusive" {
		run.Status = "inconclusive"
		return nil
	}
	goal := item.Goal
	if selected.Outcome == "refutation" {
		goal = negative(goal)
	}
	if len(selected.Source) == 0 || len(selected.Source) > 64000 {
		return errors.New("candidate source absent or oversized")
	}
	var verification leancheck.Report
	for repair := 0; ; repair++ {
		checkDir := filepath.Join(dir, fmt.Sprintf("verification-%d", repair))
		_ = os.Mkdir(checkDir, 0700)
		limited, cancel := context.WithTimeout(ctx, 90*time.Second)
		verification, err = r.Checker.Check(limited, goal, selected.Source, checkDir)
		cancel()
		run.Verification = &verification
		body, _ := json.Marshal(verification)
		if e := os.WriteFile(filepath.Join(checkDir, "report.json"), body, 0600); e != nil {
			return e
		}
		if err == nil && verification.Status == "verified" && verification.GoalSHA256 == leancheck.Digest(goal) && verification.SourceSHA256 == leancheck.Digest(selected.Source) && (c.Version != 2 || verification.AuditSHA256 == leancheck.AuditDigest()) {
			break
		}
		if c.Version != 2 || run.RequestUpperBound >= 5 || ctx.Err() != nil {
			return errors.New("final Lean verification failed")
		}
		profile := c.Formalize
		if mode == "single" {
			profile = c.Universal
		}
		result, e := invoke(fmt.Sprintf("repair-%d", repair), profile, "Repair the candidate using the native Lean diagnostic. Preserve the exact imported statement. Change method only with justification. "+outcomeFormat, map[string]any{"outcome": selected, "report": verification})
		if e != nil {
			return e
		}
		selected, err = parse(result.Candidate)
		if err != nil || selected.Outcome == "inconclusive" || len(selected.Source) == 0 || len(selected.Source) > 64000 {
			return errors.New("invalid repaired candidate")
		}
		run.Outcome = selected.Outcome
		goal = item.Goal
		if selected.Outcome == "refutation" {
			goal = negative(goal)
		}
	}
	review, e := invoke("review", c.Reviewer, `Independently review the exact goal, source, evidence and checker report. Return only JSON {"summary":"...","findings":[{"severity":"major|question|editorial","text":"..."}]}. Do not claim to have run tools.`, map[string]any{"goal": goal, "original_goal": item.Goal, "outcome": selected, "report": verification})
	if e != nil {
		return e
	}
	var findings struct {
		Summary  string `json:"summary"`
		Findings []struct {
			Severity string `json:"severity"`
			Text     string `json:"text"`
		} `json:"findings"`
	}
	if execution.DecodeAgentReport(review.Candidate, &findings) != nil || findings.Summary == "" || len(findings.Findings) > 100 {
		return errors.New("invalid independent review")
	}
	for _, f := range findings.Findings {
		if (f.Severity != "major" && f.Severity != "question" && f.Severity != "editorial") || f.Text == "" {
			return errors.New("invalid review finding")
		}
		if f.Severity == "major" || f.Severity == "question" {
			run.Status = "requires_review"
			return nil
		}
	}
	run.ReviewAccepted = true
	run.Status = "verified_not_accepted"
	return nil
}
