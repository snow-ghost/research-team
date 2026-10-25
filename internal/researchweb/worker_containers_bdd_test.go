//go:build linux

package researchweb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

func containerMarker(t *testing.T, name string, value any) {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicFile(filepath.Join("/control", name), body); err != nil {
		t.Fatal(err)
	}
}

func awaitContainerFile(t *testing.T, path string, seconds int) {
	t.Helper()
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("container synchronization timed out: %s", filepath.Base(path))
}

// This fixture runs only inside the isolated coordinator container. It exposes
// no test-only HTTP endpoint and receives no real provider or operator keys.
func TestWorkerContainersFixture(t *testing.T) {
	if os.Getenv("RESEARCH_WORKER_CONTAINER_FIXTURE") != "1" {
		t.Skip("container fixture only")
	}
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if bytes.Contains(body, []byte("PRIVATE-UNRELATED-MATERIAL")) {
			t.Error("unrelated material reached worker")
		}
		n := calls.Add(1)
		if n == 1 {
			containerMarker(t, "model-a.json", map[string]int32{"calls": n})
			for {
				if _, err := os.Stat("/control/release-a.json"); err == nil {
					break
				}
				select {
				case <-r.Context().Done():
					return
				case <-time.After(50 * time.Millisecond):
				}
			}
		}
		completeModel(w, "```lean\n"+submittedProof+"```")
	}))
	defer provider.Close()
	server := httptest.NewUnstartedServer(nil)
	defer server.Close()
	o := optionsFor(t, provider.URL+"/v1")
	o.Config.Listen = server.Listener.Addr().String()
	o.Config.MaxParallel = 2
	o.Config.Workers = []WorkerConfig{{ID: "worker-container-a", TokenEnv: "CONTAINER_KEY_A", Profiles: []string{"reader"}}, {ID: "worker-container-b", TokenEnv: "CONTAINER_KEY_B", Profiles: []string{"reader"}}}
	p := o.Profiles["reader"]
	p.Limits.TimeoutSeconds = 120
	o.Profiles["reader"] = p
	lookup := o.Lookup
	o.Lookup = func(k string) (string, bool) {
		if k == "CONTAINER_KEY_A" {
			return strings.Repeat("a", 40), true
		}
		if k == "CONTAINER_KEY_B" {
			return strings.Repeat("b", 40), true
		}
		return lookup(k)
	}
	if err := os.WriteFile(filepath.Join(o.Config.Workspaces[0].Path, "TASK.md"), []byte("Known True statement only"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(o.Config.Workspaces[0].Path, "unrelated.md"), []byte("PRIVATE-UNRELATED-MATERIAL"), 0600); err != nil {
		t.Fatal(err)
	}
	s := serviceFor(t, o)
	h, err := NewHTTP(s, strings.Repeat("o", 40))
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	var dropped atomic.Bool
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/worker-container-b/") && strings.HasSuffix(r.URL.Path, "/result") && dropped.CompareAndSwap(false, true) {
			recorder := httptest.NewRecorder()
			h.ServeHTTP(recorder, r)
			if recorder.Code != 200 {
				t.Errorf("result was not committed before lost acknowledgement: %d", recorder.Code)
			}
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			connection.Close()
			return
		}
		h.ServeHTTP(w, r)
	})
	server.Start()
	v := studyFor(t, s)
	study, targetA := v.Studies[0].ID, v.Studies[0].Goal
	v = act(t, s, Action{Type: "LEMMA", Study: study, Title: "Second independent target", Statement: "True", Assumptions: "Prop"})
	targetB := v.Entities[len(v.Entities)-1].ID
	for _, target := range []string{targetA, targetB} {
		v = stateOf(t, s)
		if err := s.SetFormalGoal(target, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}}); err != nil {
			t.Fatal(err)
		}
	}
	v = stateOf(t, s)
	if err := s.SetStudyBudget(study, BudgetRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Confirm: true, Note: "Isolated container drill; fake provider only", Budget: StudyBudget{MaxAttempts: 3, MaxModelRequests: 6}}); err != nil {
		t.Fatal(err)
	}
	for _, worker := range []string{"worker-container-a", "worker-container-b"} {
		if err := s.BindWorker(BindingRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Study: study, Connector: worker, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	for i, target := range []string{targetA, targetB} {
		v = act(t, s, Action{Type: "TASK", Target: target, Title: "Container task", Kind: "proof", Text: "Prove selected True"})
		worker := []string{"worker-container-a", "worker-container-b"}[i]
		if err := s.Start(RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[len(v.Tasks)-1].ID, Profile: "reader", Workspace: "source", RemoteWorker: worker, Confirm: true}); err != nil {
			t.Fatal(err)
		}
	}
	v = stateOf(t, s)
	old, second := v.Attempts[0].ID, v.Attempts[1].ID
	waitAttempt(t, s, old, func(a Attempt) bool { return a.Status == "awaiting_worker" })
	waitAttempt(t, s, second, func(a Attempt) bool { return a.Status == "awaiting_worker" })
	for _, letter := range []string{"a", "b"} {
		profile, _ := json.Marshal(p)
		if err := os.WriteFile("/control/worker-"+letter+"/profile.json", profile, 0600); err != nil {
			t.Fatal(err)
		}
		conf, _ := json.Marshal(map[string]string{"server": server.URL, "worker": "worker-container-" + letter, "token_env": "CONTAINER_KEY_" + strings.ToUpper(letter), "profile": "profile.json"})
		if err := os.WriteFile("/control/worker-"+letter+"/worker.json", conf, 0600); err != nil {
			t.Fatal(err)
		}
	}
	containerMarker(t, "ready.json", map[string]any{"workers": 2, "external_network": false})
	awaitContainerFile(t, "/control/b-resend.json", 30)
	v = stateOf(t, s)
	if v.attempt(second).Status != "candidate" || calls.Load() != 2 {
		t.Fatal("result resend repeated work", calls.Load())
	}
	// Wait for the real 45 second lease; no database clock manipulation.
	deadline := time.Now().Add(70 * time.Second)
	for time.Now().Before(deadline) {
		v = stateOf(t, s)
		if v.attempt(old).Status == "interrupted" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if v.attempt(old).Status != "interrupted" || v.attempt(old).RemoteOutcome != "unknown" || len(v.Attempts) != 2 {
		t.Fatal("outage replayed work or lost unknown outcome")
	}
	containerMarker(t, "expired.json", map[string]any{"automatic_replay": false})
	awaitContainerFile(t, "/control/a-late.json", 30)
	v = stateOf(t, s)
	if v.attempt(old).ResultSHA256 != "" || v.attempt(old).Status != "interrupted" {
		t.Fatal("late result overwrote expired assignment")
	}
	r := ReassignWorkerRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Worker: "worker-container-b", Confirm: true}
	if err := s.ReassignWorker(old, r); err != nil {
		t.Fatal(err)
	}
	if err := s.ReassignWorker(old, r); err != nil {
		t.Fatal("reassignment idempotency", err)
	}
	v = stateOf(t, s)
	next := v.Attempts[2].ID
	waitAttempt(t, s, next, func(a Attempt) bool { return a.Status == "awaiting_worker" })
	containerMarker(t, "reassigned.json", map[string]any{"new_attempt": true})
	awaitContainerFile(t, "/control/b-final.json", 30)
	v = stateOf(t, s)
	budget := s.observeBudget(&v.Data, study)
	if calls.Load() != 3 || len(v.Attempts) != 3 || v.attempt(next).Status != "candidate" || v.attempt(next).ParentAttempt != old || budget.ChargedModelRequests != 4 || budget.KnownModelRequests != 2 || budget.UnknownUsage != 1 || v.entity(targetA).Status != "open" {
		t.Fatal("fault drill invariants failed", calls.Load(), budget)
	}
	req, _ := http.NewRequest("GET", server.URL+"/api/bootstrap", nil)
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("b", 40))
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatal("worker gained operator scope")
	}
	containerMarker(t, "receipt.json", map[string]any{"workers": 2, "model_calls": calls.Load(), "known_requests": budget.KnownModelRequests, "charged_requests": budget.ChargedModelRequests, "unknown_usage": budget.UnknownUsage, "real_lease_seconds": 45, "accepted": false, "external_network": false, "different_machines": false, "shared_network_namespace": true, "late_result_rejected": true, "resend_model_calls": 0})
}

