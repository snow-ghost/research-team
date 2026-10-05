//go:build linux

package researchweb

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
)

func TestRemoteBDD_CommonBudgetDeadlineBoundsLeaseAndRejectsLateResult(t *testing.T) {
	o := optionsFor(t, "http://127.0.0.1:1/v1")
	o.Config.Workers = []WorkerConfig{{ID: "worker-test", TokenEnv: "WORKER_KEY", Profiles: []string{"reader"}}}
	s := serviceFor(t, o)
	v := studyFor(t, s)
	study, goal := v.Studies[0].ID, v.Studies[0].Goal
	deadline := time.Now().Add(5 * time.Second)
	budgetFor(t, s, study, 2, 2048, &deadline)
	v = stateOf(t, s)
	if err := s.BindWorker(BindingRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: study, Connector: "worker-test", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	v = act(t, s, Action{Type: "TASK", Target: goal, Kind: "proof", Title: "Remote bounded task", Text: "P"})
	if err := s.Start(RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[0].ID, Profile: "reader", Workspace: "source", RemoteWorker: "worker-test", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	a := waitAttempt(t, s, stateOf(t, s).Attempts[0].ID, func(a Attempt) bool { return a.Status == "awaiting_worker" })
	p, err := s.ClaimWorker("worker-test", WorkerRequest{RequestID: identifier("claim")})
	if err != nil || p == nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	leased := v.attempt(a.ID)
	if leased.LeaseDeadline.After(deadline) || leased.LeaseExpires.After(deadline) {
		t.Fatal("lease exceeds study deadline")
	}
	if err := s.Store.Change(0, "", "", "Move test deadline", goal, "test", func(d *Data) error {
		past := time.Now().Add(-time.Second)
		d.Studies[0].Budget.DeadlineAt = &past
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	r := execution.Result{TaskID: p.Task.ID, AttemptID: a.ID, Snapshot: p.Task.Snapshot, LeaseEpoch: p.Task.LeaseEpoch, ProfileID: p.Profile, ProfileSHA256: p.ProfileSHA256, Status: "candidate", Candidate: "P", RemoteOutcome: "response_received"}
	if err := s.SubmitWorker("worker-test", a.ID, WorkerRequest{RequestID: identifier("result"), Epoch: p.Task.LeaseEpoch, Snapshot: p.Task.Snapshot, Result: &r}); err == nil {
		t.Fatal("late result accepted before scheduler cancellation")
	}
	s.expireStudyBudgets()
	v = stateOf(t, s)
	if v.attempt(a.ID).Status != "cancelled" || v.attempt(a.ID).StopCause != "study_deadline" || v.entity(goal).Status == "accepted" {
		t.Fatal("deadline lost cancellation or promoted goal")
	}
}

func TestRemoteBDD_OptInLimitedPacketLeaseAndCandidateOnly(t *testing.T) {
	o := optionsFor(t, "http://127.0.0.1:1/v1")
	o.Config.Workers = []WorkerConfig{{ID: "worker-test", TokenEnv: "WORKER_KEY", Profiles: []string{"reader"}}}
	lookup := o.Lookup
	o.Lookup = func(key string) (string, bool) {
		if key == "WORKER_KEY" {
			return strings.Repeat("w", 40), true
		}
		return lookup(key)
	}
	if err := os.WriteFile(filepath.Join(o.Config.Workspaces[0].Path, "TASK.md"), []byte("Approved task"), 0600); err != nil {
		t.Fatal(err)
	}
	s := serviceFor(t, o)
	v := studyFor(t, s)
	goal := v.Studies[0].Goal
	v = act(t, s, Action{Type: "TASK", Target: goal, Kind: "proof", Title: "Remote proof", Text: "Prove the selected statement."})
	r := RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[0].ID, Profile: "reader", Workspace: "source", RemoteWorker: "worker-test", Confirm: true}
	if err := s.Start(r); err == nil {
		t.Fatal("worker ran without binding")
	}
	if err := s.BindWorker(BindingRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: v.Studies[0].ID, Connector: "worker-test", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	r.ExpectedRevision = stateOf(t, s).Revision
	r.RequestID = identifier("cmd")
	if err := s.Start(r); err != nil {
		t.Fatal(err)
	}
	a := waitAttempt(t, s, stateOf(t, s).Attempts[0].ID, func(a Attempt) bool { return a.Status == "awaiting_worker" })
	claim := WorkerRequest{RequestID: identifier("claim")}
	p, err := s.ClaimWorker("worker-test", claim)
	if err != nil || p == nil {
		t.Fatal("claim failed", err)
	}
	repeated, err := s.ClaimWorker("worker-test", claim)
	if err != nil || repeated.Task.LeaseEpoch != p.Task.LeaseEpoch {
		t.Fatal("claim was repeated", err)
	}
	body, _ := json.Marshal(p)
	if strings.Contains(string(body), "lemma.txt") || strings.Contains(string(body), o.Config.DataDir) || strings.Contains(string(body), "model-test-secret") || p.Task.Workspace != "" {
		t.Fatal("packet exceeded approved scope")
	}
	result := execution.Result{TaskID: p.Task.ID, AttemptID: p.Task.AttemptID, Snapshot: p.Task.Snapshot, LeaseEpoch: p.Task.LeaseEpoch, ProfileID: p.Profile, ProfileSHA256: p.ProfileSHA256, Status: "candidate", Candidate: "Unverified proof", RemoteOutcome: "response_received"}
	request := WorkerRequest{RequestID: identifier("result"), Epoch: result.LeaseEpoch + 1, Snapshot: result.Snapshot, Result: &result}
	if err := s.SubmitWorker("worker-test", a.ID, request); err == nil {
		t.Fatal("stale epoch accepted")
	}
	request.Epoch = result.LeaseEpoch
	if err := s.SubmitWorker("worker-test", a.ID, request); err != nil {
		t.Fatal(err)
	}
	if err := s.SubmitWorker("worker-test", a.ID, request); err != nil {
		t.Fatal("result dedup", err)
	}
	v = stateOf(t, s)
	if v.Entities[0].Status == "accepted" || v.Attempts[0].Status != "candidate" {
		t.Fatal("worker bypassed acceptance")
	}
	h, err := NewHTTP(s, strings.Repeat("o", 40))
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:4187/api/bootstrap", nil)
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("w", 40))
	response := httptest.NewRecorder()
	h.ServeHTTP(response, req)
	if response.Code != 401 {
		t.Fatal("worker key gained operator privileges")
	}
	v = act(t, s, Action{Type: "TASK", Target: goal, Kind: "proof", Title: "Another remote proof", Text: "Check the current statement."})
	r.ExpectedRevision = v.Revision
	r.RequestID = identifier("cmd")
	r.TaskID = v.Tasks[len(v.Tasks)-1].ID
	if err := s.Start(r); err != nil {
		t.Fatal(err)
	}
	next := waitAttempt(t, s, stateOf(t, s).Attempts[1].ID, func(a Attempt) bool { return a.Status == "awaiting_worker" })
	packet, err := s.ClaimWorker("worker-test", WorkerRequest{RequestID: identifier("claim")})
	if err != nil || packet == nil {
		t.Fatal("second claim", err)
	}
	past := time.Now().Add(-time.Second)
	if err := s.Store.Change(0, "", "", "expire lease", "", "test", func(d *Data) error { d.attempt(next.ID).LeaseExpires = &past; return nil }); err != nil {
		t.Fatal(err)
	}
	s.expireWorkerLeases()
	v = stateOf(t, s)
	if v.attempt(next.ID).Status != "interrupted" {
		t.Fatal("expired lease was replayed")
	}
	if err := s.HeartbeatWorker("worker-test", next.ID, WorkerRequest{Epoch: packet.Task.LeaseEpoch, Snapshot: packet.Task.Snapshot}); err == nil {
		t.Fatal("late heartbeat accepted")
	}
}
