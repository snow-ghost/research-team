package researchweb

import (
	"encoding/json"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestReviewModulesBDD_AcceptedCoverageIncludesItsExactStatementAndGoal(t *testing.T) {
	g := leancheck.Goal{Source: "def Coverage : Prop := True -> True", Declaration: "Coverage", Candidate: "coverProof"}
	e := Entity{ID: "coverage", Kind: "obligation", Revision: 2, Status: "accepted", Statement: "P implies P", Assumptions: "Prop"}
	l := LibraryEntry{Lemma: e.ID, LemmaRevision: 2, Goal: g, Module: "CoverageModule", Result: "coverage-result", Report: &leancheck.ModuleReport{ArtifactSHA256: "pin"}}
	body, err := json.Marshal(libraryModuleMaterial(e, l))
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Goal      leancheck.Goal `json:"goal"`
		Kind      string         `json:"kind"`
		Status    string         `json:"acceptance_status"`
		Statement string         `json:"statement"`
		Result    string         `json:"result"`
	}
	if json.Unmarshal(body, &decoded) != nil || decoded.Goal != g || decoded.Kind != "obligation" || decoded.Status != "accepted" || decoded.Statement != e.Statement || decoded.Result != l.Result {
		t.Fatal("accepted coverage lost its meaning or admission evidence", string(body))
	}
	if len(acceptedLemmas(Data{Entities: []Entity{e}}, e.ID)) != 0 {
		t.Fatal("legacy lemma-only field changed its meaning")
	}
}
