//go:build linux

package researchweb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/snow-ghost/research-team/internal/execution"
)

func TestTeamBDD_ConfigureLimitsDoesNotRunAndPreservesProfilePins(t *testing.T) {
	o := teamOptions(t, "http://127.0.0.1:1/v1")
	s := serviceFor(t, o)
	v := studyFor(t, s)
	study := v.Studies[0]
	id := identifier("team")
	if err := s.Store.Change(0, "", "", "Paused team", study.ID, "test", func(d *Data) error {
		d.Teams = append(d.Teams, ResearchTeam{ID: id, Study: study.ID, Goal: study.Goal, Status: "paused", Stage: "planning", Profiles: map[string]string{"proof": "proof"}, ProfileHashes: map[string]string{"proof": hash(o.Profiles["proof"])}, Workspace: "source", MaxAttempts: 6, UsedAttempts: 4, Current: map[string]string{}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	limits := o.Profiles["proof"].Limits
	limits.MaxOutputTokens = 16384
	limits.TimeoutSeconds = 360
	command := TeamCommand{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Kind: "configure", Confirm: true, MaxAttempts: 8, RoleLimits: map[string]execution.Limits{"proof": limits}}
	if err := s.ControlTeam(id, command); err != nil {
		t.Fatal(err)
	}
	if err := s.ControlTeam(id, command); err != nil {
		t.Fatal("duplicate", err)
	}
	v = stateOf(t, s)
	if len(v.Attempts) != 0 || len(v.Tasks) != 0 || v.Teams[0].Status != "paused" || v.Teams[0].RoleLimits["proof"].MaxOutputTokens != 16384 || v.Teams[0].ProfileHashes["proof"] != hash(o.Profiles["proof"]) {
		t.Fatal("configuration caused execution or changed profile pins")
	}
	if v.Teams[0].UsedAttempts != 4 || v.Teams[0].MaxAttempts != 8 {
		t.Fatal("budget update lost consumed attempts or was not applied")
	}
}

func TestTeamBDD_FormalRepairReservesReviewAttempt(t *testing.T) {
	s := serviceFor(t, teamOptions(t, "http://127.0.0.1:1/v1"))
	v := studyFor(t, s)
	d := v.Data
	goal := d.Studies[0].Goal
	proof, counter, formal := identifier("run"), identifier("run"), identifier("run")
	for _, id := range []string{proof, counter, formal} {
		d.Attempts = append(d.Attempts, Attempt{ID: id, Target: goal, Status: "candidate"})
	}
	verification := identifier("verify")
	d.Verifications = append(d.Verifications, Verification{ID: verification, Target: goal, Status: "failed"})
	team := ResearchTeam{ID: identifier("team"), Study: d.Studies[0].ID, Goal: goal, Target: goal, TargetRevision: d.entity(goal).Revision,
		Status: "running", Stage: "verifying", RequireLean: true, MaxAttempts: 4, UsedAttempts: 3, Workspace: "source",
		Verification: verification, Current: map[string]string{"proof": proof, "counterexample": counter, "formalize": formal},
		Profiles: map[string]string{"proof": "proof", "counterexample": "counterexample", "formalize": "formalize", "review": "review"}}
	if err := s.advanceTeam(&d, &team); err != nil {
		t.Fatal(err)
	}
	if team.Status != "blocked" || team.UsedAttempts != 3 || len(d.Attempts) != 3 || len(d.Tasks) != 0 {
		t.Fatal("repair consumed the last attempt reserved for review")
	}
	team.Status, team.MaxAttempts = "running", 5
	if err := s.advanceTeam(&d, &team); err != nil {
		t.Fatal(err)
	}
	if team.Stage != "formalize" || team.UsedAttempts != 4 || len(d.Attempts) != 4 || d.Attempts[3].ParentAttempt != formal {
		t.Fatal("repair did not start after an explicit budget increase")
	}
	if err := s.queueTeamAttempt(&d, &team, "review", ""); err != nil {
		t.Fatal("reserved review could not be queued", err)
	}
	if team.UsedAttempts != 5 || len(d.Attempts) != 5 || d.Attempts[4].Role != "review" {
		t.Fatal("review slot was lost")
	}
}

func TestTeamBDD_BudgetChangeRejectsInvalidAndUnconfirmedCommands(t *testing.T) {
	for _, tc := range []struct {
		name    string
		kind    string
		max     int
		confirm bool
		status  string
	}{
		{"unconfirmed", "configure", 8, false, "paused"},
		{"below_minimum", "configure", 3, true, "paused"},
		{"above_maximum", "configure", 21, true, "paused"},
		{"negative", "configure", -1, true, "paused"},
		{"below_consumed", "configure", 4, true, "paused"},
		{"pause_with_budget", "pause", 8, true, "paused"},
		{"running", "configure", 8, true, "running"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := serviceFor(t, teamOptions(t, "http://127.0.0.1:1/v1"))
			v := studyFor(t, s)
			id := identifier("team")
			if err := s.Store.Change(0, "", "", "Seed team", v.Studies[0].ID, "test", func(d *Data) error {
				d.Teams = append(d.Teams, ResearchTeam{ID: id, Study: v.Studies[0].ID, Goal: v.Studies[0].Goal, Status: tc.status, MaxAttempts: 6, UsedAttempts: 5})
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			v = stateOf(t, s)
			if err := s.ControlTeam(id, TeamCommand{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Kind: tc.kind, Confirm: tc.confirm, MaxAttempts: tc.max}); err == nil {
				t.Fatal("invalid budget change accepted")
			}
			after := stateOf(t, s)
			if after.Revision != v.Revision || after.Teams[0].MaxAttempts != 6 || after.Teams[0].UsedAttempts != 5 || after.Teams[0].Status != tc.status || len(after.Attempts) != 0 {
				t.Fatal("rejected budget change mutated state")
			}
		})
	}
}

func TestTeamBDD_ResumeWithBudgetPreservesProofAndQueuesOnlyRetry(t *testing.T) {
	s := serviceFor(t, teamOptions(t, "http://127.0.0.1:1/v1"))
	v := studyFor(t, s)
	id := identifier("team")
	proof, counter := identifier("run"), identifier("run")
	if err := s.Store.Change(0, "", "", "Seed blocked team", v.Studies[0].ID, "test", func(d *Data) error {
		goal := d.entity(v.Studies[0].Goal)
		d.Attempts = append(d.Attempts, Attempt{ID: proof, Profile: "proof", Status: "candidate"}, Attempt{ID: counter, Profile: "counterexample", Status: "candidate"})
		d.Teams = append(d.Teams, ResearchTeam{ID: id, Study: goal.Study, Goal: goal.ID, Target: goal.ID, TargetRevision: goal.Revision, Status: "blocked", Stage: "exploring", RetryRole: "counterexample", Profiles: map[string]string{"proof": "proof", "counterexample": "counterexample"}, Workspace: "source", MaxAttempts: 6, UsedAttempts: 4, Current: map[string]string{"proof": proof, "counterexample": counter}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	command := TeamCommand{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Kind: "resume", Confirm: true, MaxAttempts: 8}
	if err := s.ControlTeam(id, command); err != nil {
		t.Fatal(err)
	}
	if err := s.ControlTeam(id, command); err != nil {
		t.Fatal("duplicate", err)
	}
	v = stateOf(t, s)
	team := v.Teams[0]
	if team.Status != "running" || team.MaxAttempts != 8 || team.UsedAttempts != 5 || team.Current["proof"] != proof || team.Current["counterexample"] == counter || len(v.Attempts) != 3 || team.RetryRole != "" {
		t.Fatal("resume reran proof, reset history, or duplicated retry")
	}
	if a := v.attempt(team.Current["counterexample"]); a.ParentAttempt != counter || a.Role != "counterexample" || a.Status != "queued" {
		t.Fatal("retry lost lineage")
	}
}
func TestJournalBDD_LegacyCoddyTextIsNotPassedToContinuation(t *testing.T) {
	o := optionsFor(t, "http://127.0.0.1:1/v1")
	s := serviceFor(t, o)
	s.Options.Profiles["coddy-legacy"] = execution.Profile{External: &execution.ExternalConfig{Provider: "coddy-agent"}}
	dir := filepath.Join(s.Options.Config.DataDir, "attempts", "legacy-attempt")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	result := execution.Result{AttemptID: "legacy-attempt", ProfileID: "coddy-legacy", Status: "limit_reached", Partial: "UNCLASSIFIED_OLD_STREAM"}
	body, _ := json.Marshal(result)
	if err := os.WriteFile(filepath.Join(dir, "result.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	parent := Attempt{ID: "legacy-attempt", Profile: "coddy-legacy", ResultSHA256: hash(result)}
	context := s.continuationContext(Data{Attempts: []Attempt{parent}}, Attempt{ParentAttempt: parent.ID})
	object, ok := context.(map[string]any)
	if !ok || object["content_omitted"] != "legacy_uncategorized_coddy_stream" || object["partial"] != nil || object["actions"] != nil {
		t.Fatal("old unclassified stream was reused")
	}
}
