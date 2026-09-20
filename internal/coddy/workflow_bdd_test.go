//go:build linux

package coddy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type fakeGitHub struct {
	mu             sync.Mutex
	t              *testing.T
	server         *httptest.Server
	issue          *Issue
	pull           *Pull
	comments       []Comment
	prComments     []Comment
	reviews        []Review
	inline         []InlineComment
	files          []ChangedFile
	writes         int
	reads          int
	failAfterWrite bool
	actor          string
	baseSHA        string
	publicRepo     bool
	onFiles        func()
}

func fixture(t *testing.T) (*Connector, *Record, *fakeGitHub) {
	t.Helper()
	f := &fakeGitHub{t: t, comments: []Comment{}, prComments: []Comment{}, reviews: []Review{}, inline: []InlineComment{},
		files: []ChangedFile{}, actor: "alice", baseSHA: strings.Repeat("a", 40)}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	config := Config{APIURL: f.server.URL, APIVersion: "2026-03-10", Repository: "org/lab", BotLogin: "coddy-bot",
		RequesterLogin: "alice", BaseBranch: "main", TokenEnv: "GITHUB_TEST_TOKEN", CoddyRevision: SourceRevision,
		CoddyPatchset: "review-loop-v1", AllowLoopbackHTTP: true, TimeoutSeconds: 3, MaxPages: 2}
	store, err := OpenStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	connector, err := New(config, store, func(string) (string, bool) { return "private-test-token", true })
	if err != nil {
		t.Fatal(err)
	}
	record, err := connector.Prepare(Job{TaskID: "T-1", AttemptID: "A-1", InputSnapshot: "S-1", LeaseEpoch: 2,
		Title: "Генератор", Objective: "Проверить малые случаи.", SourceCommit: f.baseSHA, Acceptance: []string{"Воспроизводимые проверки"}})
	if err != nil {
		t.Fatal(err)
	}
	return connector, record, f
}
func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer private-test-token" {
		f.t.Error("missing auth")
	}
	if r.Header.Get("X-GitHub-Api-Version") != "2026-03-10" {
		f.t.Error("missing API version")
	}
	if r.Method == "GET" {
		f.reads++
	} else {
		f.writes++
	}
	reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	path := strings.TrimPrefix(r.URL.Path, "/repos/org/lab")
	if r.Method == "GET" {
		switch path {
		case "":
			reply(map[string]any{"full_name": "org/lab", "private": !f.publicRepo})
		case "/user":
			reply(User{Login: f.actor})
		case "/git/ref/heads/main":
			reply(map[string]any{"object": map[string]string{"sha": f.baseSHA}})
		case "/issues":
			values := []Issue{}
			if f.issue != nil {
				values = append(values, *f.issue)
			}
			reply(values)
		case "/issues/7":
			if f.issue == nil {
				w.WriteHeader(404)
				return
			}
			reply(f.issue)
		case "/issues/7/comments":
			reply(f.comments)
		case "/issues/8/comments":
			reply(f.prComments)
		case "/pulls":
			values := []Pull{}
			if f.pull != nil {
				values = append(values, *f.pull)
			}
			reply(values)
		case "/pulls/8":
			reply(f.pull)
		case "/pulls/8/reviews":
			reply(f.reviews)
		case "/pulls/8/comments":
			reply(f.inline)
		case "/pulls/8/files":
			reply(f.files)
			if f.onFiles != nil {
				f.onFiles()
			}
		default:
			f.t.Errorf("unexpected GET %s", r.URL)
			w.WriteHeader(404)
		}
		return
	}
	var response any
	switch path {
	case "/issues":
		var payload struct {
			Title, Body string
			Assignees   []string
		}
		if json.NewDecoder(r.Body).Decode(&payload) != nil {
			f.t.Error("invalid issue payload")
		}
		assignees := []User{}
		for _, v := range payload.Assignees {
			assignees = append(assignees, User{Login: v})
		}
		f.issue = &Issue{Number: 7, Title: payload.Title, Body: payload.Body, State: "open", User: User{Login: "alice"}, Assignees: assignees}
		response = f.issue
	case "/issues/7/comments", "/issues/8/comments":
		var payload struct{ Body string }
		_ = json.NewDecoder(r.Body).Decode(&payload)
		comment := Comment{ID: 1000 + int64(f.writes), Body: payload.Body, User: User{Login: "alice"}}
		for _, comments := range [][]Comment{f.comments, f.prComments} {
			for _, previous := range comments {
				if previous.ID >= comment.ID {
					comment.ID = previous.ID + 1
				}
			}
		}
		if path == "/issues/7/comments" {
			f.comments = append(f.comments, comment)
		} else {
			f.prComments = append(f.prComments, comment)
		}
		response = comment
	case "/pulls/8/reviews":
		var payload struct {
			Body     string
			CommitID string `json:"commit_id"`
			Event    string
			Comments []LineComment
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload.Event != "REQUEST_CHANGES" || payload.CommitID != f.pull.Head.SHA || len(payload.Comments) == 0 {
			f.t.Error("review not pinned or has no inline comments")
		}
		review := Review{ID: 300 + int64(f.writes), Body: payload.Body, CommitID: payload.CommitID, User: User{Login: "alice"}, State: "CHANGES_REQUESTED"}
		f.reviews = append(f.reviews, review)
		for _, comment := range payload.Comments {
			f.inline = append(f.inline, InlineComment{Comment: Comment{ID: 400 + int64(len(f.inline)), Body: comment.Body, User: User{Login: "alice"}},
				Path: comment.Path, Line: comment.Line, CommitID: payload.CommitID, ReviewID: review.ID})
		}
		response = review
	default:
		f.t.Errorf("unexpected write %s %s", r.Method, path)
		w.WriteHeader(404)
		return
	}
	if f.failAfterWrite {
		f.failAfterWrite = false
		w.WriteHeader(503)
		fmt.Fprint(w, "private-test-token")
		return
	}
	reply(response)
}
func (f *fakeGitHub) update(fn func()) { f.mu.Lock(); defer f.mu.Unlock(); fn() }
func (f *fakeGitHub) addPlan(pr bool) {
	f.update(func() {
		comment := Comment{ID: 99, Body: "Plan\nDoes this approach work for you?\nReply with **yes**", User: User{Login: "coddy-bot"}}
		if pr {
			comment.ID = 250
			comment.Body = "## Review response plan\nChanges\nReply with **yes**"
			f.prComments = append(f.prComments, comment)
		} else {
			f.comments = append(f.comments, comment)
		}
	})
}
func (f *fakeGitHub) addPull(r *Record) {
	f.update(func() {
		p := Pull{Number: 8, Title: "Generated changes", Body: "Report", State: "open", User: User{Login: "coddy-bot"}, ChangedFiles: 1}
		p.Head.Ref = branchName(r)
		p.Head.SHA = strings.Repeat("b", 40)
		p.Head.Repo.FullName = "org/lab"
		p.Base.Ref = "main"
		p.Base.SHA = f.baseSHA
		p.Base.Repo.FullName = "org/lab"
		f.pull = &p
		f.files = []ChangedFile{{SHA: strings.Repeat("c", 40), Filename: "generator.go", Status: "added", Patch: "+func main() {}"}}
	})
}
func mustSubmit(t *testing.T, c *Connector, r *Record) *Record {
	t.Helper()
	result, err := c.Submit(context.Background(), r.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func mustObserve(t *testing.T, c *Connector, r *Record) *Record {
	t.Helper()
	result, err := c.Observe(context.Background(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestWorkflowBDD_IssuePlanReviewAndCandidate(t *testing.T) {
	c, r, f := fixture(t)
	r = mustSubmit(t, c, r)
	if r.IssueNumber != 7 {
		t.Fatal("issue not bound")
	}
	f.addPlan(false)
	r = mustObserve(t, c, r)
	if r.Last.Phase != "plan_available" {
		t.Fatal(r.Last.Phase)
	}
	_, err := c.ApprovePlan(context.Background(), r.ID, "approve-1", "issue", r.Last.Digest, 99, true)
	if err != nil {
		t.Fatal(err)
	}
	f.addPull(r)
	r = mustObserve(t, c, r)
	review := ReviewRequest{Body: "Correct the boundary case", Comments: []LineComment{{Path: "generator.go", Line: 1, Side: "RIGHT", Body: "Handle n=1"}}}
	_, err = c.RequestChanges(context.Background(), r.ID, "review-1", r.Last.Digest, review, true)
	if err != nil {
		t.Fatal(err)
	}
	f.addPlan(true)
	r = mustObserve(t, c, r)
	_, err = c.ApprovePlan(context.Background(), r.ID, "approve-2", "pr", r.Last.Digest, 250, true)
	if err != nil {
		t.Fatal(err)
	}
	f.update(func() { f.pull.Head.SHA = strings.Repeat("d", 40) })
	r = mustObserve(t, c, r)
	r, err = c.Collect(context.Background(), r.ID, r.Last.Digest)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Candidates) != 1 {
		t.Fatal("candidate absent")
	}
	data, err := os.ReadFile(filepath.Join(c.store.dir, "candidate-"+r.Candidates[0]+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var candidate Candidate
	_ = json.Unmarshal(data, &candidate)
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != r.Candidates[0] {
		t.Fatal("candidate filename is not its content hash")
	}
	if candidate.Status != "candidate" || candidate.ChecksExecuted || candidate.FullSourceCollected ||
		candidate.SourceCompatibility != "unverified" || candidate.Job.LeaseEpoch != 2 ||
		candidate.Observation.View.Pull.Head.SHA != strings.Repeat("d", 40) {
		t.Fatalf("%+v", candidate)
	}
	if f.writes != 4 {
		t.Fatalf("unexpected writes %d", f.writes)
	}
}

func TestWorkflowBDD_LostCreateIsReconciledWithoutDuplicate(t *testing.T) {
	c, r, f := fixture(t)
	f.failAfterWrite = true
	r, err := c.Submit(context.Background(), r.ID, true)
	if !errors.Is(err, ErrUnknown) || strings.Contains(err.Error(), "private-test-token") {
		t.Fatalf("unsafe outcome %v", err)
	}
	if _, err = c.Submit(context.Background(), r.ID, true); !errors.Is(err, ErrUnknown) {
		t.Fatal(err)
	}
	if f.writes != 1 {
		t.Fatal("ambiguous write repeated")
	}
	restarted, err := New(c.config, c.store, c.lookup)
	if err != nil {
		t.Fatal(err)
	}
	r, err = restarted.Reconcile(context.Background(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.IssueNumber != 7 || r.Operations[0].State != "confirmed" {
		t.Fatalf("%+v", r)
	}
	_, err = restarted.Submit(context.Background(), r.ID, true)
	if err != nil || f.writes != 1 {
		t.Fatal("replayed submit duplicated", err)
	}
}

func TestWorkflowBDD_LostApprovalIsReconciled(t *testing.T) {
	c, r, f := fixture(t)
	r = mustSubmit(t, c, r)
	f.addPlan(false)
	r = mustObserve(t, c, r)
	expected := r.Last.Digest
	f.update(func() { f.failAfterWrite = true })
	_, err := c.ApprovePlan(context.Background(), r.ID, "approval", "issue", expected, 99, true)
	if !errors.Is(err, ErrUnknown) {
		t.Fatal(err)
	}
	_, err = c.Reconcile(context.Background(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.ApprovePlan(context.Background(), r.ID, "approval", "issue", expected, 99, true)
	if err != nil || f.writes != 2 {
		t.Fatal("approval duplicated", err)
	}
	_, err = c.ApprovePlan(context.Background(), r.ID, "approval", "issue", "different", 99, true)
	if !errors.Is(err, ErrConflict) {
		t.Fatal("request id reused for another command")
	}
}

func TestWorkflowBDD_ApprovalRequiredBeforeNetwork(t *testing.T) {
	c, r, f := fixture(t)
	_, err := c.Submit(context.Background(), r.ID, false)
	if !errors.Is(err, ErrApproval) || f.writes != 0 || f.reads != 0 {
		t.Fatal("publication without approval", err)
	}
}

func TestWorkflowBDD_ChangedPlanBlocksApproval(t *testing.T) {
	c, r, f := fixture(t)
	r = mustSubmit(t, c, r)
	f.addPlan(false)
	r = mustObserve(t, c, r)
	f.update(func() { f.comments[0].Body += " changed" })
	_, err := c.ApprovePlan(context.Background(), r.ID, "approval", "issue", r.Last.Digest, 99, true)
	if !errors.Is(err, ErrConflict) || f.writes != 1 {
		t.Fatal("changed plan approved", err)
	}
}

func TestWorkflowBDD_WrongActorAndSourceBlockSubmit(t *testing.T) {
	for _, condition := range []string{"actor", "source"} {
		t.Run(condition, func(t *testing.T) {
			c, r, f := fixture(t)
			if condition == "actor" {
				f.actor = "someone-else"
			} else {
				f.baseSHA = strings.Repeat("e", 40)
			}
			if _, err := c.Submit(context.Background(), r.ID, true); err == nil || f.writes != 0 {
				t.Fatal("preflight ignored")
			}
		})
	}
}

func TestWorkflowBDD_HoldDoesNotClaimRemoteCancellation(t *testing.T) {
	c, r, f := fixture(t)
	r = mustSubmit(t, c, r)
	r, err := c.Hold(r.ID, true)
	if err != nil || !r.Held {
		t.Fatal(err)
	}
	if f.writes != 1 {
		t.Fatal("hold changed GitHub")
	}
	f.addPlan(false)
	r = mustObserve(t, c, r)
	_, err = c.ApprovePlan(context.Background(), r.ID, "approval", "issue", r.Last.Digest, 99, true)
	if !errors.Is(err, ErrPaused) {
		t.Fatal(err)
	}
	_, err = c.Hold(r.ID, false)
	if err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowBDD_ForeignPullDoesNotBecomeCandidate(t *testing.T) {
	c, r, f := fixture(t)
	r = mustSubmit(t, c, r)
	f.addPull(r)
	f.update(func() { f.pull.Head.Repo.FullName = "other/repository" })
	r = mustObserve(t, c, r)
	if r.Last.View.Pull != nil {
		t.Fatal("foreign pull attached")
	}
	if _, err := c.Collect(context.Background(), r.ID, r.Last.Digest); err == nil {
		t.Fatal("foreign candidate accepted")
	}
}

func TestWorkflowBDD_ChangedHeadAndTruncatedFilesBlockCollection(t *testing.T) {
	for _, condition := range []string{"head", "files"} {
		t.Run(condition, func(t *testing.T) {
			c, r, f := fixture(t)
			r = mustSubmit(t, c, r)
			f.addPull(r)
			r = mustObserve(t, c, r)
			f.update(func() {
				if condition == "head" {
					f.onFiles = func() { f.pull.Head.SHA = strings.Repeat("e", 40) }
				} else {
					f.files = []ChangedFile{}
				}
			})
			result, err := c.Collect(context.Background(), r.ID, r.Last.Digest)
			if err == nil || len(result.Candidates) != 0 {
				t.Fatal("inconsistent candidate collected")
			}
		})
	}
}

func TestWorkflowBDD_OnlyInlineReviewsAreExecutableByCoddy(t *testing.T) {
	c, r, f := fixture(t)
	r = mustSubmit(t, c, r)
	f.addPull(r)
	r = mustObserve(t, c, r)
	_, err := c.RequestChanges(context.Background(), r.ID, "review", r.Last.Digest, ReviewRequest{Body: "General comment"}, true)
	if err == nil || f.writes != 1 {
		t.Fatal("review without actionable comments sent")
	}
}

func TestWorkflowBDD_PrepareIsIdempotentAndImmutable(t *testing.T) {
	c, r, f := fixture(t)
	second, err := c.Prepare(r.Job)
	if err != nil || second.ID != r.ID {
		t.Fatal(err)
	}
	job := r.Job
	job.Objective = "different"
	if _, err = c.Prepare(job); !errors.Is(err, ErrConflict) {
		t.Fatal("attempt overwritten", err)
	}
	if f.reads != 0 || f.writes != 0 {
		t.Fatal("prepare contacted GitHub")
	}
}

func TestStoreBDD_ExclusiveLockAndPrivateFiles(t *testing.T) {
	c, r, _ := fixture(t)
	unlock, err := c.store.lock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err = c.Hold(r.ID, true); err == nil {
		t.Fatal("concurrent writer accepted")
	}
	info, err := os.Stat(filepath.Join(c.store.dir, r.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("permissions: %v", info.Mode())
	}
	if _, err = c.Local("../invalid"); err == nil {
		t.Fatal("path traversal accepted")
	}
}

func TestGitHubBDD_RedirectDoesNotForwardCredential(t *testing.T) {
	c, _, f := fixture(t)
	var calls int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer redirect.Close()
	config := c.config
	config.APIURL = redirect.URL
	g, err := newGitHub(config, c.lookup)
	if err != nil {
		t.Fatal(err)
	}
	if err = g.verifyActor(context.Background()); err == nil || calls != 0 {
		t.Fatal("redirect followed")
	}
	if f.reads != 0 {
		t.Fatal("wrong fixture accessed")
	}
}

func TestGitHubBDD_PaginationLimitDoesNotMeanEmptyQueue(t *testing.T) {
	c, _, _ := fixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := make([]Issue, 100)
		_ = json.NewEncoder(w).Encode(values)
	}))
	defer server.Close()
	config := c.config
	config.APIURL = server.URL
	config.MaxPages = 1
	g, err := newGitHub(config, c.lookup)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = list[Issue](context.Background(), g, "/issues", nil); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
}

func TestWorkflowBDD_ReplyCannotImplicitlyApprovePlan(t *testing.T) {
	c, r, f := fixture(t)
	r = mustSubmit(t, c, r)
	f.addPlan(false)
	r = mustObserve(t, c, r)
	for _, body := range []string{"yes", "не подходит", "not good", "Да, но измените план"} {
		if _, err := c.Reply(context.Background(), r.ID, "reply", "issue", r.Last.Digest, body, true); err == nil {
			t.Fatalf("confirmation passed as reply: %s", body)
		}
	}
	if f.writes != 1 {
		t.Fatal("unsafe reply published")
	}
	_, err := c.Reply(context.Background(), r.ID, "reply", "issue", r.Last.Digest, "Нужно отдельно рассмотреть n=1.", true)
	if err != nil || f.writes != 2 {
		t.Fatal("neutral reply not delivered", err)
	}
}

func TestWorkflowBDD_UnresolvedWriteWithoutMatchStaysUnknown(t *testing.T) {
	c, r, f := fixture(t)
	f.failAfterWrite = true
	_, _ = c.Submit(context.Background(), r.ID, true)
	f.update(func() { f.issue = nil })
	_, err := c.Reconcile(context.Background(), r.ID)
	if !errors.Is(err, ErrUnknown) {
		t.Fatal(err)
	}
	_, err = c.Submit(context.Background(), r.ID, true)
	if !errors.Is(err, ErrUnknown) || f.writes != 1 {
		t.Fatal("ambiguous empty listing repeated write", err)
	}
}

func TestWorkflowBDD_AnotherRequestIDCannotRepeatSameApproval(t *testing.T) {
	c, r, f := fixture(t)
	r = mustSubmit(t, c, r)
	f.addPlan(false)
	r = mustObserve(t, c, r)
	_, err := c.ApprovePlan(context.Background(), r.ID, "first", "issue", r.Last.Digest, 99, true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.ApprovePlan(context.Background(), r.ID, "second", "issue", r.Last.Digest, 99, true)
	if !errors.Is(err, ErrConflict) || f.writes != 2 {
		t.Fatal("semantic duplicate published", err)
	}
}

func TestWorkflowBDD_PublicRepositoryRequiresExplicitPolicy(t *testing.T) {
	c, r, f := fixture(t)
	f.publicRepo = true
	if _, err := c.Submit(context.Background(), r.ID, true); err == nil || f.writes != 0 {
		t.Fatal("private data published to public repository")
	}
}

func TestWorkflowBDD_ClarificationRequiresExplicitProceed(t *testing.T) {
	c, r, f := fixture(t)
	r = mustSubmit(t, c, r)
	r = mustObserve(t, c, r)
	_, err := c.Reply(context.Background(), r.ID, "clarification", "issue", r.Last.Digest, "Рассмотреть также n=1.", true)
	if err != nil {
		t.Fatal(err)
	}
	f.update(func() {
		f.comments = append(f.comments, Comment{ID: 2000, User: User{Login: "coddy-bot"},
			Body: "Data is sufficient. Shall I proceed with implementation? Reply with **yes** / **go ahead** to start."})
	})
	r = mustObserve(t, c, r)
	_, err = c.ApprovePlan(context.Background(), r.ID, "proceed", "issue", r.Last.Digest, 2000, true)
	if err != nil || f.writes != 3 {
		t.Fatal("explicit continuation failed", err)
	}
}
