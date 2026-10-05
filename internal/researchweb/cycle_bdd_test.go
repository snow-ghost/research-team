//go:build linux

package researchweb

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCycleBDD_DependencyOrderSeparateAcceptanceAndCompletion(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			var calls atomic.Int32
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				completeModel(w, "Candidate argument, awaiting independent verification.")
			}))
			defer model.Close()
			var s *Service
			if backend == "postgres" {
				s = pgServiceFor(t, model.URL+"/v1")
			} else {
				s = serviceFor(t, optionsFor(t, model.URL+"/v1"))
			}
			v := studyFor(t, s)
			goal := v.Studies[0].Goal
			v = act(t, s, Action{Type: "SPLIT", Target: goal, Parts: []string{"Case one", "Case two"}})
			expected := append(append([]string{}, v.entity(goal).Dependencies...), goal)
			r := CycleRequest{ExpectedRevision: v.Revision, RequestID: identifier("cycle-request"), Study: v.Studies[0].ID, Profile: "reader", Workspace: "source", MaxAttempts: 4, Confirm: true}
			if err := s.StartCycle(r); err != nil {
				t.Fatal(err)
			}
			if err := s.StartCycle(r); err != nil {
				t.Fatal("duplicate command", err)
			}
			id := stateOf(t, s).Cycles[0].ID
			for step, target := range expected {
				v = waitCycle(t, s, id, "awaiting_review")
				c := v.cycle(id)
				a := v.attempt(c.CurrentAttempt)
				if a.Target != target || c.UsedAttempts != step+1 || v.entity(target).Status != "in_review" {
					t.Fatalf("wrong obligation: %+v", c)
				}
				if v.entity(goal).Status == "accepted" {
					t.Fatal("goal accepted by model")
				}
				if snapshot, err := s.Store.Snapshot(a.InputSnapshot); err != nil || snapshot.task(a.TaskID) == nil {
					t.Fatal("task missing in attempt snapshot", err)
				}
				act(t, s, Action{Type: "REVIEW", Target: target, Decision: "accept", Text: "Проверены предпосылки и все переходы."})
				if step < len(expected)-1 {
					deadline := time.Now().Add(10 * time.Second)
					for stateOf(t, s).Cycles[0].CurrentAttempt == a.ID && time.Now().Before(deadline) {
						time.Sleep(20 * time.Millisecond)
					}
				}
			}
			v = waitCycle(t, s, id, "completed")
			if calls.Load() != 4 || v.entity(goal).Status != "accepted" {
				t.Fatal("unexpected calls or missing acceptance")
			}
		})
	}
}

func TestCycleBDD_RejectionUsesBudgetAndDoesNotLoopForever(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { completeModel(w, "Candidate with a missing case.") }))
	defer model.Close()
	s := serviceFor(t, optionsFor(t, model.URL+"/v1"))
	v := studyFor(t, s)
	r := CycleRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: v.Studies[0].ID, Profile: "reader", Workspace: "source", MaxAttempts: 1}
	if s.StartCycle(r) == nil {
		t.Fatal("unapproved cycle started")
	}
	r.Confirm = true
	if err := s.StartCycle(r); err != nil {
		t.Fatal(err)
	}
	id := stateOf(t, s).Cycles[0].ID
	v = waitCycle(t, s, id, "awaiting_review")
	act(t, s, Action{Type: "REVIEW", Target: v.Studies[0].Goal, Decision: "reject", Text: "Не разобран граничный случай."})
	v = waitCycle(t, s, id, "exhausted")
	if len(v.Attempts) != 1 || v.Entities[0].Status != "needs_changes" {
		t.Fatal("budget bypassed")
	}
}

func TestCycleBDD_PauseStopAndRestartNeverReplay(t *testing.T) {
	var calls atomic.Int32
	entered := make(chan struct{}, 1)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		calls.Add(1)
		entered <- struct{}{}
		<-r.Context().Done()
	}))
	defer model.Close()
	o := optionsFor(t, model.URL+"/v1")
	s := serviceFor(t, o)
	v := studyFor(t, s)
	if err := s.StartCycle(CycleRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: v.Studies[0].ID, Profile: "reader", Workspace: "source", MaxAttempts: 3, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("model was not started")
	}
	v = stateOf(t, s)
	id := v.Cycles[0].ID
	if err := s.ControlCycle(id, CycleCommand{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Kind: "pause"}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	if v.Cycles[0].Status != "paused" || !active(v.Attempts[0].Status) {
		t.Fatal("pause cancelled active attempt")
	}
	if err := s.ControlCycle(id, CycleCommand{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Kind: "resume"}); err == nil {
		t.Fatal("resume without consent")
	}
	if err := s.ControlCycle(id, CycleCommand{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Kind: "stop"}); err != nil {
		t.Fatal(err)
	}
	waitAttempt(t, s, v.Attempts[0].ID, func(a Attempt) bool { return a.Status == "cancelled" })
	s.Close()
	reopened := serviceFor(t, o)
	time.Sleep(250 * time.Millisecond)
	if calls.Load() != 1 || stateOf(t, reopened).Cycles[0].Status != "stopped" {
		t.Fatal("stopped work replayed")
	}
}

