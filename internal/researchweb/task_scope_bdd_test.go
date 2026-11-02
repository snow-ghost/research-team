package researchweb

import (
	"io"
	"net/http"
	"net/http/httptest"
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

func TestTaskScopeBDD_ApprovedContextFilesReachPromptWithoutUnrelatedMaterials(t *testing.T) {
	requests := make(chan string, 1)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- string(body)
		completeModel(w, "candidate")
	}))
	defer model.Close()
	s := serviceFor(t, optionsFor(t, model.URL+"/v1"))
	workspace := &s.Options.Config.Workspaces[0]
	workspace.ContextFiles = []string{"S3.md"}
	if err := os.WriteFile(filepath.Join(workspace.Path, "S3.md"), []byte("APPROVED-BFS-CERTIFICATE"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace.Path, "unrelated.md"), []byte("UNRELATED-PRIVATE-MATERIAL"), 0600); err != nil {
		t.Fatal(err)
	}
	v := studyFor(t, s)
	v = act(t, s, Action{Type: "TASK", Target: v.Studies[0].Goal, Kind: "proof", Title: "Artifact analysis", Text: "Analyze only the supplied evidence"})
	if err := s.Start(RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[0].ID, Profile: "reader", Workspace: "source", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	waitAttempt(t, s, stateOf(t, s).Attempts[0].ID, func(a Attempt) bool { return a.Status == "candidate" })
	body := <-requests
	if !strings.Contains(body, "APPROVED-BFS-CERTIFICATE") || strings.Contains(body, "UNRELATED-PRIVATE-MATERIAL") {
		t.Fatal("workspace context allowlist was not enforced", body)
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
