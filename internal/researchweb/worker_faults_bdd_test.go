//go:build linux

package researchweb

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestWorkerFaultsBDD_SeparateProgramsLostLeaseExplicitReassignmentAndCancellation(t *testing.T) {
	var calls atomic.Int32
	first, third := make(chan struct{}), make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if bytes.Contains(body, []byte("PRIVATE-UNRELATED-MATERIAL")) {
			t.Error("worker received unrelated material")
		}
		call := calls.Add(1)
		if call == 1 {
			close(first)
			<-r.Context().Done()
			return
		}
		if call == 3 {
			close(third)
			<-r.Context().Done()
			return
		}
		completeModel(w, "```lean\n"+submittedProof+"```")
	}))
	defer provider.Close()
	server := httptest.NewUnstartedServer(nil)
	defer server.Close()
	o := optionsFor(t, provider.URL+"/v1")
	o.Config.Listen = server.Listener.Addr().String()
	o.Config.Workers = []WorkerConfig{{ID: "worker-fault-a", TokenEnv: "FAULT_KEY_A", Profiles: []string{"reader"}}, {ID: "worker-fault-b", TokenEnv: "FAULT_KEY_B", Profiles: []string{"reader"}}}
	p := o.Profiles["reader"]
	p.Limits.TimeoutSeconds = 30
	o.Profiles["reader"] = p
	lookup := o.Lookup
	o.Lookup = func(key string) (string, bool) {
		if key == "FAULT_KEY_A" {
			return strings.Repeat("a", 40), true
		}
		if key == "FAULT_KEY_B" {
			return strings.Repeat("b", 40), true
		}
		return lookup(key)
	}
	if err := os.WriteFile(filepath.Join(o.Config.Workspaces[0].Path, "TASK.md"), []byte("Known statement only"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(o.Config.Workspaces[0].Path, "unrelated.md"), []byte("PRIVATE-UNRELATED-MATERIAL"), 0600); err != nil {
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
	study, target := v.Studies[0].ID, v.Studies[0].Goal
	if err := s.SetFormalGoal(target, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	if err := s.SetStudyBudget(study, BudgetRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Confirm: true, Note: "Local fake-provider fault drill", Budget: StudyBudget{MaxAttempts: 3, MaxModelRequests: 6, MaxOutputTokens: 3072}}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	if err := s.BindWorker(BindingRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: study, Connector: "worker-fault-a", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	v = act(t, s, Action{Type: "TASK", Target: target, Title: "Fault drill", Kind: "proof", Text: "Prove the selected goal"})
	if err := s.Start(RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[len(v.Tasks)-1].ID, Profile: "reader", Workspace: "source", RemoteWorker: "worker-fault-a", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	old := waitAttempt(t, s, stateOf(t, s).Attempts[0].ID, func(a Attempt) bool { return a.Status == "awaiting_worker" })
	dir := t.TempDir()
	bin := filepath.Join(dir, "research-worker")
	build := exec.Command("go", "build", "-o", bin, "./cmd/research-worker")
	build.Dir = filepath.Join("..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("worker build: %v %s", err, out)
	}
	profile, _ := json.Marshal(p)
	if err := os.WriteFile(filepath.Join(dir, "profile.json"), profile, 0600); err != nil {
		t.Fatal(err)
	}
	start := func(worker, keyEnv string) (*exec.Cmd, *bytes.Buffer) {
		t.Helper()
		conf, _ := json.Marshal(map[string]string{"server": server.URL, "worker": worker, "token_env": keyEnv, "profile": "profile.json"})
		path := filepath.Join(dir, worker+".json")
		if err := os.WriteFile(path, conf, 0600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		t.Cleanup(cancel)
		command := exec.CommandContext(ctx, bin, "-config", path)
		key, _ := o.Lookup(keyEnv)
		token, _ := o.Lookup(p.Model.TokenEnv)
		command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, keyEnv + "=" + key, p.Model.TokenEnv + "=" + token}
		output := &bytes.Buffer{}
		command.Stdout = output
		command.Stderr = output
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if command.ProcessState == nil {
				_ = command.Process.Kill()
				_ = command.Wait()
			}
		})
		return command, output
	}
	commandA, outputA := start("worker-fault-a", "FAULT_KEY_A")
	select {
	case <-first:
	case <-time.After(20 * time.Second):
		_ = commandA.Process.Kill()
		_ = commandA.Wait()
		t.Fatal("worker A never called local provider", outputA.String())
	}
	if err := commandA.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := commandA.Wait(); err == nil {
		t.Fatal("killed worker succeeded")
	}
	// The process failure is real; the lease clock is accelerated to keep the drill bounded.
	if err := s.Store.Change(0, "", "", "Accelerate fault-drill lease", old.ID, "test", func(d *Data) error {
		past := time.Now().Add(-time.Second)
		d.attempt(old.ID).LeaseExpires = &past
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s.expireWorkerLeases()
	v = stateOf(t, s)
	if v.attempt(old.ID).Status != "interrupted" || v.attempt(old.ID).RemoteOutcome != "unknown" || len(v.Attempts) != 1 {
		t.Fatal("disconnection automatically replayed work")
	}
	r := ReassignWorkerRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Worker: "worker-fault-b", Confirm: true}
	if err := s.ReassignWorker(old.ID, r); err == nil {
		t.Fatal("unbound worker received assignment")
	}
	if err := s.BindWorker(BindingRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: study, Connector: "worker-fault-b", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	r.ExpectedRevision = stateOf(t, s).Revision
	r.RequestID = identifier("cmd")
	if err := s.ReassignWorker(old.ID, r); err != nil {
		t.Fatal(err)
	}
	if err := s.ReassignWorker(old.ID, r); err != nil {
		t.Fatal("reassignment idempotency", err)
	}
	next := waitAttempt(t, s, stateOf(t, s).Attempts[1].ID, func(a Attempt) bool { return a.Status == "awaiting_worker" })
	commandB, outputB := start("worker-fault-b", "FAULT_KEY_B")
	if err := commandB.Wait(); err != nil {
		t.Fatalf("worker B: %v %s", err, outputB.String())
	}
	v = stateOf(t, s)
	if v.attempt(next.ID).Status != "candidate" || v.attempt(next.ID).ParentAttempt != old.ID || v.entity(target).Status != "open" || calls.Load() != 2 {
		t.Fatal("reassignment lost provenance or acceptance boundary")
	}
	oBudget := s.observeBudget(&v.Data, study)
	if oBudget.ChargedModelRequests != 3 || oBudget.UnknownUsage != 1 {
		t.Fatal("unknown old work was not charged", oBudget)
	}
	late := execution.Result{TaskID: old.TaskID, AttemptID: old.ID, Snapshot: v.attempt(old.ID).InputSHA256, LeaseEpoch: v.attempt(old.ID).LeaseEpoch, ProfileID: old.Profile, Status: "candidate", Candidate: "late old result", RemoteOutcome: "response_received"}
	if err := s.SubmitWorker("worker-fault-a", old.ID, WorkerRequest{RequestID: identifier("result"), Epoch: late.LeaseEpoch, Snapshot: late.Snapshot, Result: &late}); err == nil {
		t.Fatal("late old result replaced reassigned work")
	}
	req, _ := http.NewRequest("GET", server.URL+"/api/bootstrap", nil)
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("b", 40))
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("worker gained operator scope")
	}
	v = act(t, s, Action{Type: "TASK", Target: target, Title: "Cancel running worker", Kind: "proof", Text: "Same selected statement"})
	if err := s.Start(RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[len(v.Tasks)-1].ID, Profile: "reader", Workspace: "source", RemoteWorker: "worker-fault-b", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	cancelled := waitAttempt(t, s, stateOf(t, s).Attempts[2].ID, func(a Attempt) bool { return a.Status == "awaiting_worker" })
	commandC, _ := start("worker-fault-b", "FAULT_KEY_B")
	select {
	case <-third:
	case <-time.After(20 * time.Second):
		t.Fatal("third worker never started")
	}
	if err := s.Cancel(cancelled.ID); err != nil {
		t.Fatal(err)
	}
	_ = commandC.Process.Signal(os.Interrupt)
	if err := commandC.Wait(); err == nil {
		t.Fatal("cancelled worker succeeded")
	}
	v = stateOf(t, s)
	if v.attempt(cancelled.ID).Status != "cancelled" || v.attempt(cancelled.ID).ResultSHA256 != "" {
		t.Fatal("late cancelled output accepted")
	}
	if err := s.BindWorker(BindingRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: study, Connector: "worker-fault-b", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if err := s.workerScope(stateOf(t, s).Data, "worker-fault-b", "reader", target); err == nil {
		t.Fatal("disconnected worker retained study access")
	}
}
