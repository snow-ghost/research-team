package execution

import (
	"errors"
	"testing"
)

func TestCoddyBDD_RequiresWorkflowConnectorBeforeResolvingSecrets(t *testing.T) {
	profile := Profile{
		ID:     "coddy-workflow",
		Kind:   "external",
		Limits: Limits{TimeoutSeconds: 30, MaxOutputBytes: 4096},
		External: &ExternalConfig{
			Provider: "coddy",
		},
	}
	lookups := 0
	executor, err := Build(profile, func(string) (string, bool) {
		lookups++
		return "must-not-be-read", true
	})
	if executor != nil || !errors.Is(err, ErrWorkflowRequired) || !errors.Is(err, ErrUnsupported) {
		t.Fatalf("coddy needs a distinct workflow connector: executor=%T err=%v", executor, err)
	}
	if lookups != 0 {
		t.Fatal("credentials resolved for an unsupported workflow")
	}
}

func TestCoddyBDD_WorkflowDoneIsNotAnExecutorCandidate(t *testing.T) {
	// A repository workflow status is not a completed, correlated research attempt.
	_, _, err := parseExternal("coddy", []byte(`{"type":"result","status":"done","result":"PR description"}`))
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Coddy status must not enter the CLI event parser: %v", err)
	}
}
