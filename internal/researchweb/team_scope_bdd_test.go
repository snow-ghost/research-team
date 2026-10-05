//go:build linux

package researchweb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestTeamScopeBDD_SeparatePartUsesItsGoalAndBudget(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		text := ""
		for _, m := range body.Messages {
			text += m.Content
		}
		candidate := "Proof of the selected part."
		if strings.Contains(text, "ROLE_counterexample") {
			candidate = `{"outcome":"none_found","evidence":"Checked the selected statement."}`
		}
		if strings.Contains(text, "ROLE_formalize") {
			candidate = "```lean\nimport Goal\ntheorem Child.candidate : Child.Statement := by trivial\n```"
		}
		if strings.Contains(text, "ROLE_review") {
			candidate = `{"summary":"Reviewed the selected part","findings":[]}`
		}
		completeModel(w, candidate)
	}))
	defer model.Close()
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			o := teamOptions(t, model.URL+"/v1")
			o.Checker = checkerFunc(func(_ context.Context, g leancheck.Goal, source, _ string) (leancheck.Report, error) {
				return leancheck.Report{Status: "verified", Phase: "complete", GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(source)}, nil
			})
			if backend == "postgres" {
				dsn := postgresDSN(t)
				o.Config.Database = DatabaseConfig{Driver: "postgres", DSNEnv: "TEST_DB"}
				lookup := o.Lookup
				o.Lookup = func(key string) (string, bool) {
					if key == "TEST_DB" {
						return dsn, true
					}
					return lookup(key)
				}
			}
			s := serviceFor(t, o)
			v := studyFor(t, s)
			study, root := v.Studies[0].ID, v.Studies[0].Goal
			v = act(t, s, Action{Type: "SPLIT", Target: root, Parts: []string{"First part", "Second part"}})
			child := v.entity(root).Dependencies[0]
			if err := s.SetFormalGoal(child, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: leancheck.Goal{Source: "namespace Child\ndef Statement : Prop := True\nend Child", Declaration: "Child.Statement", Candidate: "Child.candidate"}}); err != nil {
				t.Fatal(err)
			}
			v = act(t, s, Action{Type: "CREATE_STUDY", Title: "Other study", Statement: "Other statement", Assumptions: "Other assumptions"})
			r := TeamRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: study, Target: v.Studies[1].Goal,
				Profiles: map[string]string{"proof": "proof", "counterexample": "counterexample", "formalize": "formalize", "review": "review"}, Workspace: "source", MaxAttempts: 6, RequireLean: true, Confirm: true}
			if err := s.StartTeam(r); err == nil {
				t.Fatal("foreign study target accepted")
			}
			r.Target, r.RequestID = "missing", identifier("cmd")
			if err := s.StartTeam(r); err == nil {
				t.Fatal("missing target accepted")
			}
			r.Target, r.RequestID = child, identifier("cmd")
			if err := s.StartTeam(r); err != nil {
				t.Fatal(err)
			}
			if err := s.StartTeam(r); err != nil {
				t.Fatal("idempotency", err)
			}
			id := stateOf(t, s).Teams[0].ID
			v = waitTeam(t, s, id, "awaiting_review")
			if v.team(id).Goal != child || v.team(id).UsedAttempts != 4 || v.entity(root).Proof != "" || v.entity(root).Status != "open" {
				t.Fatal("part scope or budget was lost")
			}
			for _, a := range v.Attempts {
				if a.Target != child {
					t.Fatal("attempt escaped selected part")
				}
			}
			act(t, s, Action{Type: "REVIEW", Target: child, Decision: "accept", Text: "Проверены отдельная часть и ее рецензия."})
			v = waitTeam(t, s, id, "completed")
			if len(v.Attempts) != 4 || v.entity(root).Status != "open" {
				t.Fatal("accepted part started parent work")
			}
		})
	}
}

func TestTeamScopeBDD_LemmaContextContainsOnlyReachableAcceptedDependencies(t *testing.T) {
	d := Data{Entities: []Entity{
		{ID: "root", Kind: "goal", Dependencies: []string{"used", "pending"}},
		{ID: "used", Kind: "lemma", Revision: 1, Status: "accepted", Proof: "usable"},
		{ID: "pending", Kind: "lemma", Revision: 1, Status: "open"},
		{ID: "unrelated", Kind: "lemma", Revision: 1, Status: "accepted", Proof: "private unrelated proof"},
	}}
	got := acceptedLemmas(d, "root")
	if len(got) != 1 || got[0].ID != "used" {
		t.Fatalf("unexpected lemma context: %+v", got)
	}
	if len(acceptedLemmas(d, "missing")) != 0 {
		t.Fatal("missing target exposed the library")
	}
}
