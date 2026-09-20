//go:build linux

package researchweb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/snow-ghost/research-team/internal/coddy"
	"github.com/snow-ghost/research-team/internal/execution"
)

func optionsFor(t *testing.T, base string) Options {
	t.Helper()
	dir := t.TempDir()
	web, workspace := filepath.Join(dir, "web"), filepath.Join(dir, "source")
	for _, path := range []string{web, workspace} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("<!doctype html><title>Research</title>"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "lemma.txt"), []byte("original statement"), 0600); err != nil {
		t.Fatal(err)
	}
	profile := execution.Profile{ID: "reader", Kind: "model", Skills: []execution.Skill{{ID: "audit", Version: "1", Instructions: "Check the assumptions."}},
		Limits: execution.Limits{TimeoutSeconds: 5, MaxSteps: 2, MaxToolCalls: 0, MaxOutputTokens: 512, MaxOutputBytes: 1 << 20},
		Model:  &execution.ModelConfig{Protocol: "chat_completions", BaseURL: base, AllowLoopbackHTTP: true, Model: "test-model", TokenEnv: "MODEL_TOKEN"}}
	return Options{Config: Config{Listen: "127.0.0.1:4187", DataDir: filepath.Join(dir, "state"), WebDir: web, MaxParallel: 1,
		Workspaces: []Workspace{{ID: "source", Label: "Source", Path: workspace}}},
		Profiles: map[string]execution.Profile{"reader": profile}, Labels: map[string]string{"reader": "Reader"},
		Lookup: func(key string) (string, bool) {
			if key == "MODEL_TOKEN" || key == "GITHUB_TOKEN" {
				return "model-test-secret", true
			}
			return "", false
		}, Factory: execution.Build}
}
func serviceFor(t *testing.T, o Options) *Service {
	t.Helper()
	s, err := NewService(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func stateOf(t *testing.T, s *Service) View {
	t.Helper()
	v, err := s.Store.Read()
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func act(t *testing.T, s *Service, a Action) View {
	t.Helper()
	a.ExpectedRevision = stateOf(t, s).Revision
	a.RequestID = identifier("cmd")
	if err := s.Act(a); err != nil {
		t.Fatal(err)
	}
	return stateOf(t, s)
}
func studyFor(t *testing.T, s *Service) View {
	return act(t, s, Action{Type: "CREATE_STUDY", Title: "Тестовая гипотеза", Statement: "Проверяемое утверждение", Assumptions: "Явные условия"})
}
func waitAttempt(t *testing.T, s *Service, id string, match func(Attempt) bool) Attempt {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		v := stateOf(t, s)
		if a := v.attempt(id); a != nil && match(*a) {
			return *a
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("attempt did not reach expected state: %+v", stateOf(t, s).Attempts)
	return Attempt{}
}
func completeModel(w http.ResponseWriter, content string) {
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
		"message": map[string]string{"role": "assistant", "content": content}, "finish_reason": "stop"}},
		"usage": map[string]int{"prompt_tokens": 7, "completion_tokens": 4}})
}

