//go:build linux

package researchweb

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
	"github.com/snow-ghost/research-team/internal/researchcompare"
)

func TestRepresentationBDD_ServiceStoresFourChecksWithoutAcceptingTarget(t *testing.T) {
	s, p := packageFixture(t)
	s.Options.Lean = &leancheck.Config{}
	v := act(t, s, Action{Type: "CREATE_STUDY", Title: "Representation trial", Statement: "Equivalent routes", Assumptions: "Explicit equivalence"})
	target := v.Studies[len(v.Studies)-1].Goal
	source := "def Original : Prop := True\ndef Direct : Prop := True\ndef Bridge : Prop := Direct ↔ Original\n"
	o := leancheck.Goal{Source: source, Declaration: "Original", Candidate: "originalProof"}
	direct := leancheck.Goal{Source: source, Declaration: "Direct", Candidate: "directProof"}
	bridge := leancheck.Goal{Source: source, Declaration: "Bridge", Candidate: "bridgeProof"}
	if err := s.SetFormalGoal(target, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: o}); err != nil {
		t.Fatal(err)
	}
	id := identifier("library")
	module := "ResearchLemma_" + strings.TrimPrefix(id, "library-")
	dir := filepath.Join(s.Options.Config.DataDir, "library", id, "checked")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	artifact := []byte("fixture, not a real Lean module")
	if err := os.WriteFile(filepath.Join(dir, module+".olean"), artifact, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Change(0, "", "", "Library fixture", target, "test", func(d *Data) error {
		e := d.entity(p.Target.ID)
		d.Library = append(d.Library, LibraryEntry{ID: id, Lemma: e.ID, LemmaRevision: e.Revision, Result: e.ResearchResult, Goal: *e.FormalGoal, Module: module, Status: "ready", Report: &leancheck.ModuleReport{Status: "ready", Module: module, AuditSHA256: leancheck.AuditDigest(), ArtifactSHA256: leancheck.Digest(artifact), Artifacts: map[string]string{module + ".olean": leancheck.Digest(artifact)}}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	s.Options.ChainChecker = checkerFunc(func(_ context.Context, g leancheck.Goal, source, _ string) (leancheck.Report, error) {
		calls++
		return leancheck.Report{Status: "verified", AuditSHA256: leancheck.AuditDigest(), GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(source)}, nil
	})
	r := RepresentationRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Target: target, Library: id, Direct: direct, Bridge: bridge, BridgeSource: "import Goal\ntheorem bridgeProof : Bridge := Iff.rfl\n", BridgeAuthor: "fixture operator", Depth: 3, TimeoutSeconds: 10, Confirm: true}
	m, err := s.CompareRepresentation(context.Background(), r)
	v = stateOf(t, s)
	if err != nil || calls != 4 || m.Representation == nil || m.Report.Status != "verified" || v.entity(target).Status != "open" {
		t.Fatal(m, err, calls)
	}
	if _, err := os.Stat(filepath.Join(s.Options.Config.DataDir, "inspections", m.ID, "report.json")); err != nil {
		t.Fatal("local provenance missing", err)
	}
	if again, err := s.CompareRepresentation(context.Background(), r); err != nil || again.ID != m.ID || calls != 4 {
		t.Fatal("idempotent command reran checks", again, err, calls)
	}
	if err := s.AddMemory(MemoryRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Target: target, Kind: "note", Content: "Related representation", Conditions: "Explicit equivalence"}); err != nil {
		t.Fatal(err)
	}
	v = act(t, s, Action{Type: "LEMMA", Study: v.entity(target).Study, Title: "Related representation", Statement: "A separate relation", Assumptions: "Explicit equivalence"})
	related := v.Entities[len(v.Entities)-1].ID
	if err := s.SetFormalGoal(related, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: o, Libraries: []string{id}}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	if err := s.CreateMemoryRelation(MemoryRelationRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Memory: v.Memory[len(v.Memory)-1].ID, Target: related, Goal: leancheck.Goal{Source: "import " + module + "\ndef Relation : Prop := True", Declaration: "Relation", Candidate: "relationProof"}, Note: "Separate obligation needs the accepted module, not either unproved goal", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	relation := v.Memory[len(v.Memory)-1].Relations[0]
	obligation := v.entity(relation.Obligation)
	_, pins := s.libraryEnvironment(&v.Data, obligation)
	if len(obligation.RequiredLibraries) != 0 || len(obligation.Dependencies) != 1 || obligation.Dependencies[0] != p.Target.ID || pins[id] != leancheck.Digest(artifact) || relation.AutoReuse {
		t.Fatal("relation lost the module or assumed an unproved target", obligation, pins)
	}
}

func TestMemoryRelationBDD_SuggestionsAndObligationsNeverReuseFailureAcrossGoals(t *testing.T) {
	s, target := submittedService(t, "sqlite", "verified")
	v := stateOf(t, s)
	study := v.entity(target).Study
	if err := s.AddMemory(MemoryRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Target: target, Kind: "note", Content: "Bool functions and conditions", Conditions: "Explicit Bool conditions"}); err != nil {
		t.Fatal(err)
	}
	v = act(t, s, Action{Type: "LEMMA", Study: study, Title: "Bool functions", Statement: "Bool functions preserve a property", Assumptions: "Additional condition"})
	next := v.Entities[len(v.Entities)-1].ID
	goal := leancheck.Goal{Source: "def Next : Prop := True", Declaration: "Next", Candidate: "nextProof"}
	if err := s.SetFormalGoal(next, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: goal}); err != nil {
		t.Fatal(err)
	}
	hits, err := s.MemorySuggestions(next)
	if err != nil || len(hits) != 1 || hits[0].AutoReuse || hits[0].Memory.AutoReuse || len(hits[0].Differences) == 0 {
		t.Fatal(hits, err)
	}
	r := MemoryRelationRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Target: next, Memory: hits[0].Memory.ID, Goal: leancheck.Goal{Source: "def ConditionsRelation : Prop := True", Declaration: "ConditionsRelation", Candidate: "conditionProof"}, Note: "Operator-declared relation needs its own proof; no automatic failure transfer", Confirm: true}
	if err := s.CreateMemoryRelation(r); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateMemoryRelation(r); err != nil {
		t.Fatal("idempotency", err)
	}
	v = stateOf(t, s)
	if len(v.Memory[0].Relations) != 1 || len(v.entity(next).Dependencies) != 0 || v.entity(v.Memory[0].Relations[0].Obligation).Status != "open" {
		t.Fatal("relation silently accepted or changed dependencies")
	}
	obligation := v.Memory[0].Relations[0].Obligation
	if err := s.SetFormalGoal(obligation, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: goal}); err != nil {
		t.Fatal(err)
	}
	hits, err = s.MemorySuggestions(next)
	if err != nil || hits[0].Relations[0].Status != "stale" {
		t.Fatal("changed relation goal remained current", hits, err)
	}
	v = stateOf(t, s)
	goal.Source += "\n-- changed target"
	if err := s.SetFormalGoal(next, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: goal}); err != nil {
		t.Fatal(err)
	}
	hits, err = s.MemorySuggestions(next)
	if err != nil || hits[0].Relations[0].Status != "stale" || hits[0].Relations[0].AutoReuse {
		t.Fatal(hits, err)
	}
	v = studyFor(t, s)
	foreign := v.Studies[len(v.Studies)-1].Goal
	if err := s.CreateMemoryRelation(MemoryRelationRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Target: foreign, Memory: r.Memory, Goal: r.Goal, Note: r.Note, Confirm: true}); err == nil {
		t.Fatal("foreign study relation accepted")
	}
}

