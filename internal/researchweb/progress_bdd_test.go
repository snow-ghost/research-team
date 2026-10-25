//go:build linux

package researchweb

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
)

func decisionFixture(t *testing.T) (*Service, string) {
	t.Helper()
	s, target := submittedService(t, "sqlite", "verified")
	act(t, s, Action{Type: "PAUSE"})
	if err := s.Store.Change(0, "", "", "Decision fixture", target, "test", func(d *Data) error {
		e := d.entity(target)
		d.Branches = append(d.Branches, ResearchBranch{ID: "branch-test", Study: e.Study, Target: target, TargetRevision: e.Revision, GoalSHA256: leancheck.Digest(*e.FormalGoal), Method: "induction", Status: "ready", Configuration: TeamRequest{MaxAttempts: 6, Methods: []string{"induction"}}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return s, target
}

func TestBranchDecisionBDD_SplitContinueChangeStopAreExplicitAndIdempotent(t *testing.T) {
	s, _ := decisionFixture(t)
	decide := func(id, kind, method string) {
		t.Helper()
		r := BranchDecisionRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Kind: kind, Method: method, Note: "Explicit bounded decision", NoteKind: "operational", Confirm: true}
		bad := r
		bad.Confirm = false
		if err := s.DecideBranch(id, bad); err == nil {
			t.Fatal("unconfirmed decision committed")
		}
		if err := s.DecideBranch(id, r); err != nil {
			t.Fatal(err)
		}
		if err := s.DecideBranch(id, r); err != nil {
			t.Fatal("idempotency", err)
		}
	}
	decide("branch-test", "split", "")
	v := stateOf(t, s)
	if !v.Branches[0].Held || v.Branches[0].Status != "paused" || len(v.Proposals) != 0 || len(v.Branches[0].Decisions) != 1 {
		t.Fatal("split silently created a plan or did not hold branch", v.Branches)
	}
	s.advanceBranches()
	if stateOf(t, s).Branches[0].Status != "paused" {
		t.Fatal("scheduler bypassed split hold")
	}
	decide("branch-test", "continue", "")
	if !stateOf(t, s).Paused || stateOf(t, s).Branches[0].Held {
		t.Fatal("continue unpaused global scheduler")
	}
	decide("branch-test", "change_method", "representation")
	v = stateOf(t, s)
	old, next := v.Branches[0], v.Branches[1]
	if len(old.Decisions) != 3 || old.Status != "abandoned" || next.Parent != old.ID || next.TargetRevision != old.TargetRevision || next.GoalSHA256 != old.GoalSHA256 || next.Configuration.MaxAttempts != 6 || next.Configuration.Methods[0] != "representation" || next.Basis.Source != "operator" {
		t.Fatal("method replacement lost provenance or limits", v.Branches)
	}
	decide(next.ID, "stop", "")
	if stateOf(t, s).Branches[1].Status != "abandoned" {
		t.Fatal("stop not applied")
	}
	if err := s.DecideBranch(next.ID, BranchDecisionRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Kind: "continue", Note: "Retry", Confirm: true}); err == nil {
		t.Fatal("terminal branch resumed")
	}
}

func TestBranchDecisionBDD_HeldTeamCannotResumeAndInvalidChangeRollsBack(t *testing.T) {
	s, target := decisionFixture(t)
	if err := s.Store.Change(0, "", "", "Team fixture", target, "test", func(d *Data) error {
		d.Branches[0].Team = "team-test"
		d.Teams = append(d.Teams, ResearchTeam{ID: "team-test", Study: d.entity(target).Study, Target: target, Status: "running", Current: map[string]string{}})
		d.Attempts = append(d.Attempts, Attempt{ID: "run-remote", Target: target, TeamID: "team-test", Status: "running", RemoteWorker: "worker-a", ReservedModelRequests: 2, RemoteOutcome: "unknown"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	r := BranchDecisionRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Kind: "change_method", Method: "not-a-method", Note: "Invalid change", Confirm: true}
	if err := s.DecideBranch("branch-test", r); err == nil {
		t.Fatal("unknown method accepted")
	}
	if v := stateOf(t, s); v.team("team-test").Status != "running" || v.attempt("run-remote").Status != "running" {
		t.Fatal("failed transaction cancelled work")
	}
	r.RequestID = identifier("cmd")
	r.Kind = "split"
	r.Method = ""
	if err := s.DecideBranch("branch-test", r); err != nil {
		t.Fatal(err)
	}
	if err := s.ControlTeam("team-test", TeamCommand{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Kind: "resume", Note: "Bypass", Confirm: true}); err == nil {
		t.Fatal("held branch bypassed through team API")
	}
	if err := s.Store.Change(0, "", "", "Blocked team fixture", target, "test", func(d *Data) error {
		d.team("team-test").Status = "blocked"
		d.team("team-test").Stage = "exploring"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.ControlTeam("team-test", TeamCommand{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Kind: "reprocess", Note: "Bypass through saved results", Confirm: true}); err == nil {
		t.Fatal("held branch bypassed through reprocess")
	}
	r.ExpectedRevision = stateOf(t, s).Revision
	r.RequestID = identifier("cmd")
	r.Kind = "continue"
	if err := s.DecideBranch("branch-test", r); err != nil {
		t.Fatal(err)
	}
	r.ExpectedRevision = stateOf(t, s).Revision
	r.RequestID = identifier("cmd")
	r.Kind = "stop"
	if err := s.DecideBranch("branch-test", r); err != nil {
		t.Fatal(err)
	}
	v := stateOf(t, s)
	if a := v.attempt("run-remote"); a.Status != "cancelled" || a.ReservedModelRequests != 2 || a.RemoteOutcome != "unknown" {
		t.Fatal("remote cancellation lost reserve", a)
	}
}

func TestStudyHistoryBDD_CursorCrossesForeignEventsWithoutLosingPage(t *testing.T) {
	s, target := submittedService(t, "sqlite", "verified")
	v := stateOf(t, s)
	study := v.entity(target).Study
	start := v.Revision
	for i := 0; i < 201; i++ {
		if err := s.Store.Change(0, "", "", "Foreign global event", "foreign-target", "test", func(*Data) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AddMemory(MemoryRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Target: target, Kind: "note", Content: "Selected study event after foreign pages"}); err != nil {
		t.Fatal(err)
	}
	end := stateOf(t, s).Revision
	first, err := s.StudyHistory(study, start, end)
	if err != nil || !first.HasMore || len(first.Events) != 0 || first.NextAfter != start+200 {
		t.Fatal(first, err)
	}
	second, err := s.StudyHistory(study, first.NextAfter, end)
	if err != nil || second.HasMore || len(second.Events) != 1 || second.NextAfter != end {
		t.Fatal(second, err)
	}
}

func TestMemoryScopeBDD_ExactConditionsAndEvidenceNotMathematicalRejection(t *testing.T) {
	s, target := decisionFixture(t)
	if err := s.Store.Change(0, "", "", "Failure fixture", target, "test", func(d *Data) error {
		d.Attempts = append(d.Attempts, Attempt{ID: "failed-a", Target: target, TargetRevision: d.entity(target).Revision, Status: "timed_out", ResultSHA256: "result-a", RemoteOutcome: "response_received"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	add := func(kind, conditions string) {
		t.Helper()
		if err := s.AddMemory(MemoryRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Target: target, Kind: kind, Content: "Timed out; does not disprove induction", Attempt: "failed-a", Method: "induction", Conditions: conditions}); err != nil {
			t.Fatal(err)
		}
	}
	add("failed_method", "")
	add("failed_method", "Additional premise")
	current := stateOf(t, s)
	add("note", current.entity(target).Assumptions)
	v := stateOf(t, s)
	hits := memoryForTarget(v.Data, target)
	if len(hits) != 2 || !hits[0].AutoReuse || hits[0].FailureClass != "timeout" || hits[1].AutoReuse {
		t.Fatal(hits)
	}
	legacy := v.Memory[0]
	legacy.ScopeSHA256 = ""
	if memoryEntryTrust(&v.Data, legacy) != "legacy_unscoped" || memoryReusable(&v.Data, legacy) {
		t.Fatal("legacy failure automatically reused")
	}
	v.attempt("failed-a").ResultSHA256 = "changed"
	if memoryEntryTrust(&v.Data, v.Memory[0]) != "stale" {
		t.Fatal("changed failure evidence accepted")
	}
	if got := failedAttemptClass(Attempt{Status: "failed", StopCause: attemptStopCause(execution.Result{Status: "failed", FailureCode: "skill_result_contract"}, nil)}); got != "format" {
		t.Fatal(got)
	}
	if got := failedAttemptClass(Attempt{Status: "timed_out", RemoteOutcome: "unknown"}); got != "unknown_remote_outcome" {
		t.Fatal(got)
	}
}

func TestStudyHistoryBDD_ScopedSnapshotsDiffAndStaleChecks(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			s, target := submittedService(t, backend, "verified")
			initial := stateOf(t, s)
			study := initial.entity(target).Study
			id := submitFor(t, s, target, "Codex")
			v := waitVerification(t, s, id)
			if err := s.AttachVerification(id, AttachVerificationRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd")}); err != nil {
				t.Fatal(err)
			}
			before := stateOf(t, s)
			v = act(t, s, Action{Type: "CREATE_STUDY", Title: "Foreign study", Statement: "PRIVATE-OTHER-STUDY", Assumptions: "Private"})
			foreign := v.Studies[len(v.Studies)-1].Goal
			changed := *v.entity(target).FormalGoal
			changed.Source += "\n-- new version"
			if err := s.SetFormalGoal(target, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: changed}); err != nil {
				t.Fatal(err)
			}
			after := stateOf(t, s)
			diff, err := s.DiffStudy(study, "", before.Revision, after.Revision)
			if err != nil || len(diff.Entities) != 1 || len(diff.Checks) != 1 || !diff.Checks[0].BeforeCurrent || diff.Checks[0].AfterCurrent {
				t.Fatal(diff, err)
			}
			body, _ := json.Marshal(diff)
			if strings.Contains(string(body), "PRIVATE-OTHER-STUDY") {
				t.Fatal("foreign statement leaked")
			}
			if _, err := s.DiffStudy(study, foreign, before.Revision, after.Revision); err == nil {
				t.Fatal("foreign target allowed")
			}
			page, err := s.StudyHistory(study, 0, 0)
			if err != nil || len(page.Events) == 0 {
				t.Fatal(page, err)
			}
			creation := false
			for _, e := range page.Events {
				if e.Target == foreign || e.Detail == "PRIVATE-OTHER-STUDY" {
					t.Fatal("foreign event leaked")
				}
				creation = creation || e.ScopeBasis == "snapshot_creation"
			}
			if !creation {
				t.Fatal("legacy creation was not attributed from snapshots")
			}
			if stateOf(t, s).Revision != after.Revision {
				t.Fatal("read-only comparison changed state")
			}
			unchanged, err := s.DiffStudy(study, target, after.Revision, after.Revision)
			if err != nil || len(unchanged.Entities) != 0 || len(unchanged.Checks) != 0 {
				t.Fatal(unchanged, err)
			}
		})
	}
}
