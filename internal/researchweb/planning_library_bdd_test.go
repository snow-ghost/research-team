//go:build linux

package researchweb

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
)

func planningLibraryTarget(t *testing.T, s *Service, p ResultPackage) (string, string, string, leancheck.Goal) {
	t.Helper()
	v := act(t, s, Action{Type: "CREATE_STUDY", Title: "Planning library target", Statement: "True", Assumptions: "Prop"})
	target := v.Studies[len(v.Studies)-1].Goal
	goal := leancheck.Goal{Source: "def PlanningGoal : Prop := True", Declaration: "PlanningGoal", Candidate: "PlanningCandidate"}
	if err := s.SetFormalGoal(target, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: goal}); err != nil {
		t.Fatal(err)
	}
	library := identifier("library")
	module := "ResearchLemma_" + strings.TrimPrefix(library, "library-")
	if err := s.Store.Change(0, "", "", "Planning library fixture", target, "test", func(d *Data) error {
		e := d.entity(p.Target.ID)
		d.Library = append(d.Library, LibraryEntry{ID: library, Lemma: e.ID, LemmaRevision: e.Revision, Result: e.ResearchResult, Status: "ready", Goal: *e.FormalGoal, Module: module, Report: &leancheck.ModuleReport{Status: "ready", AuditSHA256: leancheck.AuditDigest(), ArtifactSHA256: "fixture-pin", Artifacts: map[string]string{"module.olean": "fixture"}}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	act(t, s, Action{Type: "PAUSE"})
	return target, library, module, goal
}

func TestPlanningLibraryBDD_VerifiedLemmaSkipsModelAndCreatesApplication(t *testing.T) {
	s, p := packageFixture(t)
	s.Options.Lean = &leancheck.Config{}
	target, _, module, _ := planningLibraryTarget(t, s, p)
	calls := 0
	s.Options.Matcher = moduleMatcherFunc(func(_ context.Context, g leancheck.Goal, _ string, m leancheck.ModuleReport, _, _ string) (leancheck.MatchReport, error) {
		calls++
		source := "import Goal\ntheorem PlanningCandidate : PlanningGoal := by trivial\n"
		verification := &leancheck.Report{Status: "verified", AuditSHA256: leancheck.AuditDigest(), GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(source)}
		return leancheck.MatchReport{Status: "verified", GoalSHA256: leancheck.Digest(g), ArtifactSHA256: m.ArtifactSHA256, MatcherSHA256: leancheck.MatcherDigest(), Bindings: []leancheck.Argument{}, Missing: []leancheck.MissingArgument{}, Source: source, Verification: verification}, nil
	})
	before := len(stateOf(t, s).Attempts)
	if err := s.Store.Change(0, "", "", "Workflow planning fixture", target, "test", func(d *Data) error {
		d.study(d.entity(target).Study).Workflow = &ResearchWorkflow{ID: "workflow-library", Target: target, ScopeSHA256: memoryScope(*d.entity(target)), Planner: "proof", PlannerSHA256: hash(s.profile("proof")), Team: TeamRequest{Workspace: "source"}, Status: "prepared"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var v View
	deadline := time.Now().Add(5 * time.Second)
	for {
		v = stateOf(t, s)
		if workflow := v.study(v.entity(target).Study).Workflow; workflow != nil && workflow.Status == "needs_operator" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("full workflow did not select the verified library lemma")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(v.Attempts) != before || len(v.Applications) != 1 || len(v.LemmaMatches) != 1 || v.LemmaMatches[0].Application == "" || calls != 1 {
		t.Fatalf("verified lemma did not replace model planning: %+v %+v workflow=%+v calls=%d", v.Applications, v.Attempts, v.study(v.entity(target).Study).Workflow, calls)
	}
	application := v.entity(v.Applications[0].ID)
	if application == nil || application.Status != "open" || application.FormalGoal == nil || !strings.Contains(application.FormalGoal.Source, "import "+module) || v.entity(target).Status != "open" {
		t.Fatal("invalid library application", application)
	}
	if len(v.Verifications) == 0 {
		t.Fatal("verified match did not create an independent application check")
	}
	request := PlanningRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Target: target, Profile: "proof", Workspace: "source", Confirm: true}
	if err := s.RequestPlanningWithContext(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if len(stateOf(t, s).Applications) != 1 || calls != 1 {
		t.Fatal("repeated request duplicated application or native check")
	}
}

func TestPlanningLibraryBDD_ObligationsArePricedAndPassedToPlanner(t *testing.T) {
	s, p := packageFixture(t)
	s.Options.Lean = &leancheck.Config{}
	target, _, _, _ := planningLibraryTarget(t, s, p)
	s.Options.Matcher = moduleMatcherFunc(func(_ context.Context, g leancheck.Goal, _ string, m leancheck.ModuleReport, _, _ string) (leancheck.MatchReport, error) {
		return leancheck.MatchReport{Status: "obligations", GoalSHA256: leancheck.Digest(g), ArtifactSHA256: m.ArtifactSHA256, MatcherSHA256: leancheck.MatcherDigest(), Bindings: []leancheck.Argument{}, Missing: []leancheck.MissingArgument{{Name: "h1", Kind: "premise", Type: "True", ClosedType: "True"}, {Name: "h2", Kind: "premise", Type: "True", ClosedType: "True"}}}, nil
	})
	report := DecompositionReport{Summary: "Reuse after proving the missing premise", Claims: []ProposedClaim{{Title: "Premise", Statement: "True", Assumptions: "None", FormalGoal: &leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}}}, Coverage: ProposedClaim{Title: "Coverage", Statement: "True", Assumptions: "Premise", FormalGoal: &leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}}, Strategies: []ProposedStrategy{{Target: -1, Method: "decomposition", Priority: 80, Rationale: "Reuse has one remaining premise"}}}
	encoded, _ := json.Marshal(report)
	var modelInput string
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		modelInput = string(body)
		completeModel(w, string(encoded))
	}))
	defer model.Close()
	profile := s.Options.Profiles["proof"]
	profile.Model.BaseURL = model.URL + "/v1"
	s.Options.Profiles["proof"] = profile
	if err := s.RequestPlanningWithContext(context.Background(), PlanningRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Target: target, Profile: "proof", Workspace: "source", Confirm: true}); err != nil {
		v := stateOf(t, s)
		t.Fatal(err, v.Paused, v.entity(target))
	}
	v := stateOf(t, s)
	a := waitAttempt(t, s, v.Attempts[len(v.Attempts)-1].ID, func(a Attempt) bool { return !active(a.Status) })
	if a.Status != "candidate" || a.LibrarySearch == nil || a.LibrarySearch.NativeChecks != 1 || len(v.Applications) != 0 {
		t.Fatalf("obligations were applied instead of planned: %+v", a)
	}
	material := s.planningLibraryMaterial(v.Data, a)
	if material == nil || len(material.Matches) != 1 || material.Matches[0].EstimatedRemainingChecks != 3 || material.Matches[0].CostComparison != "more_expensive" || len(material.Matches[0].Missing) != 2 || material.Matches[0].Missing[0].Name != "h1" || material.Accepted {
		t.Fatalf("missing reuse cost or premise: %+v", material)
	}
	if !strings.Contains(modelInput, "library_search") {
		t.Fatal("planner did not receive bounded library assessment", modelInput)
	}
	library := v.Data.Library[0]
	v.Data.LemmaSignatures = append(v.Data.LemmaSignatures, LemmaSignature{Library: library.ID, GoalSHA256: leancheck.Digest(library.Goal), Native: &leancheck.SignatureReport{Status: "verified", Binders: []leancheck.Binder{{Name: "hn", Type: "10 ≤ n"}}}})
	proofMaterial := s.planningLibraryMaterial(v.Data, Attempt{Target: target, TargetRevision: a.TargetRevision, Role: "proof"})
	if proofMaterial == nil || len(proofMaterial.Matches) != 1 {
		t.Fatal("proof executor did not inherit the current library search")
	}
	match := proofMaterial.Matches[0]
	if match.Theorem != library.Goal.Candidate || match.Declaration != library.Goal.Declaration || match.Module != library.Module || match.LibraryGoalSHA256 != leancheck.Digest(library.Goal) || len(match.Binders) != 1 || match.Binders[0].Name != "hn" {
		t.Fatalf("proof executor received incomplete lemma metadata: %+v", match)
	}
	if stale := s.planningLibraryMaterial(v.Data, Attempt{Target: target, TargetRevision: a.TargetRevision + 1, Role: "proof"}); stale != nil {
		t.Fatal("stale library search propagated to a revised target")
	}
	if v.entity(target).Status != "open" {
		t.Fatal("library estimate accepted the target")
	}
}

func TestPlanningLibraryBDD_SinglePremiseReuseSkipsModelAndCreatesObligation(t *testing.T) {
	s, p := packageFixture(t)
	s.Options.Lean = &leancheck.Config{}
	target, _, _, _ := planningLibraryTarget(t, s, p)
	s.Options.Matcher = moduleMatcherFunc(func(_ context.Context, g leancheck.Goal, _ string, m leancheck.ModuleReport, _, _ string) (leancheck.MatchReport, error) {
		return leancheck.MatchReport{Status: "obligations", GoalSHA256: leancheck.Digest(g), ArtifactSHA256: m.ArtifactSHA256, MatcherSHA256: leancheck.MatcherDigest(), Bindings: []leancheck.Argument{}, Missing: []leancheck.MissingArgument{{Name: "h", Kind: "premise", Type: "True", ClosedType: "True"}}}, nil
	})
	before := len(stateOf(t, s).Attempts)
	if err := s.RequestPlanningWithContext(context.Background(), PlanningRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Target: target, Profile: "proof", Workspace: "source", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	v := stateOf(t, s)
	if len(v.Attempts) != before || len(v.Applications) != 1 || len(v.LemmaMatches) != 1 || v.LemmaMatches[0].Application == "" {
		t.Fatal("single-premise reuse created a model attempt instead of an application")
	}
	application := v.entity(v.Applications[0].ID)
	if application == nil || len(application.Dependencies) != 2 || application.Status != "open" {
		t.Fatal("invalid single-premise application", application)
	}
	obligation := v.entity(application.Dependencies[1])
	if obligation == nil || obligation.Kind != "obligation" || obligation.Status != "open" || obligation.FormalGoal == nil {
		t.Fatal("missing premise was hidden", obligation)
	}
	if v.entity(target).Status != "open" {
		t.Fatal("single-premise reuse accepted the target")
	}
}

func TestPlanningLibraryBDD_CalibratedEstimateCanSelectThreeCheckReuse(t *testing.T) {
	s, p := packageFixture(t)
	s.Options.Lean = &leancheck.Config{}
	target, _, _, _ := planningLibraryTarget(t, s, p)
	goal := leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}
	id := identifier("run")
	result := execution.Result{AttemptID: id, Status: "candidate", Candidate: submittedProof}
	for step := 0; step < 8; step++ {
		result.Events = append(result.Events, execution.Event{Type: "model_requested", Step: step + 1})
	}
	if err := s.Store.Change(0, "", "", "Verified expensive proof fixture", target, "test", func(d *Data) error {
		d.Attempts = append(d.Attempts, Attempt{ID: id, Target: target, Role: "proof", Status: "candidate", ResultSHA256: hash(result)})
		d.Verifications = append(d.Verifications, Verification{ID: identifier("verify"), Target: target, Attempt: id, Status: "verified", Goal: goal, Source: submittedProof, Report: &leancheck.Report{Status: "verified", GoalSHA256: leancheck.Digest(goal), SourceSHA256: leancheck.Digest(submittedProof)}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(result)
	dir := filepath.Join(s.Options.Config.DataDir, "attempts", id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := atomicFile(filepath.Join(dir, "result.json"), body); err != nil {
		t.Fatal(err)
	}
	s.Options.Matcher = moduleMatcherFunc(func(_ context.Context, g leancheck.Goal, _ string, m leancheck.ModuleReport, _, _ string) (leancheck.MatchReport, error) {
		return leancheck.MatchReport{Status: "obligations", GoalSHA256: leancheck.Digest(g), ArtifactSHA256: m.ArtifactSHA256, MatcherSHA256: leancheck.MatcherDigest(), Missing: []leancheck.MissingArgument{{Name: "h1", Kind: "premise", Type: "True", ClosedType: "True"}, {Name: "h2", Kind: "premise", Type: "True", ClosedType: "True"}}}, nil
	})
	before := len(stateOf(t, s).Attempts)
	if err := s.RequestPlanningWithContext(context.Background(), PlanningRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Target: target, Profile: "proof", Workspace: "source", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	v := stateOf(t, s)
	if len(v.Attempts) != before || len(v.Applications) != 1 || v.LemmaMatches[0].Application == "" {
		t.Fatal("three-check reuse was not preferred to the calibrated four-check proof")
	}
}

func TestPlanningLibraryBDD_HistoryCalibratesNewProofEstimate(t *testing.T) {
	s := serviceFor(t, optionsFor(t, "http://127.0.0.1:1/v1"))
	v := studyFor(t, s)
	target := v.Studies[0].Goal
	goal := leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}
	firstAttempt := ""
	if err := s.Store.Change(0, "", "", "Cost history fixture", target, "test", func(d *Data) error {
		addAttempt := func(calls int, status, parent string, verified bool) (string, error) {
			id := identifier("run")
			result := execution.Result{AttemptID: id, Status: status, Candidate: submittedProof}
			for step := 0; step < calls; step++ {
				result.Events = append(result.Events, execution.Event{Type: "model_requested", Step: step + 1}, execution.Event{Type: "usage", Step: step + 1})
			}
			d.Attempts = append(d.Attempts, Attempt{ID: id, Target: target, Role: "proof", Profile: "reader", Status: status, ParentAttempt: parent, ResultSHA256: hash(result)})
			if verified {
				d.Verifications = append(d.Verifications, Verification{ID: identifier("verify"), Target: target, Attempt: id, Status: "verified", Goal: goal, Source: submittedProof, Report: &leancheck.Report{Status: "verified", GoalSHA256: leancheck.Digest(goal), SourceSHA256: leancheck.Digest(submittedProof)}})
			}
			body, _ := json.Marshal(result)
			dir := filepath.Join(s.Options.Config.DataDir, "attempts", id)
			if err := os.MkdirAll(dir, 0700); err != nil {
				return "", err
			}
			return id, atomicFile(filepath.Join(dir, "result.json"), body)
		}
		for _, calls := range []int{1, 3, 8, 10, 12} {
			id, err := addAttempt(calls, "candidate", "", true)
			if err != nil {
				return err
			}
			if firstAttempt == "" {
				firstAttempt = id
			}
		}
		if _, err := addAttempt(1, "candidate", "", false); err != nil {
			return err
		}
		if _, err := addAttempt(1, "limit_reached", "", false); err != nil {
			return err
		}
		_, err := addAttempt(2, "candidate", "parent", true)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		id, status, limit string
	}{{"computation-valid", "completed", ""}, {"computation-limited", "limit_reached", "max_states"}} {
		dir := filepath.Join(s.Options.Config.DataDir, "attempts", firstAttempt, "computations", fixture.id)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		record := computationRecord{ID: fixture.id, Attempt: firstAttempt, Tool: "run_bfs", Request: json.RawMessage(`{}`), Report: map[string]any{"status": fixture.status}}
		body, _ := json.Marshal(record)
		summary := computationSummary{ID: fixture.id, Attempt: firstAttempt, Tool: "run_bfs", Status: fixture.status, LimitReason: fixture.limit, CertificateSHA256: "certificate", ReportSHA256: bytesDigest(body)}
		summaryBody, _ := json.Marshal(summary)
		if err := atomicFile(filepath.Join(dir, "report.json"), body); err != nil {
			t.Fatal(err)
		}
		if err := atomicFile(filepath.Join(dir, "summary.json"), summaryBody); err != nil {
			t.Fatal(err)
		}
	}
	material := s.planningLibraryMaterial(stateOf(t, s).Data, Attempt{Target: target, LibrarySearch: &PlanningLibrarySearch{}})
	if material == nil || material.NewProofEstimatedChecks != 4 || material.HistorySamples != 5 || material.Costs.NewProof.ExcludedAttempts != 2 {
		t.Fatalf("history did not calibrate verified estimate: %+v", material)
	}
	if material.Costs.FormatRepair.EstimatedChecks != 2 || material.Costs.FormatRepair.HistorySamples != 1 || material.Costs.LemmaApplication.EstimatedChecks != 1 || material.Costs.BFS.EstimatedChecks != 1 || material.Costs.BFS.HistorySamples != 1 {
		t.Fatalf("strategy histories were mixed or invalid artifacts included: %+v", material.Costs)
	}
	material = s.planningLibraryMaterial(stateOf(t, s).Data, Attempt{Target: "other-target", LibrarySearch: &PlanningLibrarySearch{}})
	if material == nil || material.NewProofEstimatedChecks != 2 || material.HistorySamples != 0 || material.Costs.FormatRepair.EstimatedChecks != 1 || material.Costs.BFS.HistorySamples != 0 {
		t.Fatalf("missing-history fallback changed: %+v", material)
	}
}

func TestPlanningLibraryBDD_ACPProvidersSupplyValidatedCallCounts(t *testing.T) {
	s := serviceFor(t, optionsFor(t, "http://127.0.0.1:1/v1"))
	goal := leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}
	for _, fixture := range []struct {
		target, provider string
		want             int
	}{{"kimi-target", "kimi-agent", 1}, {"coddy-target", "coddy-agent", 4}} {
		id := identifier("run")
		result := execution.Result{AttemptID: id, Status: "candidate", Candidate: submittedProof}
		if fixture.provider == "kimi-agent" {
			result.Events = append(result.Events, execution.Event{Type: "acp_prompt_started"})
		} else {
			result.ContentPolicy = execution.CoddyContentPolicy
			for step := 0; step < fixture.want; step++ {
				usage := &execution.Usage{InputTokens: int64(step + 1), OutputTokens: 1, Source: "test"}
				result.Events = append(result.Events, execution.Event{Type: "usage", Step: step + 1, Usage: usage})
				result.Usage = usage
			}
		}
		if err := s.Store.Change(0, "", "", "ACP cost fixture", fixture.target, "test", func(d *Data) error {
			d.Attempts = append(d.Attempts, Attempt{ID: id, Target: fixture.target, Role: "formalize", Status: "candidate", ProfileConfiguration: &execution.Profile{External: &execution.ExternalConfig{Provider: fixture.provider}}, ResultSHA256: hash(result)})
			d.Verifications = append(d.Verifications, Verification{ID: identifier("verify"), Target: fixture.target, Attempt: id, Status: "verified", Goal: goal, Source: submittedProof, Report: &leancheck.Report{Status: "verified", GoalSHA256: leancheck.Digest(goal), SourceSHA256: leancheck.Digest(submittedProof)}})
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(result)
		dir := filepath.Join(s.Options.Config.DataDir, "attempts", id)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := atomicFile(filepath.Join(dir, "result.json"), body); err != nil {
			t.Fatal(err)
		}
		v := stateOf(t, s)
		cost, samples := s.newProofEstimatedChecks(&v.Data, fixture.target)
		if cost != fixture.want || samples != 1 {
			t.Fatalf("ACP calls were not measured: provider=%s cost=%d samples=%d", fixture.provider, cost, samples)
		}
	}
}

func TestPlanningLibraryBDD_PauseBlocksNativeSearchAndApplication(t *testing.T) {
	s, p := packageFixture(t)
	s.Options.Lean = &leancheck.Config{}
	target, _, _, _ := planningLibraryTarget(t, s, p)
	act(t, s, Action{Type: "PAUSE"})
	calls := 0
	s.Options.Matcher = moduleMatcherFunc(func(_ context.Context, g leancheck.Goal, _ string, m leancheck.ModuleReport, _, _ string) (leancheck.MatchReport, error) {
		calls++
		return leancheck.MatchReport{Status: "no_match", GoalSHA256: leancheck.Digest(g), ArtifactSHA256: m.ArtifactSHA256, MatcherSHA256: leancheck.MatcherDigest()}, nil
	})
	err := s.RequestPlanningWithContext(context.Background(), PlanningRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Target: target, Profile: "proof", Workspace: "source", Confirm: true})
	if err == nil || calls != 0 || len(stateOf(t, s).Applications) != 0 {
		t.Fatal("pause allowed native search or library application", err, calls)
	}
}