func TestCycleBDD_RestartRequiresFreshConsent(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { completeModel(w, "Candidate.") }))
	defer model.Close()
	o := optionsFor(t, model.URL+"/v1")
	s := serviceFor(t, o)
	v := studyFor(t, s)
	if err := s.StartCycle(CycleRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: v.Studies[0].ID, Profile: "reader", Workspace: "source", MaxAttempts: 2, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	id := stateOf(t, s).Cycles[0].ID
	waitCycle(t, s, id, "awaiting_review")
	s.Close()
	reopened := serviceFor(t, o)
	v = stateOf(t, reopened)
	if v.Cycles[0].Status != "interrupted" || len(v.Attempts) != 1 {
		t.Fatal("cycle resumed after restart")
	}
	if err := reopened.ControlCycle(id, CycleCommand{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Kind: "resume", Confirm: true}); err == nil {
		t.Fatal("interrupted cycle resumed implicitly")
	}
}

func TestCycleBDD_ReviewerFeedbackIsReused(t *testing.T) {
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if calls.Add(1) == 2 && !strings.Contains(string(raw), "missing case") {
			t.Error("review feedback absent")
		}
		completeModel(w, "Candidate.")
	}))
	defer model.Close()
	s := serviceFor(t, optionsFor(t, model.URL+"/v1"))
	v := studyFor(t, s)
	if err := s.StartCycle(CycleRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: v.Studies[0].ID, Profile: "reader", Workspace: "source", MaxAttempts: 2, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	id := stateOf(t, s).Cycles[0].ID
	v = waitCycle(t, s, id, "awaiting_review")
	old := v.Cycles[0].CurrentAttempt
	act(t, s, Action{Type: "REVIEW", Target: v.Studies[0].Goal, Decision: "reject", Text: "missing case"})
	deadline := time.Now().Add(10 * time.Second)
	for stateOf(t, s).Cycles[0].CurrentAttempt == old && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	v = waitCycle(t, s, id, "awaiting_review")
	if v.Cycles[0].UsedAttempts != 2 {
		t.Fatal("correction was not attempted")
	}
	encoded, _ := json.Marshal(v.Cycles[0])
	if strings.Contains(string(encoded), "model-test-secret") {
		t.Fatal("cycle exposes credential")
	}
}

func TestCycleBDD_ChangedTargetBlocksStaleCandidate(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		select {
		case <-release:
			completeModel(w, "Obsolete candidate.")
		case <-r.Context().Done():
		}
	}))
	defer model.Close()
	s := serviceFor(t, optionsFor(t, model.URL+"/v1"))
	v := studyFor(t, s)
	if err := s.StartCycle(CycleRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: v.Studies[0].ID, Profile: "reader", Workspace: "source", MaxAttempts: 2, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("model was not started")
	}
	act(t, s, Action{Type: "SPLIT", Target: v.Studies[0].Goal, Parts: []string{"New case A", "New case B"}})
	close(release)
	id := stateOf(t, s).Cycles[0].ID
	v = waitCycle(t, s, id, "blocked")
	if v.entity(v.Studies[0].Goal).Proof != "" || len(v.Attempts) != 1 {
		t.Fatal("stale proof attached or retried")
	}
}

func TestCycleBDD_PauseHoldsQueuedAttemptWithoutExtraCharge(t *testing.T) {
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if calls.Add(1) == 1 {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		completeModel(w, "Candidate.")
	}))
	defer model.Close()
	s := serviceFor(t, optionsFor(t, model.URL+"/v1"))
	v := studyFor(t, s)
	v = act(t, s, Action{Type: "TASK", Target: v.Studies[0].Goal, Title: "Occupy worker", Kind: "proof", Text: "Check claim"})
	if err := s.Start(RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[0].ID, Profile: "reader", Workspace: "source", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("worker was not occupied")
	}
	v = studyFor(t, s)
	if err := s.StartCycle(CycleRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: v.Studies[1].ID, Profile: "reader", Workspace: "source", MaxAttempts: 1, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for len(stateOf(t, s).Attempts) < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	v = stateOf(t, s)
	if len(v.Attempts) != 2 {
		t.Fatal("cycle attempt was not queued")
	}
	id := v.Cycles[0].ID
	if err := s.ControlCycle(id, CycleCommand{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Kind: "pause"}); err != nil {
		t.Fatal(err)
	}
	close(release)
	waitAttempt(t, s, v.Attempts[0].ID, func(a Attempt) bool { return !active(a.Status) })
	time.Sleep(250 * time.Millisecond)
	v = stateOf(t, s)
	if calls.Load() != 1 || v.Attempts[1].Status != "queued" {
		t.Fatal("paused cycle started queued work")
	}
	if err := s.ControlCycle(id, CycleCommand{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Kind: "resume", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	v = waitCycle(t, s, id, "awaiting_review")
	if calls.Load() != 2 || v.Cycles[0].UsedAttempts != 1 || len(v.Attempts) != 2 {
		t.Fatal("resume created an extra attempt")
	}
}
