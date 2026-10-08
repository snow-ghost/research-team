package researchcompare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
)

func saveReport(dir string, report Report) error {
	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "report.json.tmp"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(body)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), filepath.Join(dir, "report.json"))
}

// Version 2 freezes the audit and uses one wall-clock and request budget per arm.
func RunControlledComparison(ctx context.Context, c Config, dir string, r Runner) (Report, error) {
	report := Report{Version: 2, StartedAt: time.Now().UTC(), ConfigurationSHA256: leancheck.Digest(c), AuditSHA256: leancheck.AuditDigest(), MaxRequests: 216, Runs: []Run{}}
	if err := c.Validate(); err != nil {
		return report, err
	}
	if c.Universal.Kind != "model" {
		return report, errors.New("controlled comparison requires model executors")
	}
	classes := map[string]bool{}
	for _, item := range c.Cases {
		if item.Class == "" {
			return report, errors.New("task class required")
		}
		classes[item.Class] = true
	}
	if len(classes) < 4 {
		return report, errors.New("at least four task classes required")
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
		body, err := os.ReadFile(filepath.Join(dir, "report.json"))
		if err != nil || len(body) > 2<<20 || execution.DecodeAgentReport(string(body), &report) != nil || report.Version != 2 || report.MaxRequests != 216 || report.ConfigurationSHA256 != leancheck.Digest(c) || report.AuditSHA256 != leancheck.AuditDigest() {
			return report, errors.New("resume requires unchanged configuration and audit")
		}
		seen := map[string]bool{}
		for i := range report.Runs {
			run := &report.Runs[i]
			key := fmt.Sprintf("%s:%s:%d", run.Case, run.Mode, run.Repetition)
			valid := false
			for _, item := range c.Cases {
				valid = valid || (item.ID == run.Case && item.Class == run.Class && leancheck.Digest(item.Goal) == run.GoalSHA256)
			}
			if !valid || seen[key] || run.Repetition < 1 || run.Repetition > 2 || (run.Mode != "single" && run.Mode != "team" && run.Mode != "adaptive" && run.Mode != "lean") || run.RequestUpperBound < 0 || run.RequestUpperBound > 6 || run.MeasuredRequests > run.RequestUpperBound || (run.Mode == "lean" && run.RequestUpperBound != 0) {
				return report, errors.New("resume run identity mismatch")
			}
			seen[key] = true
			if run.Status == "running" {
				run.Status, run.Error = "failed", "interrupted_request_not_replayed"
				run.TimeIncomplete, run.UnknownRequestCount, run.UsageIncomplete = true, true, true
			}
		}
		report.ResumedAt = append(report.ResumedAt, time.Now().UTC())
	} else if err := os.Mkdir(dir, 0700); err != nil {
		return report, errors.New("destination must be new")
	}
	if err := saveReport(dir, report); err != nil {
		return report, err
	}
	for _, item := range c.Cases {
		if r.Resume {
			body, err := os.ReadFile(filepath.Join(dir, "preflight-"+item.ID, "report.json"))
			var probe leancheck.Report
			if err != nil || len(body) > 64000 || json.Unmarshal(body, &probe) != nil || probe.GoalSHA256 != leancheck.Digest(item.Goal) || probe.AuditSHA256 != leancheck.AuditDigest() || (probe.Phase != "compile" && probe.Status != "verified") {
				return report, errors.New("resume requires all goal preflights to be complete")
			}
		} else {
			work := filepath.Join(dir, "preflight-"+item.ID)
			if err := os.Mkdir(work, 0700); err != nil {
				return report, err
			}
			template, _ := leancheck.CandidateTemplate(item.Goal)
			probe, checkErr := r.Checker.Check(ctx, item.Goal, template+"  trivial\n", work)
			body, _ := json.Marshal(probe)
			if err := os.WriteFile(filepath.Join(work, "report.json"), body, 0600); err != nil {
				return report, err
			}
			if probe.Phase != "compile" && (checkErr != nil || probe.Status != "verified" || probe.AuditSHA256 != leancheck.AuditDigest()) {
				return report, errors.New("goal preflight failed before any model request: " + item.ID)
			}
		}
	}
	for repetition := 1; repetition <= 2; repetition++ {
		for i, item := range c.Cases {
			modes := []string{"single", "team", "adaptive"}
			shift := (i + repetition - 1) % len(modes)
			modes = append(append([]string{}, modes[shift:]...), modes[:shift]...)
			modes = append(modes, "lean")
			for _, mode := range modes {
				if ctx.Err() != nil {
					return report, ctx.Err()
				}
				exists := false
				for _, run := range report.Runs {
					exists = exists || (run.Case == item.ID && run.Mode == mode && run.Repetition == repetition)
				}
				if exists {
					continue
				}
				run := Run{Case: item.ID, Class: item.Class, Repetition: repetition, Mode: mode, GoalSHA256: leancheck.Digest(item.Goal), Status: "running", Profiles: map[string]string{}}
				report.Runs = append(report.Runs, run)
				index := len(report.Runs) - 1
				persist := func(value Run) error { report.Runs[index] = value; return saveReport(dir, report) }
				if err := persist(run); err != nil {
					return report, err
				}
				work := filepath.Join(dir, fmt.Sprintf("%02d-%s-%s-%d", i, item.ID, mode, repetition))
				limited, cancel := context.WithTimeout(ctx, 900*time.Second)
				var err error
				if mode == "lean" {
					err = r.baseline(limited, item, work, &run)
				} else {
					err = r.run(limited, c, item, mode, work, &run, persist)
				}
				cancel()
				if err != nil {
					run.Status, run.Error = "failed", err.Error()
				}
				if err := persist(run); err != nil {
					return report, err
				}
			}
		}
	}
	return report, nil
}

func (r Runner) baseline(ctx context.Context, item Case, dir string, run *Run) error {
	start := time.Now()
	defer func() { run.Seconds = time.Since(start).Seconds() }()
	if err := os.Mkdir(dir, 0700); err != nil {
		return err
	}
	for _, outcome := range []string{"proof", "refutation"} {
		g := item.Goal
		if outcome == "refutation" {
			g = negative(g)
		}
		template, _ := leancheck.CandidateTemplate(g)
		source := template + "  first | simp_all [" + g.Declaration + "] | (unfold " + g.Declaration + "; intros; first | omega | ring | norm_num | decide)\n"
		work := filepath.Join(dir, outcome)
		_ = os.Mkdir(work, 0700)
		report, err := r.Checker.Check(ctx, g, source, work)
		body, _ := json.Marshal(map[string]any{"author": "fixed_lean_procedure_v1", "source": source, "report": report})
		if e := os.WriteFile(filepath.Join(work, "baseline.json"), body, 0600); e != nil {
			return e
		}
		run.Verification = &report
		if err == nil && report.Status == "verified" && report.GoalSHA256 == leancheck.Digest(g) && report.SourceSHA256 == leancheck.Digest(source) && report.AuditSHA256 == leancheck.AuditDigest() {
			run.Status, run.Outcome = "verified_baseline_not_accepted", outcome
			return nil
		}
	}
	run.Status = "inconclusive"
	return nil
}
