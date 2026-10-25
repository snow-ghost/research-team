//go:build linux

package researchweb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
	"github.com/snow-ghost/research-team/internal/researchcompare"
)

func TestEvaluationWatchBDD_PinnedProgressWithoutPrivateMaterials(t *testing.T) {
	body, err := os.ReadFile("../../examples/evaluation-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var set researchcompare.EvaluationSet
	if err := json.Unmarshal(body, &set); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "PRIVATE-FILE.json")
	pin := strings.Repeat("a", 64)
	o := optionsFor(t, "http://127.0.0.1:1/v1")
	o.Config.Evaluations = []EvaluationWatch{{ID: "trial-local", Label: "Local educational comparison", File: file, ConfigurationSHA256: pin}}
	if err := resolveEvaluationWatches(&o.Config, "."); err != nil {
		t.Fatal(err)
	}
	s := serviceFor(t, o)
	if got := s.EvaluationStatuses(); got[0].Status != "not_started" {
		t.Fatal(got)
	}
	r := researchcompare.Report{Version: 4, MaxRequests: 432, ConfigurationSHA256: pin, PolicySHA256: researchcompare.EvaluationPolicyDigest(), EvaluationSet: &set, CaseSetSHA256: leancheck.Digest(set), Runs: []researchcompare.Run{{Case: set.Cases[0].ID, Class: set.Cases[0].Class, GoalSHA256: leancheck.Digest(set.Cases[0].Goal), Mode: "single", Repetition: 1, Status: "running", RequestUpperBound: 1}}}
	write := func() {
		t.Helper()
		b, _ := json.Marshal(r)
		if err := os.WriteFile(file, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	got := s.EvaluationStatuses()
	if got[0].Status != "running" || got[0].RequestUpperBound != 1 || len(got[0].Runs) != 1 {
		t.Fatal(got)
	}
	public, _ := json.Marshal(got)
	if strings.Contains(string(public), "PRIVATE-FILE") || strings.Contains(string(public), set.Cases[0].Goal.Source) {
		t.Fatal("progress leaked private file or source")
	}
	r.Simulation = true
	write()
	if s.EvaluationStatuses()[0].Status != "invalid" {
		t.Fatal("simulation presented as live")
	}
	r.Simulation = false
	r.ConfigurationSHA256 = strings.Repeat("b", 64)
	write()
	if s.EvaluationStatuses()[0].Status != "invalid" {
		t.Fatal("another experiment accepted")
	}
	o.Config.Evaluations[0].File = filepath.Join(o.Config.WebDir, "report.json")
	if err := resolveEvaluationWatches(&o.Config, "."); err == nil {
		t.Fatal("private report allowed under static web root")
	}
}
