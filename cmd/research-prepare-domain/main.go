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
	"syscall"
	"time"

	"github.com/snow-ghost/research-team/internal/leancheck"
	"github.com/snow-ghost/research-team/internal/researchweb"
)

type specification struct {
	Domain leancheck.Goal   `json:"domain"`
	Goals  []leancheck.Goal `json:"goals"`
}

func run() error {
	config := flag.String("lean-config", "", "Pinned Lean configuration")
	specFile := flag.String("spec", "", "Domain and goal definitions JSON")
	out := flag.String("out", "", "New private artifact directory")
	flag.Parse()
	if *config == "" || *specFile == "" || *out == "" {
		return errors.New("require -lean-config, -spec and -out; no model calls")
	}
	var cfg leancheck.Config
	var spec specification
	if err := researchweb.ReadJSON(*config, &cfg); err != nil {
		return err
	}
	if err := researchweb.ReadJSON(*specFile, &spec); err != nil {
		return err
	}
	if spec.Domain.Validate() != nil || len(spec.Goals) < 1 || len(spec.Goals) > 8 {
		return errors.New("require one domain and 1..8 goal definitions")
	}
	root, err := filepath.Abs(*out)
	if err != nil {
		return err
	}
	if err = os.Mkdir(root, 0700); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	started := time.Now()
	domainDir := filepath.Join(root, "domain")
	if err = os.Mkdir(domainDir, 0700); err != nil {
		return err
	}
	domain, domainErr := (leancheck.DockerChecker{Config: cfg}).PrepareGoals(ctx, []leancheck.Goal{spec.Domain}, domainDir)
	record := map[string]any{"version": "domain-preparation@1", "domain": domain, "model_calls": 0, "operator_acceptance": false}
	if domainErr == nil && domain.Status == "ready" {
		if err = leancheck.VerifyPreparedDefinitions(domainDir, domain, []leancheck.Goal{spec.Domain}); err != nil {
			return err
		}
		module := domain.Definitions[0].Module
		resolved := append([]leancheck.Goal{}, spec.Goals...)
		for i := range resolved {
			resolved[i].Source = "import " + module + "\n" + resolved[i].Source
		}
		env := cfg
		env.Libraries = append(append([]string{}, cfg.Libraries...), filepath.Join(domainDir, "checked"))
		goalDir := filepath.Join(root, "goals")
		if err = os.Mkdir(goalDir, 0700); err != nil {
			return err
		}
		goals, goalErr := (leancheck.DockerChecker{Config: env}).PrepareGoals(ctx, resolved, goalDir)
		record["goals"], record["resolved_goals"], record["environment"] = goals, resolved, env
		if goalErr != nil {
			err = goalErr
		} else if goals.Status != "ready" {
			err = errors.New("goal definitions not ready")
		} else {
			err = leancheck.VerifyPreparedDefinitions(goalDir, goals, resolved)
		}
	} else {
		err = domainErr
		if err == nil {
			err = errors.New("domain definitions not ready")
		}
	}
	record["seconds"] = time.Since(started).Seconds()
	if err != nil {
		record["error"] = err.Error()
	}
	body, marshalErr := json.MarshalIndent(record, "", "  ")
	if marshalErr != nil {
		return marshalErr
	}
	if writeErr := os.WriteFile(filepath.Join(root, "report.json"), body, 0600); writeErr != nil {
		return writeErr
	}
	if err != nil {
		return err
	}
	fmt.Printf("Prepared one canonical domain and %d goals; no theorem accepted, no model calls\n", len(spec.Goals))
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
