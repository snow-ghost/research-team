package researchweb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestTaskScopeBDD_ChildGoalReplacesParentInstructionAndPreservesOriginal(t *testing.T) {
	d := emptyData()
	g := leancheck.Goal{Source: "namespace Child\ndef Statement : Prop := True\nend Child", Declaration: "Child.Statement", Candidate: "Child.candidate"}
	d.Entities = []Entity{{ID: "child", Revision: 2, Statement: "Current lemma", Assumptions: "Exact assumptions", FormalGoal: &g}}
	d.Tasks = []Task{{ID: "task", Target: "child", Kind: "proof", Objective: "Prove only this lemma"}}
	a := Attempt{Target: "child", TaskID: "task"}
	dir := t.TempDir()
	original := "# Original research\nProve Root.Statement and the whole algorithm.\n"
	if err := os.WriteFile(filepath.Join(dir, "TASK.md"), []byte(original), 0400); err != nil {
		t.Fatal(err)
	}
	files, err := prepareGoalFiles(d, a, dir, []FileDigest{{Path: "TASK.md"}})
	if err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(filepath.Join(dir, "TASK.md"))
	if err != nil || !strings.Contains(string(current), g.Declaration) || !strings.Contains(string(current), g.Candidate) || !strings.Contains(string(current), "Current lemma") || strings.Contains(string(current), "Root.Statement") {
		t.Fatal("current task still names a different goal", string(current), err)
	}
	archive := "ResearchBackground_" + bytesDigest([]byte(original)) + ".md"
	body, err := os.ReadFile(filepath.Join(dir, archive))
	if err != nil || string(body) != original {
		t.Fatal("original task lost", err)
	}
	for _, f := range files {
		pin, err := fileSHA(filepath.Join(dir, f.Path))
		if err != nil || pin != f.SHA256 {
			t.Fatal("changed file not pinned", f, err)
		}
	}
	if _, err := prepareGoalFiles(d, a, dir, files); err != nil {
		t.Fatal("continuation cannot regenerate current scope", err)
	}
	body, err = os.ReadFile(filepath.Join(dir, archive))
	if err != nil || string(body) != original {
		t.Fatal("continuation changed original background", err)
	}
}

func TestTaskScopeBDD_RefutationFormalizationNamesTheNegativeGoal(t *testing.T) {
	d := emptyData()
	g := leancheck.Goal{Source: "def Original : Prop := False", Declaration: "Original", Candidate: "positive"}
	d.Entities = []Entity{{ID: "goal", FormalGoal: &g}}
	d.Tasks = []Task{{ID: "task", Kind: "formalize", Objective: "Formalize the refutation"}}
	d.Teams = []ResearchTeam{{ID: "team", Refuting: true}}
	dir := t.TempDir()
	_, err := prepareGoalFiles(d, Attempt{Target: "goal", TaskID: "task", TeamID: "team", Role: "formalize"}, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(filepath.Join(dir, "TASK.md"))
	if err != nil || !strings.Contains(string(current), "ResearchRefutation.Statement") || !strings.Contains(string(current), "ResearchRefutation.candidate") {
		t.Fatal("negative goal not reflected in task", string(current), err)
	}
}
