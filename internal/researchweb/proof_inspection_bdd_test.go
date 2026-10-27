//go:build linux

package researchweb

import (
	"testing"

	"github.com/snow-ghost/research-team/internal/execution"
)

func TestProofInspectionBDD_ToolLimitIsNotProviderUnavailability(t *testing.T) {
	r := execution.Result{Status: "limit_reached", Events: []execution.Event{{Type: "limit_reason", Status: "tool_or_step_limit"}}, Diagnostics: []execution.Diagnostic{{HTTPStatus: 200, Stage: "response_body", FinishReason: "tool_calls"}}}
	if inspectionFailureClass(r) != "tool_limit" || providerFailureClass(r) != "response_received" {
		t.Fatal("tool limit changed provider admission or was hidden")
	}
	r = execution.Result{Status: "timed_out", Diagnostics: []execution.Diagnostic{{Stage: "dns", Kind: "timeout"}}}
	if inspectionFailureClass(r) != "dns_timeout" {
		t.Fatal("network stage missing")
	}
}

func TestProofInspectionBDD_ReceivedCounterCandidateCanStillHaveInvalidReportFormat(t *testing.T) {
	a := Attempt{Status: "candidate"}
	r := execution.Result{Status: "candidate", Candidate: `{"outcome":"none_found","evidence":{"cases":[]}}`, Diagnostics: []execution.Diagnostic{{HTTPStatus: 200, Stage: "response_body", FinishReason: "stop"}}}
	if inspectionCandidateClass("counterexample", a, r) != "report_format" || providerFailureClass(r) != "response_received" {
		t.Fatal("received malformed evidence hidden or counted as provider failure")
	}
	r.Candidate = `{"outcome":"none_found","evidence":"Only finite cases checked"}`
	if inspectionCandidateClass("counterexample", a, r) != "response_received" {
		t.Fatal("valid legacy counter report rejected")
	}
}

func TestProofInspectionBDD_VerificationDoesNotMeanAcceptanceAndReadsDoNotChangeHistory(t *testing.T) {
	s, target := submittedService(t, "sqlite", "verified")
	id := submitFor(t, s, target, "Codex")
	waitVerification(t, s, id)
	before := stateOf(t, s)
	i, err := s.InspectProof(target)
	if err != nil {
		t.Fatal(err)
	}
	if i.Target.Status != "open" || i.Verification == nil || i.Verification.Status != "verified" || i.Result != nil || i.Files != nil || i.Gates[4].Status != "open" {
		t.Fatal("verification presented as acceptance", i)
	}
	if stateOf(t, s).Revision != before.Revision {
		t.Fatal("inspection mutated history")
	}
	if err = s.Store.Change(0, "", "", "Blocking finding fixture", target, "test", func(d *Data) error {
		d.Findings = append(d.Findings, Finding{ID: identifier("F"), Target: target, Severity: "question", State: "open", Text: "Unproved correspondence"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	i, err = s.InspectProof(target)
	if err != nil || i.Gates[3].Status != "needs_changes" || i.Verification.Status != "verified" {
		t.Fatal("mathematical question hidden by native verification", err)
	}
	if _, err = s.InspectProof("missing"); err == nil {
		t.Fatal("missing target accepted")
	}
}
