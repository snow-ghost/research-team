package researchcompare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
)

func completeAnalysisFixture() Report {
	r := Report{Version: 3, MaxRequests: 216, ConfigurationSHA256: strings.Repeat("a", 64), PolicySHA256: DemandPolicyDigest()}
	for task := 0; task < 6; task++ {
		for rep := 1; rep <= 2; rep++ {
			for _, mode := range []string{"single", "team", "demand", "lean"} {
				r.Runs = append(r.Runs, Run{Case: fmt.Sprintf("task-%03d", task), Class: fmt.Sprintf("class-%d", task/2), Mode: mode, Repetition: rep, GoalSHA256: leancheck.Digest(task), Status: "inconclusive", Profiles: map[string]string{}})
			}
		}
	}
	return r
}

func TestAnalysisBDD_ClustersRepeatsAndSeparatesNativeSuccessFromReview(t *testing.T) {
	r := completeAnalysisFixture()
	r.Runs[0].Verification = &leancheck.Report{Status: "verified"}
	r.Runs[0].RequestUpperBound = 2
	r.Runs[0].MeasuredRequests = 2
	r.Runs[1].Verification = &leancheck.Report{Status: "verified"}
	r.Runs[1].ReviewAccepted = true
	a, err := Analyze(r, "")
	if err != nil || a.Preliminary || a.IndependentTasks != 6 || a.RepeatObservations != 12 || len(a.Classes) != 3 || len(a.Effects) != 12 || len(a.ReviewGaps) != 1 || a.Summary.OperatorAcceptance {
		t.Fatal(a, err)
	}
	if a.Effects[0].ReviewedSuccessDifference != 0.5 || a.Effects[0].RequestDifference != -1 {
		t.Fatal("repetitions were not averaged within task", a.Effects[0])
	}
	r.Runs = r.Runs[:3]
	a, err = Analyze(r, "")
	if err != nil || !a.Preliminary || a.IndependentTasks != 0 || len(a.Effects) != 0 {
		t.Fatal("incomplete group compared", a, err)
	}
}

func TestAnalysisBDD_ReviewArtifactMustMatchRecordedDigest(t *testing.T) {
	r := completeAnalysisFixture()
	r.Runs[0].Verification = &leancheck.Report{Status: "verified"}
	result := execution.Result{Status: "candidate", Candidate: `{"rubric":"proof-review@2","summary":"Need premise","findings":[{"severity":"question","category":"premise","text":"Premise missing","evidence":"No h","location":"step 2"}]}`}
	r.Runs[0].Stages = []StageObservation{{Role: "review", Status: "candidate", ResultSHA256: leancheck.Digest(result)}}
	dir := t.TempDir()
	work := filepath.Join(dir, "00-task-000-single-1", "review")
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(result)
	if err := os.WriteFile(filepath.Join(work, "result.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	a, err := Analyze(r, dir)
	if err != nil || a.ReviewGaps[0].ArtifactStatus != "verified_binding" || len(a.ReviewGaps[0].Findings) != 1 {
		t.Fatal(a, err)
	}
	r.Runs[0].Stages[0].ResultSHA256 = "changed"
	a, err = Analyze(r, dir)
	if err != nil || a.ReviewGaps[0].ArtifactStatus != "unavailable" || len(a.ReviewGaps[0].Findings) != 0 {
		t.Fatal("unbound review used", a, err)
	}
	result.Candidate = `{"verdict":"approve"}`
	b, _ = json.Marshal(result)
	if err := os.WriteFile(filepath.Join(work, "result.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	r.Runs[0].Stages[0].ResultSHA256 = leancheck.Digest(result)
	a, err = Analyze(r, dir)
	if err != nil || a.ReviewGaps[0].ArtifactStatus != "bound_invalid_contract" || a.ReviewGaps[0].ReviewStatus != "invalid_report" || len(a.ReviewGaps[0].Findings) != 0 {
		t.Fatal("invalid schema interpreted as mathematical disagreement", a, err)
	}
}

func TestFinalizationBDD_LostAcknowledgementReconcilesWithoutDuplicateDelivery(t *testing.T) {
	r := completeAnalysisFixture()
	posts := 0
	stored := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/bootstrap" {
			records := []map[string]string{}
			if stored != "" {
				records = append(records, map[string]string{"id": "comparison-test", "report_sha256": stored})
			}
			json.NewEncoder(w).Encode(map[string]any{"state": map[string]any{"revision": 10, "comparisons": records}})
			return
		}
		if request.URL.Path != "/api/comparisons" {
			t.Error("unexpected finalizer endpoint")
			w.WriteHeader(404)
			return
		}
		posts++
		var body struct {
			Report    Report `json:"report"`
			RequestID string `json:"request_id"`
		}
		json.NewDecoder(request.Body).Decode(&body)
		stored = leancheck.Digest(body.Report)
		if body.RequestID != "finalize-"+stored[:48] {
			t.Error("unstable request id")
		}
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		connection.Close()
	}))
	defer server.Close()
	f := Finalizer{Server: server.URL, Token: strings.Repeat("x", 40), Pin: r.ConfigurationSHA256, Output: filepath.Join(t.TempDir(), "private"), Client: server.Client()}
	d, err := f.Deliver(context.Background(), r)
	if err == nil || d.State != "outcome_unknown" || posts != 1 {
		t.Fatal(d, err, posts)
	}
	d, err = f.Deliver(context.Background(), r)
	if err != nil || d.State != "acknowledged" || posts != 1 {
		t.Fatal("delivery replayed instead of reconciliation", d, err, posts)
	}
	if err := os.Remove(filepath.Join(f.Output, "analysis.json")); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Deliver(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.Output, "analysis.json")); err != nil {
		t.Fatal("local bundle was not repaired", err)
	}
	r.Runs = r.Runs[:2]
	if _, err = f.Deliver(context.Background(), r); err != ErrIncomplete || posts != 1 {
		t.Fatal("incomplete report delivered", err)
	}
}

func TestFinalizationBDD_RevisionConflictReconcilesBeforeNewDelivery(t *testing.T) {
	r := completeAnalysisFixture()
	posts, revision, stored := 0, 10, ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/bootstrap" {
			records := []map[string]string{}
			if stored != "" {
				records = append(records, map[string]string{"id": "comparison-test", "report_sha256": stored})
			}
			json.NewEncoder(w).Encode(map[string]any{"state": map[string]any{"revision": revision, "comparisons": records}})
			return
		}
		if request.URL.Path != "/api/comparisons" || request.Method != "POST" {
			t.Error("unexpected endpoint")
			w.WriteHeader(404)
			return
		}
		posts++
		if posts == 1 {
			revision++
			w.WriteHeader(409)
			return
		}
		var body comparisonDeliveryRequest
		if json.NewDecoder(request.Body).Decode(&body) != nil || body.ExpectedRevision != revision {
			t.Error("stale request replayed after known conflict")
		}
		stored = leancheck.Digest(body.Report)
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer server.Close()
	f := Finalizer{Server: server.URL, Token: strings.Repeat("x", 40), Pin: r.ConfigurationSHA256, Output: filepath.Join(t.TempDir(), "private"), Client: server.Client()}
	d, err := f.Deliver(context.Background(), r)
	if err != nil || d.State != "revision_conflict" || len(d.Request) != 0 || posts != 1 {
		t.Fatal(d, err, posts)
	}
	d, err = f.Deliver(context.Background(), r)
	if err != nil || d.State != "acknowledged" || posts != 2 {
		t.Fatal(d, err, posts)
	}
}