func TestBranchPolicyBDD_ApprovedBoundedChangePreservesBudgetAndManualHold(t *testing.T) {
	s, target := decisionFixture(t)
	if err := s.SetBranchPolicy("branch-test", BranchPolicyRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Policy: BranchPolicy{Enabled: true, Mode: "execute", Methods: []string{"representation"}, FailureThreshold: 1, MaxDecisions: 1}, Confirm: true}); err == nil {
		t.Fatal("automatic policy allowed without request budget")
	}
	v := stateOf(t, s)
	study := v.entity(target).Study
	if err := s.SetStudyBudget(study, BudgetRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Budget: StudyBudget{MaxAttempts: 10, MaxModelRequests: 20}, Note: "Finite approved budget", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	r := BranchPolicyRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Policy: BranchPolicy{Enabled: true, Mode: "execute", Methods: []string{"representation"}, FailureThreshold: 1, MaxDecisions: 1}, Confirm: true}
	if err := s.SetBranchPolicy("branch-test", r); err != nil {
		t.Fatal(err)
	}
	result := execution.Result{AttemptID: "run-policy-test", Status: "failed", RemoteOutcome: "response_received", Usage: &execution.Usage{InputTokens: 1, OutputTokens: 1}, Events: []execution.Event{{Type: "model_requested"}}}
	path := filepath.Join(s.Options.Config.DataDir, "attempts", "run-policy-test")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(result)
	if err := os.WriteFile(filepath.Join(path, "result.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Change(0, "", "", "Policy fixture", target, "test", func(d *Data) error {
		d.Branches[0].Status = "blocked"
		d.Branches[0].Team = "policy-team"
		d.Teams = append(d.Teams, ResearchTeam{ID: "policy-team", Study: study, Target: target, Status: "blocked", Current: map[string]string{}})
		d.Attempts = append(d.Attempts, Attempt{ID: "run-policy-test", TeamID: "policy-team", Target: target, TargetRevision: d.entity(target).Revision, Status: "failed", Role: "proof", RemoteOutcome: "response_received", ResultSHA256: hash(result)})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	choice := s.recommendBranch(&v.Data, v.Branches[0])
	if choice.Kind != "change_method" || choice.Executable {
		t.Fatal("global pause bypassed", choice)
	}
	d := v.Data
	d.Paused = false
	choice = s.recommendBranch(&d, d.Branches[0])
	if !choice.Executable || choice.Method != "representation" {
		t.Fatal(choice)
	}
	d.Branches[0].Held = true
	if s.recommendBranch(&d, d.Branches[0]).Executable {
		t.Fatal("manual hold bypassed")
	}
	d.Branches[0].Held = false
	originalAttempt := d.Attempts[0]
	d.Attempts[0].RemoteOutcome = "unknown"
	d.Attempts[0].ResultSHA256 = "missing"
	if got := s.recommendBranch(&d, d.Branches[0]); got.Kind != "needs_operator" {
		t.Fatal(got)
	}
	d.Attempts[0] = originalAttempt
	d.Branches[0].Policy.Used++
	if err := s.decideBranchData(&d, "branch-test", BranchDecisionRequest{RequestID: "policy-decision-test", Kind: "change_method", Method: "representation", Note: choice.Reason, NoteKind: "operational", Confirm: true}, "policy", r.Policy.SHA256); err != nil {
		t.Fatal(err)
	}
	if len(d.Branches) != 2 || d.Branches[1].Policy.Used != 1 || d.Branches[1].Basis.Source != "policy" || d.Branches[0].Decisions[0].Actor != "policy" || d.study(study).Budget.MaxModelRequests != 20 {
		t.Fatal("policy lost provenance or reset budget")
	}
	if d.Branches[0].Policy == d.Branches[1].Policy || d.Branches[1].Policy.Tried[len(d.Branches[1].Policy.Tried)-1] != "representation" {
		t.Fatal("replacement shares mutable policy or loses current method")
	}
	if s.recommendBranch(&d, d.Branches[1]).Executable {
		t.Fatal("authorization limit reset through replacement")
	}
	// Stop background scheduling so the transactional policy step can be
	// checked without launching the replacement branch's executor.
	s.cancel()
	s.wg.Wait()
	if err := s.Store.Change(0, "", "", "Enable isolated policy step", target, "test", func(d *Data) error {
		d.Paused = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s.advanceBranchPolicies()
	v = stateOf(t, s)
	if len(v.Branches) != 2 || v.Branches[1].Policy.Used != 1 || len(v.Branches[0].Decisions) != 1 || v.Branches[0].Decisions[0].Actor != "policy" || v.Branches[0].Decisions[0].PolicySHA256 != v.Branches[1].Policy.SHA256 || v.study(study).Budget.MaxModelRequests != 20 || len(v.Attempts) != 1 {
		t.Fatal("scheduler failed to persist one bounded decision", v.Branches)
	}
	revision := v.Revision
	for i := 0; i < 3; i++ {
		s.advanceBranchPolicies()
	}
	if next := stateOf(t, s); next.Revision != revision || len(next.Branches) != 2 || len(next.Attempts) != 1 {
		t.Fatal("scheduler repeated a consumed authorization")
	}
}

func TestEvaluationHealthBDD_ProcessAndReportFreshnessAreIndependent(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-time.Hour)
	file := filepath.Join(t.TempDir(), "process.json")
	pin := strings.Repeat("b", 64)
	w := EvaluationWatch{ConfigurationSHA256: pin, ProcessFile: file, StaleSeconds: 1200}
	write := func(state string, at time.Time) {
		t.Helper()
		b, _ := json.Marshal(researchcompare.Progress{ConfigurationSHA256: pin, ObservedAt: at, ProcessState: state, Phase: "preparation"})
		if err := os.WriteFile(file, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("running", now)
	h := evaluationHealth(w, &old, false, now)
	if !h.ProcessObservationFresh || !h.ReportStale || !h.NeedsAttention || h.Phase != "last_checkpoint_only" {
		t.Fatal(h)
	}
	write("stopped", now)
	h = evaluationHealth(w, &now, false, now)
	if !h.NeedsAttention {
		t.Fatal("stopped process shown healthy")
	}
	write("running", old)
	h = evaluationHealth(w, &now, false, now)
	if h.ProcessState != "unknown" || h.ProcessObservationFresh || !h.NeedsAttention {
		t.Fatal("stale heartbeat trusted", h)
	}
	h = evaluationHealth(w, &old, true, now)
	if h.NeedsAttention || h.ReportStale {
		t.Fatal("completed immutable report treated as stalled", h)
	}
}
