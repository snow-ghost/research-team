//go:build linux

package researchweb

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
)

func budgetFor(t *testing.T, s *Service, study string, attempts int, tokens int64, deadline *time.Time) {
	t.Helper()
	r := BudgetRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Confirm: true, Note: "Explicit test budget", Budget: StudyBudget{MaxAttempts: attempts, MaxOutputTokens: tokens, DeadlineAt: deadline}}
	if err := s.SetStudyBudget(study, r); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStudyBudget(study, r); err != nil {
		t.Fatal("idempotency", err)
	}
}

func TestBudgetBDD_ReservationAcrossPartsAndUnknownUsage(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			o := optionsFor(t, "http://127.0.0.1:1/v1")
			if backend == "postgres" {
				dsn, old := postgresDSN(t), o.Lookup
				o.Config.Database = DatabaseConfig{Driver: "postgres", DSNEnv: "TEST_DB"}
				o.Lookup = func(k string) (string, bool) {
					if k == "TEST_DB" {
						return dsn, true
					}
					return old(k)
				}
			}
			s := serviceFor(t, o)
			v := studyFor(t, s)
			study, root := v.Studies[0].ID, v.Studies[0].Goal
			v = act(t, s, Action{Type: "LEMMA", Study: study, Title: "Part", Statement: "P", Assumptions: "None"})
			part := v.Entities[1].ID
			budgetFor(t, s, study, 2, 2048, nil)
			// Freeze scheduling while reserving through the public start API.
			s.mu.Lock()
			for _, target := range []string{root, part} {
				v = act(t, s, Action{Type: "TASK", Target: target, Kind: "proof", Title: "Proof", Text: "P"})
				if err := s.Start(RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[len(v.Tasks)-1].ID, Profile: "reader", Workspace: "source", Confirm: true}); err != nil {
					s.mu.Unlock()
					t.Fatal(err)
				}
			}
			v = stateOf(t, s)
			ob := s.observeBudget(&v.Data, study)
			if ob.ReservedTokens != 2048 || ob.Attempts != 2 {
				s.mu.Unlock()
				t.Fatal(ob)
			}
			v = act(t, s, Action{Type: "TASK", Target: part, Kind: "proof", Title: "Extra", Text: "P"})
			if err := s.Start(RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[len(v.Tasks)-1].ID, Profile: "reader", Workspace: "source", Confirm: true}); err == nil {
				s.mu.Unlock()
				t.Fatal("part reset study budget")
			}
			if err := s.Store.Change(0, "", "", "Unknown completion", root, "test", func(d *Data) error {
				for i := range d.Attempts {
					d.Attempts[i].Status = "failed"
					d.Attempts[i].RemoteOutcome = "unknown"
					d.task(d.Attempts[i].TaskID).State = "failed"
				}
				return nil
			}); err != nil {
				s.mu.Unlock()
				t.Fatal(err)
			}
			s.mu.Unlock()
			v = stateOf(t, s)
			ob = s.observeBudget(&v.Data, study)
			if ob.ChargedTokens != 2048 || ob.UnknownUsage != 2 || ob.UnreservedUnknown != 0 {
				t.Fatal("unknown cost became zero", ob)
			}
			budgetFor(t, s, study, 3, 2048, nil)
			v = stateOf(t, s)
			if err := s.Start(RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[len(v.Tasks)-1].ID, Profile: "reader", Workspace: "source", Confirm: true}); err == nil {
				t.Fatal("spent unknown reservation reused")
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s = serviceFor(t, o)
			v = stateOf(t, s)
			if v.Studies[0].Budget.MaxAttempts != 3 || s.observeBudget(&v.Data, study).ChargedTokens != 2048 {
				t.Fatal("restart lost budget")
			}
		})
	}
}

func TestBudgetBDD_ActualUsageReleasesUnusedReservationAndDeadlineCancels(t *testing.T) {
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); completeModel(w, "P") }))
	defer model.Close()
	s := serviceFor(t, optionsFor(t, model.URL+"/v1"))
	v := studyFor(t, s)
	study, goal := v.Studies[0].ID, v.Studies[0].Goal
	budgetFor(t, s, study, 3, 1028, nil)
	for i := 0; i < 2; i++ {
		v = act(t, s, Action{Type: "TASK", Target: goal, Kind: "proof", Title: "Proof", Text: "P"})
		if err := s.Start(RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[len(v.Tasks)-1].ID, Profile: "reader", Workspace: "source", Confirm: true}); err != nil {
			t.Fatal(err)
		}
		a := stateOf(t, s).Attempts[i]
		waitAttempt(t, s, a.ID, func(a Attempt) bool { return !active(a.Status) })
	}
	v = stateOf(t, s)
	ob := s.observeBudget(&v.Data, study)
	if calls.Load() != 2 || ob.KnownOutputTokens != 8 || ob.ChargedTokens != 8 || ob.ReservedTokens != 0 {
		t.Fatal(ob)
	}
	past := time.Now().Add(-time.Second)
	budgetFor(t, s, study, 3, 1028, &past)
	v = act(t, s, Action{Type: "TASK", Target: goal, Kind: "proof", Title: "Late", Text: "P"})
	if err := s.Start(RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[len(v.Tasks)-1].ID, Profile: "reader", Workspace: "source", Confirm: true}); err == nil {
		t.Fatal("deadline allowed start")
	}
	if calls.Load() != 2 {
		t.Fatal("rejected call reached model")
	}
}

