package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
	"github.com/snow-ghost/research-team/internal/proofreview"
	"github.com/snow-ghost/research-team/internal/researchweb"
)

func save(file string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	config := flag.String("server-config", "", "Trusted server configuration with credentials")
	profile := flag.String("profile", "", "Existing native model profile")
	out := flag.String("out", "", "New private result directory")
	suite := flag.String("suite", "core", "core (24 calls) or expanded (48 calls)")
	continueFrom := flag.String("continue-from", "", "Explicitly continue only unreserved cases of a stopped run; never repeat reserved or unknown requests")
	confirm := flag.Bool("confirm", false, "Explicit authorization for the selected suite's bounded model calls")
	flag.Parse()
	cases := proofreview.CalibrationCases()
	if *suite == "expanded" {
		cases = proofreview.ExpandedCalibrationCases()
	} else if *suite != "core" {
		return errors.New("unknown suite")
	}
	maxRequests := len(cases) * 4
	if !*confirm || *config == "" || *profile == "" || *out == "" {
		return errors.New("require -server-config, -profile, -out and -confirm; no automatic resume")
	}
	o, err := researchweb.LoadOptions(*config)
	if err != nil {
		return err
	}
	p, ok := o.Profiles[*profile]
	if !ok || p.Kind != "model" || p.Model == nil || o.Lean == nil {
		return errors.New("requires a trusted native model profile and pinned Lean")
	}
	p.Limits = execution.Limits{TimeoutSeconds: 900, MaxSteps: 1, MaxToolCalls: 0, MaxOutputTokens: 32768, MaxOutputBytes: 16777216}
	p.Model.Tools = nil
	p.Skills = []execution.Skill{{ID: "static-review-calibration", Version: "1", Instructions: "Проверь соответствие естественной постановки statement и assumptions формальной цели, затем проверенное доказательство или отрицание. Успешный отчет Lean не подтверждает соответствие естественного и формального текста. Верни только JSON proof-review@2, без вступления. Приемка не выполняется."}}
	if *suite == "expanded" {
		p.Skills[0].Instructions += " Если есть exposition, независимо проверь каждый переход этого текста. Правильный Lean-файл не подтверждает правильность изложения. Различай дефект изложения, ложность теоремы и ошибку Lean. Не требуй от корректного краткого изложения раскрывать стандартное правило, которое в нем явно названо."
	}
	if err := p.Validate(); err != nil {
		return err
	}
	var previous *continuation
	if *continueFrom != "" {
		previous, err = loadContinuation(*continueFrom, *suite, leancheck.Digest(p), cases)
		if err != nil {
			return err
		}
	}
	root, err := filepath.Abs(*out)
	if err != nil {
		return err
	}
	if err := os.Mkdir(root, 0700); err != nil {
		return err
	}
	if err := save(filepath.Join(root, "authorization.json"), map[string]any{"suite": *suite, "max_requests": maxRequests, "max_output_tokens": 32768, "timeout_seconds": 900, "profile_sha256": leancheck.Digest(p), "endpoint": p.Model.BaseURL, "model": p.Model.Model, "automatic_retry": false}); err != nil {
		return err
	}
	if previous != nil {
		if err := claimContinuation(previous, root); err != nil {
			return errors.New("stopped allocation already claimed; no model calls started")
		}
		if err := save(filepath.Join(root, "continuation.json"), map[string]any{"previous_directory": previous.Directory, "rows": previous.Rows, "tasks": previous.Tasks, "inherited_reserved": len(previous.Rows), "remaining_requests": maxRequests - len(previous.Rows), "max_cumulative_requests": maxRequests, "automatic_retry": false}); err != nil {
			return err
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	checker := leancheck.DockerChecker{Config: *o.Lean}
	reports := map[string]leancheck.Report{}
	if err := save(filepath.Join(root, "reference-private.json"), cases); err != nil {
		return err
	}
	for _, c := range cases {
		dir := filepath.Join(root, "native-"+c.ID)
		if err := os.Mkdir(dir, 0700); err != nil {
			return err
		}
		r, err := checker.Check(ctx, proofreview.CalibrationClaim(c), c.Source, dir)
		if e := save(filepath.Join(dir, "report.json"), r); e != nil {
			return e
		}
		if err != nil || r.Status != "verified" {
			return errors.New("native fixture check failed; no model calls started")
		}
		reports[c.ID] = r
	}
	rows := []map[string]any{}
	reserved := 0
	if previous != nil {
		rows = append(rows, previous.Rows...)
		reserved = len(previous.Rows)
	}
	for _, c := range cases {
		for rep := 1; rep <= 2; rep++ {
			for order := 0; order < 2; order++ {
				arm := order
				if rep == 2 {
					arm = 1 - order
				}
				name := "legacy"
				objective := proofreview.Instructions
				claim := proofreview.CalibrationClaim(c)
				r := reports[c.ID]
				positive, _ := leancheck.CandidateTemplate(c.Goal)
				negative := c.Goal
				negative.Source = c.Goal.Source + "\nnamespace ResearchRefutation\ndef Statement : Prop := Not (" + c.Goal.Declaration + ")\nend ResearchRefutation\n"
				negative.Declaration = "ResearchRefutation.Statement"
				negative.Candidate = "ResearchRefutation.candidate"
				negativeTemplate, _ := leancheck.CandidateTemplate(negative)
				var material any = map[string]any{"statement": c.Statement, "assumptions": c.Assumptions, "goal": c.Goal, "candidate_templates": map[string]string{"proof": positive, "refutation": negativeTemplate}, "previous_unverified": map[string]any{"original_goal": c.Goal, "goal": claim, "outcome": map[string]string{"outcome": c.Kind, "source": c.Source}, "report": r}}
				if c.Exposition != "" {
					material.(map[string]any)["exposition"] = c.Exposition
				}
				if arm == 1 {
					input, err := proofreview.NewInput(c.Goal, claim, c.Source, r, c.Kind, c.Statement, "fixture")
					if err != nil {
						return err
					}
					input.Assumptions = c.Assumptions
					input.Exposition = c.Exposition
					material = input
					name = "typed"
					objective = proofreview.InputInstructions
				}
				b, err := json.Marshal(material)
				if err != nil {
					return err
				}
				for _, issue := range c.Expected {
					if strings.Contains(string(b), issue.ID) || strings.Contains(string(b), issue.Description) {
						return errors.New("reference annotations leaked into model input")
					}
				}
				id := fmt.Sprintf("%s-%d-%s", c.ID, rep, name)
				if previous != nil {
					if frozen, ok := previous.Tasks[id]; ok {
						if frozen.Context != string(b) || frozen.Objective != objective {
							return errors.New("frozen input changed; continuation held without new calls")
						}
						continue
					}
				}
				dir := filepath.Join(root, id)
				if err := os.Mkdir(dir, 0700); err != nil {
					return err
				}
				workspace := filepath.Join(dir, "workspace")
				if err := os.Mkdir(workspace, 0700); err != nil {
					return err
				}
				anonymousID := fmt.Sprintf("calibration-%03d", reserved+1)
				task := execution.Task{ID: anonymousID, AttemptID: anonymousID, Snapshot: leancheck.Digest(string(b)), LeaseEpoch: 1, Workspace: workspace, Objective: objective, Context: string(b)}
				packet, err := execution.TaskPacket(p, task)
				if err != nil {
					return err
				}
				if strings.Contains(packet, "/opt/") || strings.Contains(packet, "/home/") {
					return errors.New("local path in calibration packet")
				}
				if err := save(filepath.Join(dir, "request-packet.json"), json.RawMessage(packet)); err != nil {
					return err
				}
				reserved++
				if reserved > maxRequests {
					return errors.New("shared calibration limit exhausted")
				}
				if err := save(filepath.Join(dir, "reserved.json"), map[string]any{"request": reserved, "reserved_at": time.Now().UTC(), "outcome": "unknown_until_result"}); err != nil {
					return err
				}
				executor, err := execution.Build(p, o.Lookup)
				if err != nil {
					return err
				}
				started := time.Now()
				result, runErr := executor.Run(ctx, task)
				if err := save(filepath.Join(dir, "result.json"), result); err != nil {
					return err
				}
				row := map[string]any{"id": id, "case": c.ID, "arm": name, "repetition": rep, "status": result.Status, "remote_outcome": result.RemoteOutcome, "usage": result.Usage, "seconds": time.Since(started).Seconds(), "packet_sha256": result.PacketSHA256}
				var review proofreview.Report
				if execution.DecodeAgentReport(result.Candidate, &review) == nil && proofreview.Validate(review) == nil {
					row["review"] = review
					row["review_sha256"] = leancheck.Digest(review)
				} else {
					row["invalid_review"] = true
				}
				if runErr != nil {
					row["error"] = runErr.Error()
				}
				rows = append(rows, row)
				if err := save(filepath.Join(dir, "summary.json"), row); err != nil {
					return err
				}
				fmt.Printf("%s: %s\n", id, result.Status)
				if result.RemoteOutcome == "unknown" {
					_ = save(filepath.Join(root, "stopped-unknown.json"), rows)
					return errors.New("unknown remote outcome retained; no automatic retry")
				}
				if result.Status != "candidate" {
					_ = save(filepath.Join(root, "stopped-execution.json"), rows)
					return errors.New("executor did not return a candidate; remaining calls not sent")
				}
			}
		}
	}
	return save(filepath.Join(root, "report.json"), map[string]any{"version": "review-calibration@1", "suite": *suite, "runs": rows, "max_requests": maxRequests, "operator_acceptance": false, "manual_annotations_required": true, "completed_at": time.Now().UTC()})
}
