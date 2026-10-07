//go:build linux

package researchweb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

func waitVerification(t *testing.T, s *Service, id string) View {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		v := stateOf(t, s)
		if check := v.verification(id); check != nil && check.Status != "queued" && check.Status != "running" {
			return v
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("verification did not finish")
	return View{}
}
func TestVerificationBDD_AcceptanceRequiresExactSourceAndCurrentGoal(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		completeModel(w, "```lean\nimport Goal\ntheorem Candidate : Statement := by trivial\n```")
	}))
	defer model.Close()
	o := optionsFor(t, model.URL+"/v1")
	o.Checker = checkerFunc(func(_ context.Context, g leancheck.Goal, source, _ string) (leancheck.Report, error) {
		return leancheck.Report{AuditSHA256: leancheck.AuditDigest(), Status: "verified", GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(source)}, nil
	})
	s := serviceFor(t, o)
	v := studyFor(t, s)
	goal := v.Studies[0].Goal
	if err := s.SetFormalGoal(goal, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}}); err != nil {
		t.Fatal(err)
	}
	v = act(t, s, Action{Type: "TASK", Target: goal, Kind: "proof", Title: "Proof", Text: "Prove statement"})
	if err := s.Start(RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[0].ID, Profile: "reader", Workspace: "source", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	a := waitAttempt(t, s, stateOf(t, s).Attempts[0].ID, func(a Attempt) bool { return a.Status == "candidate" })
	v = act(t, s, Action{Type: "ATTACH_PROOF", Target: goal, Attempt: a.ID})
	accept := Action{Type: "REVIEW", Target: goal, Decision: "accept", Text: "Проверена точная постановка.", ExpectedRevision: v.Revision, RequestID: identifier("cmd")}
	if err := s.Act(accept); err == nil {
		t.Fatal("accepted without Lean")
	}
	verify := VerifyRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Attempt: a.ID, Source: "different proof"}
	if err := s.StartVerification(verify); err == nil {
		t.Fatal("substituted source verified")
	}
	verify.Source = ""
	if err := s.StartVerification(verify); err != nil {
		t.Fatal(err)
	}
	v = waitVerification(t, s, stateOf(t, s).Verifications[0].ID)
	if !hasVerifiedProof(&v.Data, v.entity(goal)) {
		t.Fatal("attached proof not verifiable")
	}
	changed := *v.entity(goal).FormalGoal
	changed.Source = "def Statement : Prop := False"
	if err := s.SetFormalGoal(goal, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: changed}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	if hasVerifiedProof(&v.Data, v.entity(goal)) || v.entity(goal).Proof != "" {
		t.Fatal("old verification survived goal replacement")
	}
}

