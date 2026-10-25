//go:build linux

package researchweb

import "testing"

func TestReadyPolicyBDD_HoldsComputationWithoutAcceptanceAndRequiresExplicitContinuation(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) { testReadyPolicy(t, backend) })
	}
}

func testReadyPolicy(t *testing.T, backend string) {
	_, p := packageFixture(t)
	s := packageDestination(t, "verified", backend)
	if err := s.ImportResultPackage(ImportResultPackageRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Package: p, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	v := waitVerification(t, s, p.Verification.ID)
	goal := v.entity(p.Target.ID)
	study := goal.Study
	if err := s.Store.Change(0, "", "", "Queued research and operator question", study, "test", func(d *Data) error {
		d.Tasks = append(d.Tasks, Task{ID: "answer-task", Target: goal.ID, Kind: "answer"}, Task{ID: "queued-task", Target: goal.ID, Kind: "proof"}, Task{ID: "remote-task", Target: goal.ID, Kind: "proof"})
		d.Attempts = append(d.Attempts, Attempt{ID: "queued", TaskID: "queued-task", Target: goal.ID, Status: "queued", RemoteOutcome: "not_started"}, Attempt{ID: "remote", TaskID: "remote-task", Target: goal.ID, Status: "awaiting_worker", RemoteWorker: "worker", RemoteOutcome: "unknown", ReservedModelRequests: 1}, Attempt{ID: "answer", TaskID: "answer-task", Target: goal.ID, Status: "queued"})
		d.Teams = append(d.Teams, ResearchTeam{ID: "ready-team", Study: study, Goal: goal.ID, Status: "running"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	req := StudyPolicyRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), PauseOnReady: true, Confirm: true}
	if err := s.SetStudyPolicy(study, req); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStudyPolicy(study, req); err != nil {
		t.Fatal("policy replay", err)
	}
	v = stateOf(t, s)
	if !studySearchSuspended(&v.Data, study) || v.entity(goal.ID).Status == "accepted" || v.Results[0].Status != "ready" || v.attempt("queued").Status != "cancelled" || v.attempt("remote").RemoteOutcome != "unknown" || v.attempt("remote").ReservedModelRequests != 1 || v.attempt("answer").Status != "queued" || v.team("ready-team").Status != "paused" {
		t.Fatal("hold changed evidence, answer or reserve", v.Attempts)
	}
	if err := s.ControlTeam("ready-team", TeamCommand{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Kind: "resume", Confirm: true}); err == nil {
		t.Fatal("ready result allowed new computation")
	}
	if err := s.Store.Change(0, "", "", "Mathematical question", goal.ID, "operator", func(d *Data) error {
		d.Findings = append(d.Findings, Finding{ID: "question", Target: goal.ID, Severity: "question", State: "open", Text: "Check this premise"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	if studySearchSuspended(&v.Data, study) || v.Results[0].Status != "needs_changes" || v.team("ready-team").Status != "paused" || v.attempt("queued").Status != "cancelled" {
		t.Fatal("question replayed work or retained readiness")
	}
}