func TestServerBDD_CommandsPersistAndRejectStaleOrDuplicateWrites(t *testing.T) {
	o := optionsFor(t, "http://127.0.0.1:1/v1")
	s := serviceFor(t, o)
	a := Action{Type: "CREATE_STUDY", ExpectedRevision: 1, RequestID: "command-0001", Title: "Hypothesis", Statement: "Statement", Assumptions: "Conditions"}
	if err := s.Act(a); err != nil {
		t.Fatal(err)
	}
	if err := s.Act(a); err != nil {
		t.Fatal(err)
	}
	v := stateOf(t, s)
	if v.Revision != 2 || len(v.Studies) != 1 {
		t.Fatal("duplicate mutation")
	}
	a.Title = "different"
	if !errors.Is(s.Act(a), ErrConflict) {
		t.Fatal("request id accepted another payload")
	}
	a.RequestID = "command-0002"
	if !errors.Is(s.Act(a), ErrConflict) {
		t.Fatal("stale write accepted")
	}
	if _, err := OpenStore(o.Config.DataDir); err == nil {
		t.Fatal("two processes may own one state directory")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := serviceFor(t, o)
	v = stateOf(t, reopened)
	if len(v.Studies) != 1 || v.Revision != 2 || len(v.History) != 1 {
		t.Fatalf("%+v", v)
	}
	past, err := reopened.Store.Snapshot(2)
	if err != nil || past.Studies[0].Title != "Hypothesis" {
		t.Fatal("historical snapshot lost")
	}
}

func TestServerBDD_SplittingAndQuestionsDoNotProveClaims(t *testing.T) {
	s := serviceFor(t, optionsFor(t, "http://127.0.0.1:1/v1"))
	v := studyFor(t, s)
	target := v.Studies[0].Goal
	v = act(t, s, Action{Type: "SPLIT", Target: target, Parts: []string{"Case one", "Case two"}})
	if len(v.entity(target).Dependencies) != 3 || v.entity(target).Status != "open" || len(v.Entities) != 4 {
		t.Fatal("split obligations missing")
	}
	v = act(t, s, Action{Type: "QUESTION", Target: target, Text: "Which assumptions remain?"})
	if v.Questions[0].Answer != "" || len(v.Tasks) != 1 || v.Tasks[0].Kind != "answer" {
		t.Fatal("fabricated answer or missing task")
	}
	err := s.Act(Action{Type: "REVIEW", Target: target, Decision: "accept", Text: "Trust me", ExpectedRevision: v.Revision, RequestID: identifier("cmd")})
	if err == nil {
		t.Fatal("accepted missing proof")
	}
}

func TestServerBDD_ModelRunUsesSnapshotAndRequiresSeparateAcceptance(t *testing.T) {
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer model-test-secret" {
			t.Error("credential missing")
		}
		data, _ := io.ReadAll(r.Body)
		if !bytes.Contains(data, []byte("Check the assumptions.")) {
			t.Error("skill missing")
		}
		completeModel(w, "Candidate argument. model-test-secret")
	}))
	defer model.Close()
	o := optionsFor(t, model.URL+"/v1")
	s := serviceFor(t, o)
	v := studyFor(t, s)
	target := v.Studies[0].Goal
	if err := os.WriteFile(filepath.Join(o.Config.Workspaces[0].Path, ".env.private"), []byte("unpublished-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	v = act(t, s, Action{Type: "TASK", Target: target, Title: "Find proof", Kind: "proof", Text: "Read the claim and produce a candidate proof."})
	r := RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[0].ID, Profile: "reader", Workspace: "source", Confirm: true}
	if err := s.Start(r); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(r); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	a := waitAttempt(t, s, v.Attempts[0].ID, func(a Attempt) bool { return !active(a.Status) })
	v = stateOf(t, s)
	if a.Status != "candidate" || calls.Load() != 1 || v.entity(target).Status != "open" {
		t.Fatalf("%+v calls=%d", a, calls.Load())
	}
	result, err := s.Result(a.ID)
	if err != nil || strings.Contains(result.Candidate, "model-test-secret") {
		t.Fatalf("%+v %v", result, err)
	}
	dir := filepath.Join(o.Config.DataDir, "attempts", a.ID, "workspace")
	if _, err := os.Stat(filepath.Join(dir, ".env.private")); !os.IsNotExist(err) {
		t.Fatal("copied an environment file")
	}
	if err := os.WriteFile(filepath.Join(o.Config.Workspaces[0].Path, "lemma.txt"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := os.ReadFile(filepath.Join(dir, "lemma.txt"))
	if string(snapshot) != "original statement" {
		t.Fatal("snapshot shares source files")
	}
	v = act(t, s, Action{Type: "ATTACH_PROOF", Target: target, Attempt: a.ID})
	if v.entity(target).Status != "in_review" || v.entity(target).ProofAuthor != "executor:reader" {
		t.Fatal("candidate bypassed review")
	}
	v = act(t, s, Action{Type: "REVIEW", Target: target, Decision: "accept", Text: "Проверены условия и переходы."})
	if v.entity(target).Status != "accepted" || v.entity(target).ReviewedBy != "operator" {
		t.Fatal("review was not recorded")
	}
	if err := os.WriteFile(filepath.Join(o.Config.DataDir, "attempts", a.ID, "result.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Result(a.ID); err == nil {
		t.Fatal("tampered result accepted")
	}
}

func TestServerBDD_CancellationAndRestartNeverReplayWork(t *testing.T) {
	entered := make(chan struct{}, 1)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		entered <- struct{}{}
		<-r.Context().Done()
	}))
	defer model.Close()
	o := optionsFor(t, model.URL+"/v1")
	s := serviceFor(t, o)
	v := studyFor(t, s)
	v = act(t, s, Action{Type: "TASK", Target: v.Studies[0].Goal, Title: "Long proof", Kind: "proof", Text: "Investigate"})
	if err := s.Start(RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[0].ID, Profile: "reader", Workspace: "source", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	id := stateOf(t, s).Attempts[0].ID
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("model did not start")
	}
	if err := s.Cancel(id); err != nil {
		t.Fatal(err)
	}
	a := waitAttempt(t, s, id, func(a Attempt) bool { return !active(a.Status) })
	result, err := s.Result(id)
	if err != nil || a.Status != "cancelled" || result.Candidate != "" {
		t.Fatalf("%+v %+v %v", a, result, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(o.Config.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Change(0, "", "", "crash fixture", "", "test", func(d *Data) error {
		d.Attempts[0].Status = "running"
		d.Attempts[0].RemoteOutcome = "unknown"
		d.Tasks[0].State = "running"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	store.Close()
	reopened := serviceFor(t, o)
	a = stateOf(t, reopened).Attempts[0]
	if a.Status != "interrupted" || a.RemoteOutcome != "unknown" {
		t.Fatal("unfinished attempt was not quarantined")
	}
	time.Sleep(200 * time.Millisecond)
	select {
	case <-entered:
		t.Fatal("work was replayed")
	default:
	}
}

func TestServerBDD_ChangedDependenciesBlockAcceptance(t *testing.T) {
	d := emptyData()
	lemma := newEntity("L", "Lemma", "lemma", "S", "P", "C")
	lemma.Status = "accepted"
	lemma.Proof = "proof"
	lemma.ProofAuthor = "executor:test"
	claim := newEntity("H", "Claim", "goal", "S", "Q", "C")
	claim.Status = "in_review"
	claim.Proof = "proof"
	claim.ProofAuthor = "executor:test"
	claim.Dependencies = []string{"L"}
	claim.DependencyRevisions = map[string]int{"L": 1}
	d.Entities = []Entity{lemma, claim}
	d.Studies = []Study{{ID: "S", Goal: "H"}}
	d.entity("L").Revision++
	if d.effective("H", map[string]bool{}) != "blocked" {
		t.Fatal("stale proof remains valid")
	}
	if applyAction(&d, Action{Type: "REVIEW", Target: "H", Decision: "accept", Text: "Checked"}) == nil {
		t.Fatal("accepted stale dependency")
	}
	d.entity("H").DependencyRevisions = nil
	d.entity("H").Status = "open"
	if err := applyAction(&d, Action{Type: "APPLY", Target: "H", Lemma: "L"}); err != nil {
		t.Fatal(err)
	}
	app := d.Applications[0]
	d.entity(app.ID).Status = "accepted"
	d.entity("L").Revision++
	if d.effective(app.ID, map[string]bool{}) != "blocked" {
		t.Fatal("stale application remains valid")
	}
}

func TestServerBDD_UnsafeWorkspacesAndUnapprovedRunsAreRejected(t *testing.T) {
	source := t.TempDir()
	if err := os.Symlink("/etc/passwd", filepath.Join(source, "outside")); err != nil {
		t.Fatal(err)
	}
	if _, err := copyWorkspace(context.Background(), source, filepath.Join(t.TempDir(), "copy")); err == nil {
		t.Fatal("symlink copied")
	}
	source = t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(source, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := copyWorkspace(context.Background(), source, filepath.Join(t.TempDir(), "copy")); err == nil {
		t.Fatal("FIFO copied")
	}
	s := serviceFor(t, optionsFor(t, "http://127.0.0.1:1/v1"))
	v := studyFor(t, s)
	v = act(t, s, Action{Type: "TASK", Target: v.Studies[0].Goal, Title: "Proof", Kind: "proof", Text: "Investigate"})
	r := RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[0].ID, Profile: "reader", Workspace: "source"}
	if s.Start(r) == nil {
		t.Fatal("run without consent")
	}
	r.Confirm = true
	r.Workspace = "/etc"
	if s.Start(r) == nil {
		t.Fatal("arbitrary workspace accepted")
	}
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(t.TempDir(), "target"), filepath.Join(dir, "research.db")); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(dir); err == nil {
		t.Fatal("database symlink accepted")
	}
}

func TestServerBDD_HTTPAuthenticationCSRFAndNoSecretDisclosure(t *testing.T) {
	s := serviceFor(t, optionsFor(t, "http://127.0.0.1:1/v1"))
	key := strings.Repeat("a", 64)
	h, err := NewHTTP(s, key)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	request := func(method, path string, body any, cookie *http.Cookie, csrf, origin, bearer string) *httptest.ResponseRecorder {
		t.Helper()
		data, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "http://127.0.0.1:4187"+path, bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if csrf != "" {
			r.Header.Set("X-Research-CSRF", csrf)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/api/bootstrap", nil, nil, "", "", ""); w.Code != 401 {
		t.Fatalf("%d", w.Code)
	}
	for _, value := range []string{key, " \t" + key + "\r\n"} {
		if w := request("POST", "/api/session", map[string]string{"token": value}, nil, "", "http://127.0.0.1:4187", ""); w.Code != 200 {
			t.Fatal("key with surrounding whitespace rejected")
		}
	}
	for _, value := range []string{"wrong-key", key[:32] + " " + key[32:]} {
		w := request("POST", "/api/session", map[string]string{"token": value}, nil, "", "http://127.0.0.1:4187", "")
		if w.Code != 401 || strings.TrimSpace(w.Body.String()) != "Неверный ключ." || len(w.Result().Cookies()) != 0 {
			t.Fatal("invalid key accepted or disclosed")
		}
	}
	login := request("POST", "/api/session", map[string]string{"token": key}, nil, "", "http://127.0.0.1:4187", "")
	if login.Code != 200 {
		t.Fatal(login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe cookie")
	}
	var session map[string]string
	json.Unmarshal(login.Body.Bytes(), &session)
	a := Action{Type: "CREATE_STUDY", ExpectedRevision: 1, RequestID: "browser-cmd-1", Title: "Title", Statement: "Claim", Assumptions: "Conditions"}
	if w := request("POST", "/api/actions", a, cookie, "", "http://127.0.0.1:4187", ""); w.Code != 403 {
		t.Fatal("missing CSRF accepted")
	}
	if w := request("POST", "/api/actions", a, cookie, session["csrf"], "https://evil.example", ""); w.Code != 403 {
		t.Fatal("foreign origin accepted")
	}
	if w := request("POST", "/api/actions", a, cookie, session["csrf"], "http://127.0.0.1:4187", ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w := request("GET", "/api/bootstrap", nil, cookie, "", "", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "model-test-secret") || strings.Contains(w.Body.String(), "token_env") || strings.Contains(w.Body.String(), "base_url") {
		t.Fatal("profile secrets exposed")
	}
	var bootstrap map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &bootstrap); err != nil || bootstrap["state"] == nil {
		t.Fatal("JSON schema damaged")
	}
	for _, path := range []string{"/access-token", "/.research-server/research.db", "/../access-token"} {
		if w := request("GET", path, nil, nil, "", "", ""); w.Code == 200 {
			t.Fatalf("served private file %s", path)
		}
	}
	r := httptest.NewRequest("GET", "http://evil.example/api/bootstrap", nil)
	r.Header.Set("Authorization", "Bearer "+key)
	rebinding := httptest.NewRecorder()
	h.ServeHTTP(rebinding, r)
	if rebinding.Code != 403 {
		t.Fatal("foreign Host accepted")
	}
	malformed := map[string]any{"type": "CREATE_STUDY", "expected_revision": 2, "request_id": "browser-cmd-2", "token": "do-not-echo"}
	if w := request("POST", "/api/actions", malformed, nil, "", "", ""+key); w.Code != 400 || strings.Contains(w.Body.String(), "do-not-echo") {
		t.Fatal("unknown secret field accepted or echoed")
	}
}

func TestServerBDD_CoddyPreparationDoesNotPublishAndNeedsExplicitConsent(t *testing.T) {
	var calls atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); http.Error(w, "unexpected", 500) }))
	defer remote.Close()
	o := optionsFor(t, "http://127.0.0.1:1/v1")
	o.Coddy = &coddy.Config{APIURL: remote.URL, AllowLoopbackHTTP: true, APIVersion: "2026-03-10", Repository: "org/lab", BotLogin: "coddy-bot", RequesterLogin: "research-owner", BaseBranch: "main", TokenEnv: "GITHUB_TOKEN", CoddyRevision: coddy.SourceRevision, CoddyPatchset: "review-loop-v1", TimeoutSeconds: 2, MaxPages: 2}
	s := serviceFor(t, o)
	v := studyFor(t, s)
	v = act(t, s, Action{Type: "TASK", Target: v.Studies[0].Goal, Title: "Implement checker", Kind: "proof", Text: "Check small cases"})
	r := PrepareCoddy{ExpectedRevision: v.Revision, RequestID: "prepare-coddy-001", TaskID: v.Tasks[0].ID, SourceCommit: strings.Repeat("1", 40), Acceptance: []string{"Reproducible test"}, Profile: "reader"}
	if err := s.PrepareCoddy(r); err != nil {
		t.Fatal(err)
	}
	if err := s.PrepareCoddy(r); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, s)
	if calls.Load() != 0 || len(v.Delegations) != 1 {
		t.Fatal("prepare performed a remote action")
	}
	id := v.Delegations[0].ID
	if _, err := s.CoddyCommand(context.Background(), id, CoddyCommand{Kind: "submit", RequestID: "submit-coddy-001"}); !errors.Is(err, coddy.ErrApproval) {
		t.Fatalf("approval: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("unapproved operation contacted GitHub")
	}
	if _, err := s.CoddyCommand(context.Background(), "unknown", CoddyCommand{Kind: "observe", RequestID: "observe-coddy-001"}); err == nil {
		t.Fatal("foreign delegation accepted")
	}
}
