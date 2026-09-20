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
	"path/filepath"
	"syscall"

	"github.com/snow-ghost/research-team/internal/execution"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "", "Trusted executor profile JSON")
	taskPath := flag.String("task", "", "Task JSON")
	execute := flag.Bool("execute", false, "Execute the task; may incur provider charges")
	flag.Parse()
	if *configPath == "" {
		return errors.New("-config is required")
	}
	var profile execution.Profile
	if err := readJSON(*configPath, &profile); err != nil {
		return err
	}
	if err := profile.Validate(); err != nil {
		return err
	}
	if !*execute {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"profile_id": profile.ID, "kind": profile.Kind, "configuration_valid": true, "executed": false})
	}
	if *taskPath == "" {
		return errors.New("-task is required for execution")
	}
	task, err := readTask(*taskPath)
	if err != nil {
		return err
	}
	executor, err := execution.Build(profile, os.LookupEnv)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	result, runErr := executor.Run(ctx, task)
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		return err
	}
	return runErr
}

func readTask(path string) (execution.Task, error) {
	var task execution.Task
	if err := readJSON(path, &task); err != nil {
		return task, err
	}
	if task.Workspace == "" {
		return task, errors.New("task workspace is required")
	}
	if !filepath.IsAbs(task.Workspace) {
		workspace, err := filepath.Abs(filepath.Join(filepath.Dir(path), task.Workspace))
		if err != nil {
			return task, errors.New("cannot resolve task workspace")
		}
		task.Workspace = workspace
	}
	return task, nil
}

func readJSON(path string, value any) error {
	file, err := os.Open(path)
	if err != nil {
		return errors.New("configuration or task file unavailable")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil || len(data) > 1<<20 {
		return errors.New("input file too large or unreadable")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	// Decoder diagnostics can quote a mistakenly pasted secret; withhold them.
	if decoder.Decode(value) != nil || decoder.Decode(new(any)) != io.EOF {
		return errors.New("invalid JSON input or unknown fields")
	}
	return nil
}
