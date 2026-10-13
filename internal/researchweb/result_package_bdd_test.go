//go:build linux

package researchweb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

func packageFixture(t *testing.T) (*Service, ResultPackage) {
	t.Helper()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body any
		_ = json.NewDecoder(r.Body).Decode(&body)
		data, _ := json.Marshal(body)
		answer := "```lean\n" + submittedProof + "```"
		if strings.Contains(string(data), "ROLE_counterexample") {
			answer = `{"outcome":"none_found","evidence":"Boundary cases; no claim of proof by enumeration."}`
		}
		if strings.Contains(string(data), "ROLE_review") {
			answer = `{"rubric":"proof-review@2","summary":"Exact source independently reviewed","findings":[]}`
		}
		completeModel(w, answer)
	}))
	t.Cleanup(provider.Close)
	o := teamOptions(t, provider.URL+"/v1")
	o.Checker = checkerFunc(func(_ context.Context, g leancheck.Goal, source, _ string) (leancheck.Report, error) {
		return leancheck.Report{Status: "verified", AuditSHA256: leancheck.AuditDigest(), GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(source)}, nil
	})
	s := serviceFor(t, o)
	v := studyFor(t, s)
	study, goal := v.Studies[0].ID, v.Studies[0].Goal
	act(t, s, Action{Type: "CREATE_STUDY", Title: "UNRELATED-PRIVATE-STUDY", Statement: "Do not export", Assumptions: "Private"})
	v = stateOf(t, s)
	if err := s.SetFormalGoal(goal, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	if err := s.StartTeam(TeamRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: study, Strategy: "demand", Profiles: map[string]string{"proof": "proof", "counterexample": "counterexample", "formalize": "formalize", "review": "review"}, Workspace: "source", MaxAttempts: 6, RequireLean: true, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	v = waitTeam(t, s, stateOf(t, s).Teams[0].ID, "awaiting_review")
	act(t, s, Action{Type: "PAUSE"})
	act(t, s, Action{Type: "REVIEW", Target: goal, Decision: "accept", Text: "Fixture acceptance after exact review"})
	v = waitTeam(t, s, v.Teams[0].ID, "completed")
	p, err := s.ExportResultPackage(v.Results[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	return s, p
}

func packageDestination(t *testing.T, status string, backend ...string) *Service {
	t.Helper()
	o := optionsFor(t, "http://127.0.0.1:1/v1")
	if len(backend) > 0 && backend[0] == "postgres" {
		dsn := postgresDSN(t)
		o.Config.Database = DatabaseConfig{Driver: "postgres", DSNEnv: "TEST_PACKAGE_DB"}
		lookup := o.Lookup
		o.Lookup = func(key string) (string, bool) {
			if key == "TEST_PACKAGE_DB" {
				return dsn, true
			}
			return lookup(key)
		}
	}
	o.Checker = checkerFunc(func(_ context.Context, g leancheck.Goal, source, _ string) (leancheck.Report, error) {
		return leancheck.Report{Status: status, AuditSHA256: leancheck.AuditDigest(), GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(source)}, nil
	})
	s := serviceFor(t, o)
	act(t, s, Action{Type: "PAUSE"})
	return s
}

func TestResultPackagePostgresBDD_ReferencesAndHistoryCommitAtomically(t *testing.T) {
	_, p := packageFixture(t)
	s := packageDestination(t, "verified", "postgres")
	r := ImportResultPackageRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Package: p, Confirm: true}
	if err := s.ImportResultPackage(r); err != nil {
		t.Fatal(err)
	}
	v := waitVerification(t, s, p.Verification.ID)
	if len(v.Results) != 1 || v.Results[0].Status != "ready" || len(v.Attempts) != 3 {
		t.Fatal("Postgres lost transfer references", v.Results)
	}
	snapshot, err := s.Store.Snapshot(v.Revision)
	if err != nil || snapshot.Results[0].Transfer.PackageSHA256 != p.SHA256 {
		t.Fatal("transfer was not preserved in history", err)
	}
}

func TestResultPackageBDD_ScopedTransferRechecksNativeKeepsAuthorshipAndDoesNotDoubleCharge(t *testing.T) {
	_, p := packageFixture(t)
	body, _ := json.Marshal(p)
	if strings.Contains(string(body), "UNRELATED-PRIVATE-STUDY") || strings.Contains(string(body), "model-test-secret") {
		t.Fatal("package leaked unrelated inputs or credentials")
	}
	s := packageDestination(t, "verified")
	r := ImportResultPackageRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Package: p, Confirm: true}
	if err := s.ImportResultPackage(r); err != nil {
		t.Fatal(err)
	}
	if err := s.ImportResultPackage(r); err != nil {
		t.Fatal("import idempotency", err)
	}
	v := waitVerification(t, s, p.Verification.ID)
	e := v.entity(p.Target.ID)
	if e.Status != "in_review" || e.ProofAuthor != p.Target.ProofAuthor || e.ProofAttempt != p.Target.ProofAttempt || len(v.Results) != 1 || v.Results[0].Status != "ready" || v.Results[0].Transfer == nil || v.Results[0].Transfer.SourceStatus != "accepted" {
		t.Fatal("provenance or acceptance boundary lost", e, v.Results)
	}
	for _, a := range v.Attempts {
		if a.ImportedPackageSHA256 != p.SHA256 || a.TeamID != "" || a.RemoteWorker != "" {
			t.Fatal("import recreated an active assignment")
		}
		if _, err := s.readInput(a); err != nil {
			t.Fatal(err)
		}
		if _, err := s.readResult(a); err != nil {
			t.Fatal(err)
		}
	}
	o := s.observeBudget(&v.Data, e.Study)
	if o.ChargedModelRequests != 0 || o.Attempts != 0 || o.ImportedAttempts != 3 || o.ImportedModelRequests != 3 {
		t.Fatal("import charged provider calls twice", o)
	}
	act(t, s, Action{Type: "REVIEW", Target: e.ID, Decision: "accept", Text: "Local native check and original exact independent review checked"})
	v = stateOf(t, s)
	if v.entity(e.ID).Status != "accepted" {
		t.Fatal("explicit acceptance failed")
	}
}

func TestResultPackageBDD_MutationsAndFailedNativeCheckCannotBecomeAccepted(t *testing.T) {
	_, p := packageFixture(t)
	for _, failure := range []string{"digest", "review_binding", "missing_attempt", "author", "foreign_target", "module_prefix", "input_scope"} {
		t.Run(failure, func(t *testing.T) {
			raw, _ := json.Marshal(p)
			var bad ResultPackage
			_ = json.Unmarshal(raw, &bad)
			switch failure {
			case "digest":
				bad.SHA256 = "wrong"
			case "review_binding":
				for i := range bad.Attempts {
					if bad.Attempts[i].Attempt.ID == bad.Result.ReviewAttempt {
						bad.Attempts[i].Attempt.ProofBinding.SourceSHA256 = "wrong"
					}
				}
			case "missing_attempt":
				bad.Attempts = bad.Attempts[:1]
			case "author":
				bad.Target.ProofAuthor = "executor:review"
			case "foreign_target":
				bad.Verification.Target = identifier("H")
			case "module_prefix":
				bad.Module = &TransferredModule{ID: "not-library-id", Module: "invalid"}
			case "input_scope":
				bad.Attempts[0].Input = []byte(`{"research":{"unknown":"payload"}}`)
				bad.Attempts[0].InputSHA256 = bytesDigest(bad.Attempts[0].Input)
			}
			if failure != "digest" {
				bad.SHA256 = packageDigest(bad)
			}
			s := packageDestination(t, "verified")
			if err := s.ImportResultPackage(ImportResultPackageRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Package: bad, Confirm: true}); err == nil {
				t.Fatal("mutated package imported")
			}
			if len(stateOf(t, s).Attempts) != 0 {
				t.Fatal("rejection created attempts")
			}
		})
	}
	s := packageDestination(t, "failed")
	if err := s.ImportResultPackage(ImportResultPackageRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Package: p, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	v := waitVerification(t, s, p.Verification.ID)
	if err := s.Act(Action{Type: "REVIEW", Target: p.Target.ID, Decision: "accept", Text: "Cannot override kernel", ExpectedRevision: v.Revision, RequestID: identifier("cmd")}); err == nil {
		t.Fatal("imported historical acceptance bypassed failed native check")
	}
}

func TestResultPackageBDD_FailedTransferredModuleCannotReuseIdentity(t *testing.T) {
	s, p := packageFixture(t)
	s.Options.Lean = &leancheck.Config{}
	if err := s.Store.Change(0, "", "", "Failed transferred module fixture", p.Target.ID, "test", func(d *Data) error {
		e := d.entity(p.Target.ID)
		v := proofVerification(d, e)
		source, err := leancheck.ModuleSource(v.Goal, v.Source)
		if err != nil {
			return err
		}
		id := identifier("library")
		module := "ResearchLemma_" + strings.TrimPrefix(id, "library-")
		for i := range d.Results {
			if d.Results[i].ID == e.ResearchResult {
				d.Results[i].Transfer = &ResultTransfer{Module: &TransferredModule{ID: id, Module: module, SourceSHA256: leancheck.Digest(source)}}
			}
		}
		d.Library = append(d.Library, LibraryEntry{ID: id, Lemma: e.ID, LemmaRevision: e.Revision, Module: module, Status: "failed"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	v := stateOf(t, s)
	err := s.PublishLemma(PublishRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Lemma: p.Target.ID})
	if err == nil || !strings.Contains(err.Error(), "идентификатором") || stateOf(t, s).Revision != v.Revision || len(stateOf(t, s).Library) != 1 {
		t.Fatal("failed transferred module identity was reused", err)
	}
}

func TestResultPackageBDD_FilesystemFailureLeavesNoPartialState(t *testing.T) {
	_, p := packageFixture(t)
	s := packageDestination(t, "verified")
	path := filepath.Join(s.Options.Config.DataDir, "attempts", p.Attempts[1].Attempt.ID)
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.ImportResultPackage(ImportResultPackageRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Package: p, Confirm: true}); err == nil {
		t.Fatal("existing artifact directory overwritten")
	}
	v := stateOf(t, s)
	if len(v.Attempts) != 0 || len(v.Entities) != 0 {
		t.Fatal("partial import committed")
	}
	if _, err := os.Stat(filepath.Join(s.Options.Config.DataDir, "attempts", p.Attempts[0].Attempt.ID)); !os.IsNotExist(err) {
		t.Fatal("partial artifact directory retained")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("preexisting directory removed")
	}
}
