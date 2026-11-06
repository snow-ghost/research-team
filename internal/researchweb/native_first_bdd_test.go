//go:build linux

package researchweb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestNativeFirstServiceSkipsModelAfterLeanVerification(t *testing.T) {
	var modelCalls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		modelCalls.Add(1)
		completeModel(w, "model fallback")
	}))
	defer model.Close()
	o := optionsFor(t, model.URL+"/v1")
	o.Config.LeanConfig = "pinned"
	o.Checker = checkerFunc(func(_ context.Context, goal leancheck.Goal, source, _ string) (leancheck.Report, error) {
		status := "failed"
		if strings.Contains(source, "omega") {
			status = "verified"
		}
		return leancheck.Report{Status: status, AuditSHA256: leancheck.AuditDigest(), GoalSHA256: leancheck.Digest(goal), SourceSHA256: leancheck.Digest(source)}, nil
	})
	s := serviceFor(t, o)
	v := studyFor(t, s)
	target := v.Studies[0].Goal
	if err := s.SetFormalGoal(target, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}}); err != nil {
		t.Fatal(err)
	}
	v = act(t, s, Action{Type: "TASK", Target: target, Kind: "proof", Title: "Native first", Text: "Prove the goal"})
	request := RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[0].ID, Profile: "reader", Workspace: "source", Confirm: true}
	if err := s.Start(request); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	a := waitAttempt(t, s, v.Attempts[0].ID, func(a Attempt) bool { return !active(a.Status) })
	if a.Status != "candidate" || modelCalls.Load() != 0 {
		t.Fatalf("native verification did not skip model: %+v calls=%d", a, modelCalls.Load())
	}
	v = stateOf(t, s)
	if stored := v.attempt(a.ID); stored == nil || stored.Native == nil || stored.Native.Trials != 2 || stored.Native.Verification.Status != "verified" {
		t.Fatalf("native metrics were not persisted: %+v", stored)
	}
	result, err := s.Result(a.ID)
	if err != nil || result.Native == nil || result.Native.ModelFallback || result.Native.Trials != 2 || result.Native.Verification.Status != "verified" || !strings.Contains(result.Candidate, "omega") {
		t.Fatalf("native report missing: %+v err=%v", result, err)
	}
}
