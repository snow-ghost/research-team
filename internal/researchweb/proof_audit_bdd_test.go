//go:build linux

package researchweb

import (
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestAssemblyBDD_TransitivelyDeclaredProofsMustAppearInTheKernelTerm(t *testing.T) {
	root := Entity{ID: "root", RequireDependencyProofs: true, Dependencies: []string{"cover"}}
	d := emptyData()
	d.Entities = []Entity{root, {ID: "cover", Dependencies: []string{"part"}}, {ID: "part"}}
	d.Library = []LibraryEntry{{ID: "cover-lib", Lemma: "cover", Status: "ready", Module: "CoverModule", Goal: leancheck.Goal{Candidate: "coverProof"}, Report: &leancheck.ModuleReport{ArtifactSHA256: "cover-pin"}}, {ID: "part-lib", Lemma: "part", Status: "ready", Module: "PartModule", Goal: leancheck.Goal{Candidate: "partProof"}, Report: &leancheck.ModuleReport{ArtifactSHA256: "part-pin"}}}
	pins := map[string]string{"cover-lib": "cover-pin", "part-lib": "part-pin"}
	r := leancheck.Report{Dependencies: &leancheck.ProofDependencies{ProcedureSHA256: leancheck.DependencyDigest(), LibraryReferences: []leancheck.DeclarationReference{{Module: "CoverModule", Declaration: "coverProof"}}}}
	missing := missingRequiredLemmas(&d, &root, pins, &r)
	if len(missing) != 1 || missing[0] != "dependency:part" {
		t.Fatal("transitive proof omitted", missing)
	}
	r.Dependencies.LibraryReferences = append(r.Dependencies.LibraryReferences, leancheck.DeclarationReference{Module: "PartModule", Declaration: "partProof"})
	if len(missingRequiredLemmas(&d, &root, pins, &r)) != 0 {
		t.Fatal("exact proof references rejected")
	}
	delete(pins, "part-lib")
	if len(missingRequiredLemmas(&d, &root, pins, &r)) != 1 {
		t.Fatal("unpinned proof used")
	}
}

func TestProofAuditBDD_RequiredModuleMustBeReferencedByExactDeclaration(t *testing.T) {
	s, p := packageFixture(t)
	d := stateOf(t, s).Data
	id := "library-11111111111111111111111111111111"
	module := "ResearchLemma_11111111111111111111111111111111"
	d.Library = append(d.Library, LibraryEntry{ID: id, Lemma: p.Target.ID, LemmaRevision: p.Target.Revision, Result: p.Result.ID, Module: module, Goal: p.Verification.Goal, Status: "ready", Report: &leancheck.ModuleReport{Status: "ready", ArtifactSHA256: "pinned", Artifacts: map[string]string{module + ".olean": "pinned"}}})
	g := leancheck.Goal{Source: "def Target : Prop := True", Declaration: "Target", Candidate: "Candidate2"}
	e := Entity{ID: "H-audit", Study: p.Study.ID, Revision: 1, FormalGoal: &g, Dependencies: []string{p.Target.ID}, RequiredLibraries: []string{id}}
	d.Entities = append(d.Entities, e)
	v := Verification{ID: "verify-audit", Target: e.ID, TargetRevision: 1, Goal: g, Source: "candidate", Status: "verified", LibraryPins: map[string]string{id: "pinned"}, Report: &leancheck.Report{Status: "verified", GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest("candidate")}}
	if verificationMatches(&d, v) {
		t.Fatal("import alone satisfied required dependency")
	}
	v.Report.Dependencies = &leancheck.ProofDependencies{ProcedureSHA256: leancheck.DependencyDigest(), Direct: []string{}, LibraryReferences: []leancheck.DeclarationReference{{Module: module, Declaration: "wrong"}}}
	if verificationMatches(&d, v) {
		t.Fatal("wrong declaration satisfied required dependency")
	}
	v.Report.Dependencies.LibraryReferences[0].Declaration = p.Verification.Goal.Candidate
	if !verificationMatches(&d, v) {
		t.Fatal("verified exact dependency rejected")
	}
	d.Verifications = append(d.Verifications, v)
	checks, _, _ := proofAudit(&d, e)
	if len(checks) != 1 || !checks[0].Current || !checks[0].DependencyAuditCurrent || len(checks[0].Lemmas) != 1 || !checks[0].Lemmas[0].Required || !checks[0].Lemmas[0].Referenced {
		t.Fatal(checks)
	}
	v.Report.Dependencies.ProcedureSHA256 = "old"
	if verificationMatches(&d, v) {
		t.Fatal("old procedure trusted for new requirement")
	}
}

func TestProvenanceBDD_NotesAreTypedAndUnspecifiedIsNotAMathematicalHint(t *testing.T) {
	team := ResearchTeam{MethodBasis: &MethodBasis{Source: "planner", Rationale: "A proposed method"}}
	recordOperatorNote(&team, TeamCommand{RequestID: "cmd-one", Note: "Increase time", NoteKind: "operational"})
	recordOperatorNote(&team, TeamCommand{RequestID: "cmd-two", Note: "Use induction", NoteKind: "mathematical_hint"})
	recordOperatorNote(&team, TeamCommand{RequestID: "cmd-three", Note: "Unclassified"})
	if len(team.Notes) != 3 || team.Notes[0].Kind != "operational" || team.Notes[1].Kind != "mathematical_hint" || team.Notes[2].Kind != "unspecified" || team.MethodBasis.Source != "planner" {
		t.Fatal(team)
	}
}