func TestWorkerContainersBDD_TwoExecutorsOutageLateResultAndDurableDelivery(t *testing.T) {
	image := os.Getenv("RESEARCH_TEST_WORKER_CONTAINER_IMAGE")
	if image == "" {
		t.Skip("set RESEARCH_TEST_WORKER_CONTAINER_IMAGE to an already installed Linux image; no real API used")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	docker := func(args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %s: %v %s", args[0], err, out)
		}
		return strings.TrimSpace(string(out))
	}
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	control := filepath.Join(root, "control")
	for _, dir := range []string{bin, control, filepath.Join(control, "worker-a"), filepath.Join(control, "worker-b"), filepath.Join(root, "outbox-a"), filepath.Join(root, "outbox-b")} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, build := range [][]string{{"test", "-c", "-o", filepath.Join(bin, "fixture"), "./internal/researchweb"}, {"build", "-o", filepath.Join(bin, "worker"), "./cmd/research-worker"}} {
		command := exec.CommandContext(ctx, "go", build...)
		command.Dir = filepath.Join("..", "..")
		command.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("static fixture build: %v %s", err, out)
		}
	}
	prefix := "research-worker-drill-" + strings.TrimPrefix(identifier("drill"), "drill-")[:12]
	coordinator, a, b := prefix+"-coordinator", prefix+"-a", prefix+"-b"
	names := []string{coordinator, a, b, prefix + "-resend", prefix + "-final"}
	t.Cleanup(func() {
		for i := len(names) - 1; i >= 0; i-- {
			out, _ := exec.Command("docker", "rm", "-f", names[i]).CombinedOutput()
			_ = out
		}
	})
	base := func(name, network string) []string {
		return []string{"run", "-d", "--pull=never", "--name", name, "--network", network, "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "--cap-drop=ALL", "--security-opt=no-new-privileges", "--read-only", "--pids-limit=128", "--memory=512m", "--tmpfs", "/tmp:rw,nosuid,nodev,size=256m", "--mount", "type=bind,source=" + bin + ",target=/bin-test,readonly"}
	}
	args := base(coordinator, "none")
	args = append(args, "--mount", "type=bind,source="+control+",target=/control", "-e", "RESEARCH_WORKER_CONTAINER_FIXTURE=1", "--entrypoint", "/bin-test/fixture", image, "-test.run=^TestWorkerContainersFixture$", "-test.v", "-test.timeout=180s")
	docker(args...)
	awaitContainerFile(t, filepath.Join(control, "ready.json"), 30)
	worker := func(name, letter, delivery string, resend bool) {
		args := base(name, "container:"+coordinator)
		key := strings.Repeat(letter, 40)
		args = append(args, "--mount", "type=bind,source="+filepath.Join(control, "worker-"+letter)+",target=/worker,readonly", "--mount", "type=bind,source="+filepath.Join(root, "outbox-"+letter)+",target=/outbox", "-e", "CONTAINER_KEY_"+strings.ToUpper(letter)+"="+key, "-e", "MODEL_TOKEN=model-test-secret", "--entrypoint", "/bin-test/worker", image, "-config", "/worker/worker.json", "-state-dir", "/outbox/"+delivery)
		if resend {
			args = append(args, "-resend-result")
		}
		docker(args...)
	}
	worker(a, "a", "first", false)
	awaitContainerFile(t, filepath.Join(control, "model-a.json"), 20)
	docker("pause", a)
	worker(b, "b", "first", false)
	if status := docker("wait", b); status != "1" {
		t.Fatal("lost acknowledgement should leave pending delivery", status, docker("logs", b))
	}
	worker(prefix+"-resend", "b", "first", true)
	if status := docker("wait", prefix+"-resend"); status != "0" {
		t.Fatal("saved result delivery failed", docker("logs", prefix+"-resend"))
	}
	write := func(name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(control, name), []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("b-resend.json")
	awaitContainerFile(t, filepath.Join(control, "expired.json"), 75)
	write("release-a.json")
	docker("unpause", a)
	if status := docker("wait", a); status != "1" || !strings.Contains(docker("logs", a), "worker API status 409") {
		t.Fatal("late result was not rejected", status, docker("logs", a))
	}
	write("a-late.json")
	awaitContainerFile(t, filepath.Join(control, "reassigned.json"), 20)
	worker(prefix+"-final", "b", "replacement", false)
	if status := docker("wait", prefix+"-final"); status != "0" {
		t.Fatal("replacement failed", docker("logs", prefix+"-final"))
	}
	write("b-final.json")
	if status := docker("wait", coordinator); status != "0" {
		t.Fatal("coordinator assertions failed", docker("logs", coordinator))
	}
	body, err := os.ReadFile(filepath.Join(control, "receipt.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Log(string(body))
}
