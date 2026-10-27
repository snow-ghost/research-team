//go:build linux

package researchweb

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
)

func TestCounterReportBDD_ExtrasPreservedAndMathematicalQuestionsBlock(t *testing.T) {
	var r CounterReport
	body := `{"schema":"counterexample-report@2","outcome":"none_found","evidence":"Finite checks","assumptions":"Nat","limitations":"Not a proof","unresolved_obligations":[{"kind":"mathematical","text":"Case n=0 remains open"}]}`
	if decodeAgentReport(body, &r) != nil || validateCounterReport(r) != nil || r.Limitations != "Not a proof" || !counterHasObligations(r) {
		t.Fatal("details lost", r)
	}
	d := emptyData()
	if counterCleared(&d, nil, r) {
		t.Fatal("mathematical issue silently cleared")
	}
	for _, bad := range []string{
		`{"schema":"counterexample-report@2","outcome":"none_found","evidence":"Checked","unresolved_obligations":"none"}`,
		`{"schema":"counterexample-report@2","outcome":"none_found","evidence":"Checked"}`,
		`{"outcome":"none_found","evidence":"Checked","unresolved_obligations":[{"kind":"unknown","text":"Question"}]}`,
	} {
		var value CounterReport
		if decodeAgentReport(bad, &value) == nil && validateCounterReport(value) == nil {
			t.Fatal("invalid contract accepted", bad)
		}
	}
}

