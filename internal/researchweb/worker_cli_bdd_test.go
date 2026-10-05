//go:build linux

package researchweb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerCLIBDD_LoopbackTaskReturnsCandidateAndDisconnectRevokesScope(t *testing.T) {
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		completeModel(w, "Candidate produced by the separate worker program.")
	}))
	defer model.Close()
	server := httptest.NewUnstartedServer(nil)
	defer server.Close()
	o := optionsFor(t, model.URL+"/v1")
	o.Config.Listen = server.Listener.Addr().String()
	o.Config.Workers = []WorkerConfig{{ID: "worker-cli", TokenEnv: "CLI_WORKER_KEY", Profiles: []string{"reader"}}}
	workerKey := strings.Repeat("w", 40)
	lookup := o.Lookup
	o.Lookup = func(key string) (string, bool) {
		if key == "CLI_WORKER_KEY" {
			return workerKey, true
		}
		return lookup(key)
	}
	if err := os.WriteFile(filepath.Join(o.Config.Workspaces[0].Path, "TASK.md"), []byte("Only the selected task is approved."), 0600); err != nil {
		t.Fatal(err)
	}
	s := serviceFor(t, o)
	h, err := NewHTTP(s, strings.Repeat("o", 40))
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	server.Config.Handler = h
	server.Start()
	v := studyFor(t, s)
	study, goal := v.Studies[0].ID, v.Studies[0].Goal
	if err := s.BindWorker(BindingRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: study, Connector: "worker-cli", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	v = act(t, s, Action{Type: "TASK", Target: goal, Kind: "proof", Title: "One isolated worker task", Text: "Return a candidate for the selected goal."})
	if err := s.Start(RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[len(v.Tasks)-1].ID, Profile: "reader", Workspace: "source", RemoteWorker: "worker-cli", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	attempt := waitAttempt(t, s, stateOf(t, s).Attempts[0].ID, func(a Attempt) bool { return a.Status == "awaiting_worker" })
	dir := t.TempDir()
	profile := o.Profiles["reader"]
	body, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "profile.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(map[string]string{"server": server.URL, "worker": "worker-cli", "token_env": "CLI_WORKER_KEY", "profile": "profile.json"})
	configPath := filepath.Join(dir, "worker.json")
	if err := os.WriteFile(configPath, body, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "run", "./cmd/research-worker", "-config", configPath)
	command.Dir = filepath.Join("..", "..")
	command.Env = append(os.Environ(), "CLI_WORKER_KEY="+workerKey)
	if value, ok := o.Lookup(profile.Model.TokenEnv); ok {
		command.Env = append(command.Env, profile.Model.TokenEnv+"="+value)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("worker program failed: %v %s", err, output)
	}
	v = stateOf(t, s)
	a := v.attempt(attempt.ID)
	result, err := s.readResult(*a)
	if err != nil || a.Status != "candidate" || result.ExecutionProfileSHA256 == "" || result.Candidate != "Candidate produced by the separate worker program." || v.entity(goal).Status != "open" || calls.Load() != 1 {
		t.Fatal("worker result, provenance, or candidate boundary failed", err)
	}
	if err := s.BindWorker(BindingRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: study, Connector: "worker-cli", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if err := s.workerScope(stateOf(t, s).Data, "worker-cli", "reader", goal); err == nil {
		t.Fatal("disconnected worker retained study scope")
	}
}
