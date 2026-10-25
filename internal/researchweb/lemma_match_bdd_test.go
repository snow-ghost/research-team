//go:build linux

package researchweb

import (
	"strings"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestLemmaMatchBDD_MissingPremiseBecomesPinnedObligationNotAcceptedFact(t *testing.T) {
	for _, scenario := range []string{"premise", "parameter", "stale", "unverified_term"} {
		t.Run(scenario, func(t *testing.T) {
			s, p := packageFixture(t)
			source := p.Target.ID
			v := act(t, s, Action{Type: "CREATE_STUDY", Title: "Application target", Statement: "True", Assumptions: "None"})
			target := v.Studies[len(v.Studies)-1].Goal
			goal := leancheck.Goal{Source: "def MatchTarget : Prop := True", Declaration: "MatchTarget", Candidate: "matchTargetProof"}
			if err := s.SetFormalGoal(target, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: goal}); err != nil {
				t.Fatal(err)
			}
			id := identifier("match")
			lib := identifier("library")
			module := "ResearchLemma_" + strings.TrimPrefix(lib, "library-")
			if err := s.Store.Change(0, "", "", "Native match fixture", target, "test", func(d *Data) error {
				e := d.entity(source)
				d.Library = append(d.Library, LibraryEntry{ID: lib, Lemma: source, LemmaRevision: e.Revision, Result: e.ResearchResult, Goal: *e.FormalGoal, Module: module, Status: "ready", Report: &leancheck.ModuleReport{Status: "ready", ArtifactSHA256: "fixture-pin", Artifacts: map[string]string{"module.olean": "fixture"}}})
				g := goal
				g.Source = "import " + module + "\n" + g.Source
				report := leancheck.MatchReport{Status: "obligations", GoalSHA256: leancheck.Digest(g), ArtifactSHA256: "fixture-pin", MatcherSHA256: leancheck.MatcherDigest(), Bindings: []leancheck.Argument{}, Missing: []leancheck.MissingArgument{{Name: "h", Kind: "premise", Type: "True", ClosedType: "True"}}}
				if scenario == "parameter" {
					report.Missing[0].Kind = "parameter"
				}
				if scenario == "unverified_term" {
					report.Status = "verified"
					report.Source = "import Goal\ntheorem matchTargetProof : MatchTarget := by trivial\n"
				}
				d.LemmaMatches = append(d.LemmaMatches, NativeLemmaMatch{ID: id, Target: target, TargetRevision: d.entity(target).Revision, TargetGoalSHA256: leancheck.Digest(goal), Library: lib, Goal: g, Report: report})
				if scenario == "stale" {
					d.entity(target).Revision++
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			v = stateOf(t, s)
			if scenario == "premise" {
				if err := s.AddMemory(MemoryRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Target: target, Kind: "unmet_assumption", Match: id, Content: "This premise is still open; no claim that it is false."}); err != nil {
					t.Fatal(err)
				}
				v = stateOf(t, s)
				hits := memoryForTarget(v.Data, target)
				if len(hits) != 1 || hits[0].Trust != "recorded_obligation" || !hits[0].AutoReuse || len(hits[0].Obligations) != 1 || v.entity(target).Status != "open" {
					t.Fatal("open premise became a fact or was not recorded", hits)
				}
			}
			r := ApplyMatchRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Confirm: true}
			err := s.ApplyLemmaMatch(id, r)
			if scenario != "premise" {
				if err == nil {
					t.Fatal("invalid match created obligations")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := s.ApplyLemmaMatch(id, r); err != nil {
				t.Fatal("idempotency", err)
			}
			v = stateOf(t, s)
			app := v.entity(v.Applications[0].ID)
			if app.Status != "open" || len(app.Dependencies) != 2 || app.FormalGoal.Declaration != goal.Declaration || v.entity(target).Status != "open" || len(v.LemmaMatches) != 1 || v.LemmaMatches[0].Application != app.ID {
				t.Fatal("application weakened or accepted exact target", app)
			}
			condition := v.entity(app.Dependencies[1])
			if condition.Kind != "obligation" || condition.Status != "open" || condition.FormalGoal == nil || !strings.Contains(condition.FormalGoal.Source, " : Prop := True") {
				t.Fatal("missing premise omitted", condition)
			}
		})
	}
}

func TestResultSnapshotBDD_ReadOnlyExportKeepsWriterOwnerAndRevision(t *testing.T) {
	s, p := packageFixture(t)
	before := stateOf(t, s).Revision
	v, err := ReadSQLiteResultSnapshot(s.Options.Config.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	q, err := ExportResultPackageSnapshot(v, s.Options.Config.DataDir, p.Result.ID)
	if err != nil {
		t.Fatal(err)
	}
	if q.Result.ID != p.Result.ID || q.Result.Binding != p.Result.Binding || stateOf(t, s).Revision != before {
		t.Fatal("read-only exporter changed the source")
	}
}