func TestProviderBDD_QueuedWorkIsNotDispatchedWhileEndpointHeld(t *testing.T) {
	o := optionsFor(t, "http://127.0.0.1:1/v1")
	var calls atomic.Int32
	o.Factory = func(execution.Profile, func(string) (string, bool)) (execution.Executor, error) {
		calls.Add(1)
		return nil, errors.New("test must not execute")
	}
	s := serviceFor(t, o)
	v := studyFor(t, s)
	target := v.Studies[0].Goal
	if err := s.ControlProvider(ProviderCommand{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Profile: "reader", Kind: "pause", Note: "Hold queued transmissions", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	id := identifier("run")
	profile := o.Profiles["reader"]
	if err := s.Store.Change(0, "", "", "Queue held endpoint", target, "test", func(d *Data) error {
		d.addTask(target, "Queued proof", "proof", "No dispatch before release")
		task := &d.Tasks[len(d.Tasks)-1]
		task.Attempt = id
		d.Attempts = append(d.Attempts, Attempt{ID: id, Target: target, TargetRevision: d.entity(target).Revision, TaskID: task.ID, Profile: "reader", ProfileConfiguration: &profile, Workspace: "source", Status: "queued", InputSnapshot: d.Revision + 1, RemoteOutcome: "not_started"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)
	v = stateOf(t, s)
	if calls.Load() != 0 || v.attempt(id).Status != "queued" || v.attempt(id).RemoteOutcome != "not_started" {
		t.Fatal("held queue dispatched")
	}
}

func TestReliabilityBDD_MetadataSurvivesSQLiteAndPostgresRestart(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			s, target := submittedService(t, backend, "verified")
			v := stateOf(t, s)
			if err := s.ControlProvider(ProviderCommand{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Profile: "reader", Kind: "pause", Note: "Recorded outage, interval not established", Confirm: true}); err != nil {
				t.Fatal(err)
			}
			if err := s.Store.Change(0, "", "", "Audit persistence fixture", target, "test", func(d *Data) error {
				d.CounterAudits = append(d.CounterAudits, CounterAudit{ID: identifier("counter-audit"), Target: target, Note: "Persistence only"})
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before := stateOf(t, s)
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			next := serviceFor(t, s.Options)
			after := stateOf(t, next)
			if hash(before.ProviderControls) != hash(after.ProviderControls) || hash(before.CounterAudits) != hash(after.CounterAudits) {
				t.Fatal("reliability metadata lost")
			}
			snapshot, err := next.Store.Snapshot(before.Revision)
			if err != nil || len(snapshot.CounterAudits) != 1 || len(snapshot.ProviderControls) != 1 {
				t.Fatal("history lost", err)
			}
		})
	}
}

func TestCounterReportBDD_OperatorAuditIsBoundToUnchangedResponseAndScope(t *testing.T) {
	s, p := packageFixture(t)
	v := stateOf(t, s)
	a := v.attempt(p.Result.CounterAttempt)
	result, err := s.readResult(*a)
	if err != nil {
		t.Fatal(err)
	}
	report := p.Result.Counter
	report.Assumptions = "Exact Goal.lean"
	report.Limitations = "No independent Lean rerun"
	report.Unresolved = json.RawMessage(`"Operator acceptance is pending"`)
	content, _ := json.Marshal(report)
	result.Candidate = string(content)
	body, _ := json.Marshal(result)
	if err = atomicFile(filepath.Join(s.Options.Config.DataDir, "attempts", a.ID, "result.json"), body); err != nil {
		t.Fatal(err)
	}
	if err = s.Store.Change(0, "", "", "Fixture expanded response", a.ID, "test", func(d *Data) error { d.attempt(a.ID).ResultSHA256 = hash(result); return nil }); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	a = v.attempt(a.ID)
	if counterCleared(&v.Data, a, report) {
		t.Fatal("legacy question guessed to be operational")
	}
	r := CounterAuditRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Attempt: a.ID, ReportSHA256: hash(report), Note: "The only listed obligation is operator acceptance; no mathematical claim remains open", Confirm: true}
	if err = s.AuditCounter(r); err != nil {
		t.Fatal(err)
	}
	if err = s.AuditCounter(r); err != nil {
		t.Fatal("idempotency", err)
	}
	v = stateOf(t, s)
	if len(v.CounterAudits) != 1 || !counterCleared(&v.Data, v.attempt(a.ID), report) {
		t.Fatal("audited question still blocked")
	}
	unchanged, err := s.readResult(*v.attempt(a.ID))
	if err != nil || unchanged.Candidate != result.Candidate {
		t.Fatal("original response rewritten", err)
	}
	changed := report
	changed.Limitations = "Changed after audit"
	if counterCleared(&v.Data, v.attempt(a.ID), changed) {
		t.Fatal("audit reused for changed report")
	}
	v.entity(a.Target).Assumptions += " altered"
	if counterCleared(&v.Data, v.attempt(a.ID), report) {
		t.Fatal("audit reused for changed assumptions")
	}
}

func TestProviderBDD_OnlyAvailabilityFailuresOpenTheGateAndSuccessDoesNotResume(t *testing.T) {
	p := optionsFor(t, "http://127.0.0.1:1/v1").Profiles["reader"]
	d := emptyData()
	result := execution.Result{Status: "failed", Diagnostics: []execution.Diagnostic{{Kind: "http_error", Stage: "response_headers", HTTPStatus: 504}}}
	for _, id := range []string{"first", "second"} {
		recordProviderOutcome(&d, Attempt{ID: id}, p, result)
	}
	if providerHeld(&d, p) {
		t.Fatal("gate opened too early")
	}
	recordProviderOutcome(&d, Attempt{ID: "third"}, p, result)
	if !providerHeld(&d, p) {
		t.Fatal("series did not hold provider")
	}
	recordProviderOutcome(&d, Attempt{ID: "third"}, p, result)
	if d.ProviderControls[0].ConsecutiveFailures != 3 {
		t.Fatal("duplicate event charged twice")
	}
	result.Status = "limit_reached"
	result.Diagnostics = []execution.Diagnostic{{Stage: "response_body", HTTPStatus: 200, FinishReason: "length"}}
	recordProviderOutcome(&d, Attempt{ID: "fourth"}, p, result)
	if !providerHeld(&d, p) || d.ProviderControls[0].ConsecutiveFailures != 0 || d.ProviderControls[0].LastClass != "output_limit" {
		t.Fatal("output limit blamed on outage or gate auto-resumed")
	}
	other := p
	other.ID = "another-model"
	model := *p.Model
	model.Model = "another-model"
	other.Model = &model
	if providerKey(other) != providerKey(p) || !providerHeld(&d, other) {
		t.Fatal("same endpoint did not share hold")
	}
}

func TestProviderBDD_PauseSurvivesRestartAndBlocksReservationWithoutNewCalls(t *testing.T) {
	o := optionsFor(t, "http://127.0.0.1:1/v1")
	s := serviceFor(t, o)
	v := studyFor(t, s)
	r := ProviderCommand{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Profile: "reader", Kind: "pause", Note: "Provider maintenance, reported by operator; no times established", Confirm: true}
	if err := s.ControlProvider(r); err != nil {
		t.Fatal(err)
	}
	if err := s.ControlProvider(r); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	if _, err := s.reserveStudyBudget(&v.Data, v.Studies[0].Goal, o.Profiles["reader"]); err == nil {
		t.Fatal("reservation bypassed provider hold")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = serviceFor(t, o)
	v = stateOf(t, s)
	if !providerHeld(&v.Data, o.Profiles["reader"]) || len(v.Attempts) != 0 {
		t.Fatal("restart resumed or launched a request")
	}
	if err := s.ControlProvider(ProviderCommand{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Profile: "reader", Kind: "resume", Note: "Operator permits new requests; previous unknown usage retained", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	if providerHeld(&v.Data, o.Profiles["reader"]) {
		t.Fatal("operator resume failed")
	}
}
