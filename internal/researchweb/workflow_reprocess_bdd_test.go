//go:build linux

package researchweb

import "testing"

func TestWorkflowBDD_ReprocessingDoesNotReplayHistoricalUnknownUsage(t *testing.T) {
	s := serviceFor(t, optionsFor(t, "http://127.0.0.1:1/v1"))
	v := studyFor(t, s)
	study, target := v.Studies[0].ID, v.Studies[0].Goal
	if err := s.Store.Change(0, "", "", "Received reports and historical unknown usage", target, "test", func(d *Data) error {
		d.Paused = true
		d.study(study).Workflow = &ResearchWorkflow{ID: "workflow-fixture", Target: target, Status: "needs_operator", Proposal: "proposal-fixture", PlanningAttempt: "plan-fixture"}
		d.Proposals = append(d.Proposals, Decomposition{ID: "proposal-fixture", Status: "applied", Target: target})
		d.Branches = append(d.Branches, ResearchBranch{ID: "branch-fixture", Study: study, Target: target, Plan: "proposal-fixture", Team: "team-fixture", Status: "blocked"})
		d.Teams = append(d.Teams, ResearchTeam{ID: "team-fixture", Study: study, Goal: target, Target: target, Status: "blocked", Current: map[string]string{"proof": "proof-fixture"}})
		d.Attempts = append(d.Attempts, Attempt{ID: "historical-failure", Target: target, Status: "failed", RemoteOutcome: "unknown", ReservedOutputTokens: 32768, ReservedModelRequests: 1}, Attempt{ID: "plan-fixture", Target: target, Status: "candidate", RemoteOutcome: "response_received", ResultSHA256: "plan-hash", ReservedOutputTokens: 32768, ReservedModelRequests: 1}, Attempt{ID: "proof-fixture", Target: target, Status: "candidate", RemoteOutcome: "response_received", ResultSHA256: "proof-hash", ReservedOutputTokens: 32768, ReservedModelRequests: 1})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	before := s.observeBudget(&v.Data, study)
	r := WorkflowCommand{Kind: "reprocess", Note: "Reprocess received reports, never resend historical failures", Confirm: true, ExpectedRevision: v.Revision, RequestID: identifier("cmd")}
	if err := s.ControlWorkflow(study, r); err != nil {
		t.Fatal(err)
	}
	if err := s.ControlWorkflow(study, r); err != nil {
		t.Fatal("idempotency", err)
	}
	v = stateOf(t, s)
	after := s.observeBudget(&v.Data, study)
	if !v.Paused || len(v.Attempts) != 3 || before.ChargedTokens != after.ChargedTokens || before.UnknownUsage != after.UnknownUsage || v.study(study).Workflow.Status != "running" {
		t.Fatal("reprocessing reset budget or created requests")
	}
	if err := s.Store.Change(0, "", "", "Unknown current report", target, "test", func(d *Data) error {
		d.study(study).Workflow.Status = "needs_operator"
		d.attempt("proof-fixture").RemoteOutcome = "unknown"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	r.ExpectedRevision = stateOf(t, s).Revision
	r.RequestID = identifier("cmd")
	if s.ControlWorkflow(study, r) == nil {
		t.Fatal("unknown current request replayed")
	}
}
