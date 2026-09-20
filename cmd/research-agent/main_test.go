package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snow-ghost/research-team/internal/execution"
)

func TestConfigurationDoesNotAcceptOrEchoInlineSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"token":"private-inline-token"}`), 0600); err != nil {
		t.Fatal(err)
	}
	var profile execution.Profile
	err := readJSON(path, &profile)
	if err == nil || strings.Contains(err.Error(), "private-inline-token") {
		t.Fatalf("unsafe diagnostic: %v", err)
	}
}

func TestExampleProfilesValidateWithoutCredentials(t *testing.T) {
	for _, name := range []string{"model", "codex", "claude", "opencode", "coddy-agent"} {
		var p execution.Profile
		if err := readJSON("../../examples/agents/"+name+".json", &p); err != nil {
			t.Fatal(err)
		}
		if err := p.Validate(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestTaskWorkspaceIsRelativeToItsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task.json")
	if err := os.WriteFile(path, []byte(`{"workspace":"source"}`), 0600); err != nil {
		t.Fatal(err)
	}
	task, err := readTask(path)
	if err != nil || task.Workspace != filepath.Join(dir, "source") {
		t.Fatalf("workspace=%q err=%v", task.Workspace, err)
	}
	if err := os.WriteFile(path, []byte(`{"workspace":""}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readTask(path); err == nil {
		t.Fatal("empty workspace must not select the task directory")
	}
}

func TestPortableTaskExampleResolvesToTheIncludedWorkspace(t *testing.T) {
	task, err := readTask("../../examples/agents/task.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(task.Workspace, "lemma.txt")); err != nil {
		t.Fatal(err)
	}
}