func TestVerificationBDD_LegacyAuditCannotAuthorizeNewAcceptance(t *testing.T) {
	for _, digest := range []string{"", strings.Repeat("b", 64)} {
		t.Run("audit_"+digest, func(t *testing.T) {
			s, goal := submittedService(t, "sqlite", "verified")
			id := submitFor(t, s, goal, "Codex")
			v := waitVerification(t, s, id)
			if err := s.AttachVerification(id, AttachVerificationRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd")}); err != nil {
				t.Fatal(err)
			}
			if err := s.Store.Change(0, "", "", "Historical test report", id, "checker", func(d *Data) error {
				d.verification(id).Report.AuditSHA256 = digest
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			v = stateOf(t, s)
			err := s.Act(Action{Type: "REVIEW", Target: goal, Decision: "accept", Text: "Reviewed", ExpectedRevision: v.Revision, RequestID: identifier("cmd")})
			if err == nil || !strings.Contains(err.Error(), "актуальной программой аудита") {
				t.Fatal("legacy audit authorized acceptance", err)
			}
			after := stateOf(t, s)
			if after.entity(goal).Status == "accepted" || after.verification(id).Report.AuditSHA256 != digest {
				t.Fatal("acceptance or historical report changed")
			}
		})
	}
}

func TestVerificationBDD_RecheckIsPreferredWithoutRewritingLegacyReport(t *testing.T) {
	g := leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}
	e := Entity{ID: "goal", Revision: 1, FormalGoal: &g, ProofAttempt: "attempt"}
	old := Verification{ID: "old", Target: e.ID, TargetRevision: 1, Attempt: e.ProofAttempt, Goal: g, Source: submittedProof, Status: "verified", Report: &leancheck.Report{Status: "verified", GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(submittedProof)}}
	fresh := old
	fresh.ID = "fresh"
	fresh.Report = &leancheck.Report{AuditSHA256: leancheck.AuditDigest(), Status: "verified", GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(submittedProof)}
	d := Data{Entities: []Entity{e}, Verifications: []Verification{old, fresh}}
	if found := proofVerification(&d, &e); found == nil || found.ID != fresh.ID || d.Verifications[0].Report.AuditSHA256 != "" {
		t.Fatal("new audit not selected or historical report changed")
	}
}

func TestVerificationBDD_UnrelatedConsumerDoesNotRecurseIntoSourceProof(t *testing.T) {
	g := leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}
	source := newEntity("lemma", "Source", "lemma", "study", "True", "Prop")
	source.Status, source.ProofAttempt, source.ProofAuthor, source.ResearchResult = "accepted", "source-attempt", "author", "result"
	source.FormalGoal = &g
	proof := Verification{ID: "source-proof", Target: source.ID, TargetRevision: source.Revision, Attempt: source.ProofAttempt, Goal: g, Source: submittedProof, Status: "verified", Report: &leancheck.Report{Status: "verified", GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(submittedProof)}}
	consumer := newEntity("consumer", "Consumer", "hypothesis", "study", "True", "Prop")
	consumer.FormalGoal, consumer.ProofAttempt = &g, "consumer-attempt"
	dependent := proof
	dependent.ID, dependent.Target, dependent.Attempt = "consumer-proof", consumer.ID, consumer.ProofAttempt
	dependent.LibraryPins = map[string]string{"library": "pin"}
	result := ResearchResult{ID: source.ResearchResult, Target: source.ID, TargetRevision: source.Revision, Author: source.ProofAuthor, Binding: ProofBinding{proof.ID, proof.Report.GoalSHA256, proof.Report.SourceSHA256}, ReviewAttempt: "review", CounterAttempt: "counter", ReviewResultSHA256: "review-hash", CounterResultSHA256: "counter-hash", Counter: CounterReport{Outcome: "none_found"}}
	library := LibraryEntry{ID: "library", Lemma: source.ID, LemmaRevision: source.Revision, Result: result.ID, Status: "ready", Report: &leancheck.ModuleReport{Status: "ready", ArtifactSHA256: "pin", Artifacts: map[string]string{"module.olean": "pin"}}}
	d := Data{Entities: []Entity{source, consumer}, Verifications: []Verification{proof, dependent}, Results: []ResearchResult{result}, Library: []LibraryEntry{library}, Attempts: []Attempt{{ID: "review", ResultSHA256: "review-hash"}, {ID: "counter", ResultSHA256: "counter-hash"}}}
	if found := proofVerification(&d, &source); found == nil || found.ID != proof.ID {
		t.Fatal("unrelated verification prevented source proof selection")
	}
	if !libraryMatches(&d, library) || !verificationMatches(&d, dependent) {
		t.Fatal("valid consumer or legacy library became unusable")
	}
	refreshEvidence(&d)
	if d.Results[0].Status != "accepted" || d.Library[0].Status != "ready" {
		t.Fatal("evidence refresh lost a valid legacy result")
	}
}
func TestTeamBDD_RejectionExhaustsBudgetWithoutAutomaticAcceptance(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Messages []struct{ Content string } }
		json.NewDecoder(r.Body).Decode(&body)
		for _, m := range body.Messages {
			if strings.Contains(m.Content, "ROLE_review") {
				completeModel(w, `{"summary":"Reviewed","findings":[]}`)
				return
			}
		}
		completeModel(w, `{"outcome":"none_found","evidence":"Boundary cases"}`)
	}))
	defer model.Close()
	s := serviceFor(t, teamOptions(t, model.URL+"/v1"))
	v := studyFor(t, s)
	if err := s.StartTeam(TeamRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: v.Studies[0].ID, Profiles: map[string]string{"proof": "proof", "counterexample": "counterexample", "review": "review"}, Workspace: "source", MaxAttempts: 4, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	id := stateOf(t, s).Teams[0].ID
	v = waitTeam(t, s, id, "awaiting_review")
	act(t, s, Action{Type: "REVIEW", Target: v.Studies[0].Goal, Decision: "reject", Text: "Нужно разобрать дополнительный случай."})
	v = waitTeam(t, s, id, "blocked")
	if v.Teams[0].UsedAttempts > 4 || v.Entities[0].Status == "accepted" {
		t.Fatal("team bypassed budget or operator decision")
	}
}