func TestBudgetBDD_DeadlineStopsRunningExecutor(t *testing.T) {
	o := optionsFor(t, "http://127.0.0.1:1/v1")
	o.Factory = func(p execution.Profile, _ func(string) (string, bool)) (execution.Executor, error) {
		return budgetWaitingExecutor{}, nil
	}
	s := serviceFor(t, o)
	v := studyFor(t, s)
	study, goal := v.Studies[0].ID, v.Studies[0].Goal
	deadline := time.Now().Add(800 * time.Millisecond)
	budgetFor(t, s, study, 3, 2048, &deadline)
	v = act(t, s, Action{Type: "TASK", Target: goal, Kind: "proof", Title: "Long", Text: "P"})
	if err := s.Start(RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[len(v.Tasks)-1].ID, Profile: "reader", Workspace: "source", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	a := stateOf(t, s).Attempts[0]
	waitAttempt(t, s, a.ID, func(a Attempt) bool { return a.Status == "cancelled" })
	v = stateOf(t, s)
	if !s.observeBudget(&v.Data, study).DeadlineReached {
		t.Fatal("deadline not reported")
	}
}

type budgetWaitingExecutor struct{}

func (budgetWaitingExecutor) Run(ctx context.Context, t execution.Task) (execution.Result, error) {
	<-ctx.Done()
	return execution.Result{Status: "cancelled", RemoteOutcome: "unknown"}, ctx.Err()
}

func TestBudgetBDD_HTTPRequiresOperatorAndConfirmation(t *testing.T) {
	s := serviceFor(t, optionsFor(t, "http://127.0.0.1:1/v1"))
	v := studyFor(t, s)
	h, err := NewHTTP(s, "operator-test-key-00000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	body, _ := json.Marshal(BudgetRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Budget: StudyBudget{MaxAttempts: 2}, Confirm: true, Note: "Test"})
	r, err := http.Post(server.URL+"/api/studies/"+v.Studies[0].ID+"/budget", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusUnauthorized && r.StatusCode != http.StatusForbidden {
		t.Fatal(r.StatusCode)
	}
}

func TestBudgetBDD_TeamsAndCyclesCannotResetTheCommonLimit(t *testing.T) {
	s := serviceFor(t, teamOptions(t, "http://127.0.0.1:1/v1"))
	v := studyFor(t, s)
	study, goal := v.Studies[0].ID, v.Studies[0].Goal
	budgetFor(t, s, study, 1, 0, nil)
	d := stateOf(t, s).Data
	d.Attempts = append(d.Attempts, Attempt{ID: "previous", Target: goal, Status: "failed", RemoteOutcome: "not_started"})
	team := ResearchTeam{ID: "team-test", Study: study, Goal: goal, Target: goal, Workspace: "source", Profiles: map[string]string{"proof": "proof"}, Current: map[string]string{}, MaxAttempts: 8}
	if err := s.queueTeamAttempt(&d, &team, "proof", ""); err == nil || len(d.Attempts) != 1 || len(d.Tasks) != 0 || team.UsedAttempts != 0 {
		t.Fatal("team bypassed common limit")
	}
	c := Cycle{ID: "cycle-test", Study: study, Goal: goal, Profile: "proof", ProfileSHA: hash(s.Options.Profiles["proof"]), Workspace: "source", Status: "running", MaxAttempts: 8}
	if err := s.advanceCycle(&d, &c); err != nil {
		t.Fatal(err)
	}
	if c.Status != "blocked" || len(d.Attempts) != 1 || c.UsedAttempts != 0 {
		t.Fatal("cycle bypassed common limit", c)
	}
}

func TestBudgetBDD_DistinguishesTokenByteAndTransportLimits(t *testing.T) {
	limits := execution.Limits{MaxOutputTokens: 2048, MaxSteps: 1}
	cases := []struct {
		result execution.Result
		want   string
	}{
		{execution.Result{Status: "limit_reached", Usage: &execution.Usage{OutputTokens: 2048}}, "output_tokens"},
		{execution.Result{Status: "limit_reached", Diagnostics: []execution.Diagnostic{{ByteLimitReached: true}}}, "output_bytes"},
		{execution.Result{Status: "limit_reached"}, "execution_limit"},
		{execution.Result{Status: "limit_reached", Events: []execution.Event{{Type: "acp_turn_limit_reached"}}}, "execution_steps"},
		{execution.Result{Status: "failed", Diagnostics: []execution.Diagnostic{{Kind: "rpc_error"}}}, "acp_or_transport_error"},
		{execution.Result{Status: "timed_out"}, "attempt_timeout"},
	}
	for _, c := range cases {
		if got := attemptStopCause(c.result, &limits); got != c.want {
			t.Fatal(got, c.want)
		}
	}
}
