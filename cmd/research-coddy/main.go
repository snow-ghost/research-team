package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/snow-ghost/research-team/internal/coddy"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, out, diagnostics io.Writer) error {
	flags := flag.NewFlagSet("research-coddy", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	configPath := flags.String("config", "", "Trusted connector configuration")
	stateDir := flags.String("state-dir", "", "Private persistent state directory")
	action := flags.String("action", "validate", "validate, prepare, local, submit, observe, reconcile, reply, approve-plan, request-changes, collect, hold, resume")
	jobPath := flags.String("job", "", "Pinned job JSON for prepare")
	reviewPath := flags.String("review", "", "Review JSON with summary and line comments")
	messagePath := flags.String("message", "", "JSON with a body field for reply")
	id := flags.String("id", "", "Delegation id")
	requestID := flags.String("request-id", "", "Stable id for a published command")
	expected := flags.String("expected-view", "", "Digest returned by observe")
	target := flags.String("target", "issue", "Plan location: issue or pr")
	commentID := flags.Int64("comment-id", 0, "Bot plan comment id")
	publish := flags.Bool("publish", false, "Authorize this GitHub write; plan approval allows Coddy to push and open a PR")
	timeout := flags.Duration("timeout", 2*time.Minute, "Total command deadline")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if *configPath == "" || *timeout <= 0 || *timeout > time.Hour {
		return errors.New("config and timeout (0..1h] required")
	}
	var config coddy.Config
	if err := readJSON(*configPath, &config); err != nil {
		return err
	}
	if err := config.Validate(); err != nil {
		return err
	}
	if *action == "validate" {
		return json.NewEncoder(out).Encode(map[string]any{"configuration_valid": true, "executed": false, "workflow": "coddy_github"})
	}
	if *stateDir == "" {
		return errors.New("-state-dir required")
	}
	store, err := coddy.OpenStore(*stateDir)
	if err != nil {
		return err
	}
	connector, err := coddy.New(config, store, os.LookupEnv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	var record *coddy.Record
	switch *action {
	case "prepare":
		var job coddy.Job
		if err = readJSON(*jobPath, &job); err != nil {
			return err
		}
		record, err = connector.Prepare(job)
	case "local":
		record, err = connector.Local(*id)
	case "submit":
		record, err = connector.Submit(ctx, *id, *publish)
	case "observe":
		record, err = connector.Observe(ctx, *id)
	case "reconcile":
		record, err = connector.Reconcile(ctx, *id)
	case "approve-plan":
		record, err = connector.ApprovePlan(ctx, *id, *requestID, *target, *expected, *commentID, *publish)
	case "reply":
		var message struct {
			Body string `json:"body"`
		}
		if err = readJSON(*messagePath, &message); err != nil {
			return err
		}
		record, err = connector.Reply(ctx, *id, *requestID, *target, *expected, message.Body, *publish)
	case "request-changes":
		var review coddy.ReviewRequest
		if err = readJSON(*reviewPath, &review); err != nil {
			return err
		}
		record, err = connector.RequestChanges(ctx, *id, *requestID, *expected, review, *publish)
	case "collect":
		record, err = connector.Collect(ctx, *id, *expected)
	case "hold":
		record, err = connector.Hold(*id, true)
	case "resume":
		record, err = connector.Hold(*id, false)
	default:
		return errors.New("unknown connector action")
	}
	if record != nil {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		if writeErr := encoder.Encode(record); writeErr != nil {
			return writeErr
		}
	}
	return err
}

func readJSON(path string, out any) error {
	file, err := os.Open(path)
	if err != nil {
		return errors.New("input file unavailable")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil || len(data) > 1<<20 {
		return errors.New("input file too large or unreadable")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil || decoder.Decode(new(any)) != io.EOF {
		return errors.New("invalid JSON input or unknown fields")
	}
	return nil
}
