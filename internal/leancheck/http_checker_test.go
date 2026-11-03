package leancheck

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHTTPCheckerCheckAndMatchValidateBindings(t *testing.T) {
	goal := Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}
	source := "import Goal\ntheorem Candidate : Statement := by trivial\n"
	module := ModuleReport{ArtifactSHA256: "module-artifact"}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing service token")
		}
		switch r.URL.Path {
		case "/v1/check":
			_ = json.NewEncoder(w).Encode(Report{Status: "verified", GoalSHA256: Digest(goal), SourceSHA256: Digest(source), AuditSHA256: AuditDigest()})
		case "/v1/match":
			_ = json.NewEncoder(w).Encode(MatchReport{Status: "obligations", GoalSHA256: Digest(goal), ArtifactSHA256: module.ArtifactSHA256, MatcherSHA256: MatcherDigest()})
		case "/v1/inspect":
			_ = json.NewEncoder(w).Encode(SignatureReport{Status: "verified", GoalSHA256: Digest(goal), ArtifactSHA256: module.ArtifactSHA256, InspectorSHA256: InspectorDigest()})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	checker := NewHTTPChecker(server.URL, "test-token")
	report, err := checker.Check(context.Background(), goal, source, "")
	if err != nil || report.Status != "verified" {
		t.Fatal(report, err)
	}
	match, err := checker.MatchModule(context.Background(), goal, "module", module, "candidate", "")
	if err != nil || match.Status != "obligations" {
		t.Fatal(match, err)
	}
	dir := filepath.Join(t.TempDir(), "nested")
	inspection, err := checker.InspectModule(context.Background(), goal, "module", module, dir)
	if err != nil || inspection.Status != "verified" {
		t.Fatal(inspection, err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("inspection directory was not created", err)
	}
}

func TestHTTPCheckerRejectsBindingMismatchAndHidesToken(t *testing.T) {
	goal := Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Report{Status: "verified", GoalSHA256: "wrong"})
	}))
	defer server.Close()
	checker := NewHTTPChecker(server.URL, "private-token")
	_, err := checker.Check(context.Background(), goal, "source", "")
	if err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatal("invalid report accepted or token disclosed", err)
	}
}
