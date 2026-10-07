//go:build linux

package researchweb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestSubmittedReviewBDD_ExactSourceBindingAndSeparateAuthorship(t *testing.T) {
	var received string
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&b)
		for _, m := range b.Messages {
			if strings.Contains(m.Content, "review_materials") {
				received = m.Content
			}
		}
		completeModel(w, `{"summary":"Exact submitted source reviewed","findings":[]}`)
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
	id := submitFor(t, s, goal, "Codex")
	waitVerification(t, s, id)
	v = act(t, s, Action{Type: "TASK", Target: goal, Kind: "review", Title: "Review", Text: "Review the pinned source"})
	r := RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[0].ID, Profile: "reader", Workspace: "source", Confirm: true, ReviewVerification: id}
	if err := s.Start(r); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(r); err != nil {
		t.Fatal("idempotency", err)
	}
	a := waitAttempt(t, s, stateOf(t, s).Attempts[0].ID, func(a Attempt) bool { return a.Status == "candidate" })
	if a.ProofBinding == nil || a.ProofBinding.Verification != id || a.ProofBinding.SourceSHA256 != leancheck.Digest(submittedProof) {
		t.Fatal("missing exact binding", a)
	}
	input, err := s.readInput(a)
	if err != nil {
		t.Fatal(err)
	}
	if input.attempt(a.ID).ProofBinding == nil || !strings.Contains(received, `\"author\":\"Codex\"`) && !strings.Contains(received, `"author":"Codex"`) {
		t.Fatal("reviewer did not receive provenance", received)
	}
	if !strings.Contains(received, `review_materials`) || !strings.Contains(received, leancheck.Digest(submittedProof)) || !strings.Contains(received, `theorem Candidate`) {
		t.Fatal("missing exact review material")
	}
	v = stateOf(t, s)
	if v.entity(goal).Proof != "" || v.entity(goal).Status != "open" {
		t.Fatal("review implicitly attached or accepted source")
	}
}

func TestSubmittedReviewBDD_RejectsForeignFailedStaleAndSelfReview(t *testing.T) {
	for _, failure := range []string{"foreign", "failed", "stale", "author", "task_kind"} {
		t.Run(failure, func(t *testing.T) {
			outcome := "verified"
			if failure == "failed" {
				outcome = "failed"
			}
			s, goal := submittedService(t, "sqlite", outcome)
			author := "Codex"
			if failure == "author" {
				author = "executor:reader"
			}
			id := submitFor(t, s, goal, author)
			waitVerification(t, s, id)
			if failure == "foreign" {
				v := studyFor(t, s)
				goal = v.Studies[len(v.Studies)-1].Goal
			}
			if failure == "stale" {
				v := stateOf(t, s)
				g := *v.entity(goal).FormalGoal
				g.Source += "\n-- new version"
				if err := s.SetFormalGoal(goal, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: g}); err != nil {
					t.Fatal(err)
				}
			}
			kind := "review"
			if failure == "task_kind" {
				kind = "proof"
			}
			v := act(t, s, Action{Type: "TASK", Target: goal, Kind: kind, Title: "Review", Text: "Pinned review"})
			if err := s.Start(RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[len(v.Tasks)-1].ID, Profile: "reader", Workspace: "source", Confirm: true, ReviewVerification: id}); err == nil {
				t.Fatal("invalid binding queued")
			}
			if len(stateOf(t, s).Attempts) != 0 {
				t.Fatal("rejected launch consumed attempt")
			}
		})
	}
}
