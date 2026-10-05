//go:build linux

package researchweb

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
)

func TestJournalBDD_LimitPartialRedactionAndExplicitContinuation(t *testing.T) {
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request any
		_ = json.NewDecoder(r.Body).Decode(&request)
		input, _ := json.Marshal(request)
		if strings.Contains(string(input), "LOCAL_DIAGNOSTIC_ONLY") {
			t.Error("operator diagnostics sent to a model")
		}
		if calls.Add(1) == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": "partial model-test-secret"}, "finish_reason": "length"}}})
		} else {
			completeModel(w, "Complete candidate")
		}
	}))
	defer model.Close()
	s := serviceFor(t, optionsFor(t, model.URL+"/v1"))
	v := studyFor(t, s)
	goal := v.Studies[0].Goal
	v = act(t, s, Action{Type: "TASK", Target: goal, Kind: "proof", Title: "Proof", Text: "Prove statement"})
	if err := s.Start(RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[0].ID, Profile: "reader", Workspace: "source", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	a := waitAttempt(t, s, stateOf(t, s).Attempts[0].ID, func(a Attempt) bool { return a.Status == "limit_reached" })
	result, err := s.readResult(a)
	if err != nil || result.Partial == "" || strings.Contains(result.Partial, "model-test-secret") {
		t.Fatal("partial not retained or secret leaked", err)
	}
	rows, err := s.Journal(a.ID, 0)
	if err != nil || len(rows) < 3 {
		t.Fatal("journal unavailable", err)
	}
	body, _ := json.Marshal(rows)
	if strings.Contains(string(body), "model-test-secret") {
		t.Fatal("journal leaked credential")
	}
	info, _ := os.Stat(filepath.Join(s.Options.Config.DataDir, "attempts", a.ID, "events.jsonl"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("journal not private")
	}
	file, err := os.OpenFile(filepath.Join(s.Options.Config.DataDir, "attempts", a.ID, "events.jsonl"), os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	err = json.NewEncoder(file).Encode(JournalEvent{Sequence: rows[len(rows)-1].Sequence + 1, At: time.Now().UTC(), Event: execution.Event{Type: "executor_diagnostic", Output: "LOCAL_DIAGNOSTIC_ONLY"}})
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		t.Fatal("cannot append operator diagnostic", err, closeErr)
	}
	rows, err = s.Journal(a.ID, 0)
	if err != nil || rows[len(rows)-1].Output != "LOCAL_DIAGNOSTIC_ONLY" {
		t.Fatal("operator lost diagnostic", err)
	}
	v = stateOf(t, s)
	r := ResumeRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Confirm: true, Note: "Use partial result"}
	if err := s.ResumeAttempt(a.ID, r); err != nil {
		t.Fatal(err)
	}
	if err := s.ResumeAttempt(a.ID, r); err != nil {
		t.Fatal("dedup", err)
	}
	next := stateOf(t, s).Attempts[1]
	waitAttempt(t, s, next.ID, func(a Attempt) bool { return a.Status == "candidate" })
	if calls.Load() != 2 || next.ParentAttempt != a.ID || stateOf(t, s).Entities[0].Status == "accepted" {
		t.Fatal("continuation auto-accepted or repeated")
	}
}
