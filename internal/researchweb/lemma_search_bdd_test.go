//go:build linux

package researchweb

import (
	"strings"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestLemmaSearchBDD_StructureConditionsAndStaleness(t *testing.T) {
	s := serviceFor(t, optionsFor(t, "http://127.0.0.1:1/v1"))
	v := studyFor(t, s)
	target := v.Studies[0].Goal
	g := leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}
	if err := s.SetFormalGoal(target, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: g}); err != nil {
		t.Fatal(err)
	}
	var lemmaID, libraryID string
	if err := s.Store.Change(0, "", "", "Fixture", "", "test", func(d *Data) error {
		lemma := newEntity(identifier("L"), "Typed lemma", "lemma", d.Studies[0].ID, "True", "n is natural")
		lemmaID = lemma.ID
		lemma.Status = "accepted"
		lemma.FormalGoal = &g
		lemma.Proof = submittedProof
		lemma.ProofAuthor = "executor:author"
		lemma.ProofVerification = identifier("verify")
		lemma.ResearchResult = identifier("result")
		v := Verification{ID: lemma.ProofVerification, Target: lemma.ID, TargetRevision: lemma.Revision, Origin: "submitted", Author: lemma.ProofAuthor, Status: "verified", Goal: g, Source: submittedProof, Report: &leancheck.Report{Status: "verified", GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(submittedProof), EnvironmentSHA256: "fixture"}}
		review, counter := identifier("run"), identifier("run")
		d.Attempts = append(d.Attempts, Attempt{ID: review, Status: "candidate", ResultSHA256: "review"}, Attempt{ID: counter, Status: "candidate", ResultSHA256: "counter"})
		result := ResearchResult{ID: lemma.ResearchResult, Target: lemma.ID, TargetRevision: lemma.Revision, Author: lemma.ProofAuthor, Binding: ProofBinding{v.ID, v.Report.GoalSHA256, v.Report.SourceSHA256}, EnvironmentSHA256: "fixture", ReviewAttempt: review, CounterAttempt: counter, ReviewResultSHA256: "review", CounterResultSHA256: "counter", Status: "accepted", Counter: CounterReport{Outcome: "none_found"}}
		d.Entities = append(d.Entities, lemma)
		d.Verifications = append(d.Verifications, v)
		d.Results = append(d.Results, result)
		libraryID = identifier("library")
		d.Library = append(d.Library, LibraryEntry{ID: libraryID, Lemma: lemma.ID, LemmaRevision: lemma.Revision, Result: result.ID, Goal: g, Module: "ResearchLemma_" + strings.TrimPrefix(libraryID, "library-"), Status: "ready", Report: &leancheck.ModuleReport{Status: "ready", Artifacts: map[string]string{"module.olean": "fixture"}}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	r := SignatureRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Signature: LemmaSignature{Library: libraryID, Domain: "arithmetic", Parameters: []LemmaParameter{{Name: "n", Type: "Nat"}}, Conditions: []string{"0 < n"}, Conclusion: "polynomial equality"}}
	if err := s.IndexLemma(r); err != nil {
		t.Fatal(err)
	}
	hits, err := s.SearchLemmas(LemmaQuery{Domain: "arithmetic", ParameterTypes: []string{"Nat"}, Conclusion: "polynomial equality"})
	if err != nil || len(hits) != 1 || len(hits[0].MissingConditions) != 1 || hits[0].Applicability != "requires_lean_obligation" {
		t.Fatal(hits, err)
	}
	if hits, err := s.SearchLemmas(LemmaQuery{ParameterTypes: []string{"Int"}}); err != nil || len(hits) != 0 {
		t.Fatal("type mismatch ignored", hits, err)
	}
	v = stateOf(t, s)
	if err := s.CreateApplicability(ApplicabilityRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Target: target, Library: libraryID, Substitution: "n := 2", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	app := v.entity(v.Applications[0].ID)
	if app.Status != "open" || app.FormalGoal.Declaration != g.Declaration || !strings.HasSuffix(app.FormalGoal.Source, g.Source) || v.entity(target).Status == "accepted" {
		t.Fatal("annotations accepted or weakened goal", app)
	}
	if err := s.Store.Change(0, "", "", "Invalidate lemma", lemmaID, "test", func(d *Data) error { d.entity(lemmaID).Status = "challenged"; return nil }); err != nil {
		t.Fatal(err)
	}
	hits, err = s.SearchLemmas(LemmaQuery{})
	if err != nil || len(hits) != 0 {
		t.Fatal("stale lemma found", hits, err)
	}
}
