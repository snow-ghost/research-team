package coddy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type Connector struct {
	config Config
	store  *Store
	lookup func(string) (string, bool)
}

func New(c Config, s *Store, lookup func(string) (string, bool)) (*Connector, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if s == nil || lookup == nil {
		return nil, errors.New("store and credential resolver required")
	}
	return &Connector{config: c, store: s, lookup: lookup}, nil
}

func (c *Connector) Prepare(j Job) (*Record, error) {
	if err := j.validate(); err != nil {
		return nil, err
	}
	unlock, err := c.store.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	id := digest([]string{strings.ToLower(c.config.Repository), j.AttemptID})
	r, err := c.store.Load(id)
	if err == nil {
		if digest(r.Job) != digest(j) || r.ConfigSHA256 != digest(c.config) {
			return nil, ErrConflict
		}
		return r, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	r = &Record{Schema: 1, ID: id, ConfigSHA256: digest(c.config), Job: j, CreatedAt: time.Now().UTC(),
		Operations: []Operation{}, Events: []Event{}}
	event(r, "prepared", "")
	return r, c.store.save(r)
}

func (c *Connector) Local(id string) (*Record, error) {
	r, err := c.store.Load(id)
	if err == nil && r.ConfigSHA256 != digest(c.config) {
		return nil, ErrConflict
	}
	return r, err
}

func (c *Connector) locked(id string, action func(*Record) error) (*Record, error) {
	unlock, err := c.store.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	r, err := c.Local(id)
	if err != nil {
		return nil, err
	}
	return r, action(r)
}

func (c *Connector) Hold(id string, held bool) (*Record, error) {
	return c.locked(id, func(r *Record) error {
		r.Held = held
		kind := "local_hold"
		if !held {
			kind = "local_resume"
		}
		event(r, kind, "")
		return c.store.save(r)
	})
}

func writable(r *Record, approved bool) error {
	if !approved {
		return ErrApproval
	}
	if r.Held {
		return ErrPaused
	}
	for _, op := range r.Operations {
		if op.State != "confirmed" {
			return ErrUnknown
		}
	}
	return nil
}

func replay(r *Record, requestID, fingerprint string) (bool, error) {
	if requestID == "" || len(requestID) > 128 {
		return false, errors.New("request id (1..128 bytes) required")
	}
	for _, op := range r.Operations {
		if op.RequestID != requestID {
			if op.Fingerprint == fingerprint {
				return true, ErrConflict
			}
			continue
		}
		if op.Fingerprint != fingerprint {
			return true, ErrConflict
		}
		if op.State != "confirmed" {
			return true, ErrUnknown
		}
		return true, nil
	}
	return false, nil
}

func (c *Connector) Submit(ctx context.Context, id string, approved bool) (*Record, error) {
	return c.locked(id, func(r *Record) error {
		fingerprint := digest([]string{"submit", r.ID})
		if done, err := replay(r, "submit", fingerprint); done || err != nil {
			return err
		}
		if err := writable(r, approved); err != nil {
			return err
		}
		g, err := newGitHub(c.config, c.lookup)
		if err != nil {
			return err
		}
		if err = g.verifyActor(ctx); err != nil {
			return err
		}
		var repository struct {
			FullName string `json:"full_name"`
			Private  bool   `json:"private"`
		}
		if err = g.request(ctx, http.MethodGet, g.path(""), nil, &repository); err != nil {
			return err
		}
		if !strings.EqualFold(repository.FullName, c.config.Repository) {
			return ErrProtocol
		}
		if !repository.Private && !c.config.AllowPublicRepository {
			return errors.New("public repository publication is not enabled")
		}
		var ref struct {
			Object struct {
				SHA string `json:"sha"`
			} `json:"object"`
		}
		if err = g.request(ctx, http.MethodGet, g.path("/git/ref/heads/")+url.PathEscape(c.config.BaseBranch), nil, &ref); err != nil {
			return err
		}
		if ref.Object.SHA != r.Job.SourceCommit {
			return errors.New("base branch no longer matches the pinned source commit")
		}
		payload := map[string]any{"title": issueTitle(r), "body": issueBody(r), "assignees": []string{c.config.BotLogin}}
		return c.mutate(ctx, g, r, "submit", "issue", fingerprint, 0, payload)
	})
}

func operationPath(g *github, op Operation) (string, error) {
	switch op.Kind {
	case "issue":
		return g.path("/issues"), nil
	case "comment":
		return g.path("/issues/" + intString(op.Target) + "/comments"), nil
	case "review":
		return g.path("/pulls/" + intString(op.Target) + "/reviews"), nil
	default:
		return "", ErrProtocol
	}
}

func (c *Connector) mutate(ctx context.Context, g *github, r *Record, requestID, kind, fingerprint string, target int64, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil || len(data) > 64*1024 {
		return ErrLimit
	}
	op := Operation{RequestID: requestID, Kind: kind, Fingerprint: fingerprint, Target: target, Payload: data, State: "unknown"}
	path, err := operationPath(g, op)
	if err != nil {
		return err
	}
	r.Operations = append(r.Operations, op)
	index := len(r.Operations) - 1
	event(r, "remote_write_intent", requestID)
	// Durable intent precedes a non-idempotent remote write. No automatic retry.
	if err = c.store.save(r); err != nil {
		return err
	}
	var response json.RawMessage
	if err = g.request(ctx, http.MethodPost, path, op.Payload, &response); err != nil {
		return fmt.Errorf("%w: %w", ErrUnknown, err)
	}
	if err = c.confirm(r, index, response); err != nil {
		return fmt.Errorf("%w: %w", ErrUnknown, err)
	}
	event(r, "remote_write_confirmed", requestID)
	return c.store.save(r)
}

func (c *Connector) confirm(r *Record, index int, response json.RawMessage) error {
	op := &r.Operations[index]
	var wanted struct {
		Body     string `json:"body"`
		CommitID string `json:"commit_id"`
	}
	if json.Unmarshal(op.Payload, &wanted) != nil {
		return ErrProtocol
	}
	var remoteID int64
	switch op.Kind {
	case "issue":
		var issue Issue
		if json.Unmarshal(response, &issue) != nil || !c.matchesIssue(r, issue) {
			return ErrProtocol
		}
		r.IssueNumber = issue.Number
		remoteID = issue.Number
	case "comment":
		var comment Comment
		if json.Unmarshal(response, &comment) != nil || comment.ID <= 0 || comment.Body != wanted.Body ||
			!strings.EqualFold(comment.User.Login, c.config.RequesterLogin) {
			return ErrProtocol
		}
		remoteID = comment.ID
	case "review":
		var review Review
		if json.Unmarshal(response, &review) != nil || review.ID <= 0 || review.Body != wanted.Body ||
			review.CommitID != wanted.CommitID || review.State != "CHANGES_REQUESTED" ||
			!strings.EqualFold(review.User.Login, c.config.RequesterLogin) {
			return ErrProtocol
		}
		remoteID = review.ID
	default:
		return ErrProtocol
	}
	op.State, op.RemoteID = "confirmed", remoteID
	return nil
}

func (c *Connector) matchesIssue(r *Record, i Issue) bool {
	return i.Number > 0 && (len(i.PullRequest) == 0 || string(i.PullRequest) == "null") && i.Title == issueTitle(r) && i.Body == issueBody(r) &&
		strings.EqualFold(i.User.Login, c.config.RequesterLogin)
}

func (c *Connector) Reconcile(ctx context.Context, id string) (*Record, error) {
	return c.locked(id, func(r *Record) error {
		var index = -1
		for i, op := range r.Operations {
			if op.State != "confirmed" {
				index = i
				break
			}
		}
		if index < 0 {
			return nil
		}
		g, err := newGitHub(c.config, c.lookup)
		if err != nil {
			return err
		}
		op := r.Operations[index]
		path, err := operationPath(g, op)
		if err != nil {
			return err
		}
		query := url.Values{}
		if op.Kind == "issue" {
			query.Set("state", "all")
			query.Set("creator", c.config.RequesterLogin)
			query.Set("since", r.CreatedAt.Add(-time.Minute).Format(time.RFC3339))
		}
		items, err := list[json.RawMessage](ctx, g, path, query)
		if err != nil {
			return err
		}
		var matches []json.RawMessage
		// Confirm against a copy; an unrelated list entry must not mutate state.
		for _, raw := range items {
			copyRecord := *r
			copyRecord.Operations = append([]Operation(nil), r.Operations...)
			if c.confirm(&copyRecord, index, raw) == nil {
				matches = append(matches, raw)
			}
		}
		if len(matches) == 0 {
			return ErrUnknown
		}
		if len(matches) > 1 {
			return errors.New("multiple remote matches; operator reconciliation required")
		}
		if err = c.confirm(r, index, matches[0]); err != nil {
			return err
		}
		event(r, "remote_write_reconciled", op.RequestID)
		return c.store.save(r)
	})
}

func (c *Connector) Observe(ctx context.Context, id string) (*Record, error) {
	return c.locked(id, func(r *Record) error {
		g, err := newGitHub(c.config, c.lookup)
		if err != nil {
			return err
		}
		observed, err := c.observe(ctx, g, r)
		if err != nil {
			return err
		}
		r.Last = &observed
		event(r, "remote_observed", "")
		return c.store.save(r)
	})
}

func (c *Connector) observe(ctx context.Context, g *github, r *Record) (Observation, error) {
	if r.IssueNumber <= 0 {
		return Observation{}, errors.New("submit or reconcile the issue first")
	}
	var view View
	path := g.path("/issues/" + intString(r.IssueNumber))
	if err := g.request(ctx, http.MethodGet, path, nil, &view.Issue); err != nil {
		return Observation{}, err
	}
	if !c.matchesIssue(r, view.Issue) || view.Issue.Number != r.IssueNumber {
		return Observation{}, ErrConflict
	}
	var err error
	view.IssueComments, err = list[Comment](ctx, g, path+"/comments", nil)
	if err != nil {
		return Observation{}, err
	}
	owner := strings.Split(c.config.Repository, "/")[0]
	pulls, err := list[Pull](ctx, g, g.path("/pulls"), url.Values{
		"state": {"all"}, "head": {owner + ":" + branchName(r)}, "base": {c.config.BaseBranch},
	})
	if err != nil {
		return Observation{}, err
	}
	var candidates []Pull
	for _, p := range pulls {
		if c.matchesPull(r, p) {
			candidates = append(candidates, p)
		}
	}
	if len(candidates) > 1 {
		return Observation{}, errors.New("multiple correlated pull requests")
	}
	if len(candidates) == 1 {
		number := candidates[0].Number
		var pull Pull
		if err = g.request(ctx, http.MethodGet, g.path("/pulls/"+intString(number)), nil, &pull); err != nil {
			return Observation{}, err
		}
		if !c.matchesPull(r, pull) || pull.Number != number {
			return Observation{}, ErrConflict
		}
		view.Pull = &pull
		view.PullComments, err = list[Comment](ctx, g, g.path("/issues/"+intString(number)+"/comments"), nil)
		if err != nil {
			return Observation{}, err
		}
		view.Reviews, err = list[Review](ctx, g, g.path("/pulls/"+intString(number)+"/reviews"), nil)
		if err != nil {
			return Observation{}, err
		}
		view.InlineComments, err = list[InlineComment](ctx, g, g.path("/pulls/"+intString(number)+"/comments"), nil)
		if err != nil {
			return Observation{}, err
		}
	}
	return Observation{Digest: digest(view), ObservedAt: time.Now().UTC(), Phase: c.phase(view), View: view}, nil
}

func (c *Connector) matchesPull(r *Record, p Pull) bool {
	return p.Number > 0 && p.Head.Ref == branchName(r) && p.Base.Ref == c.config.BaseBranch &&
		strings.EqualFold(p.Head.Repo.FullName, c.config.Repository) &&
		strings.EqualFold(p.Base.Repo.FullName, c.config.Repository) &&
		strings.EqualFold(p.User.Login, c.config.BotLogin) &&
		regexpSHA(p.Head.SHA) && regexpSHA(p.Base.SHA)
}
func regexpSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, ch := range value {
		if !(ch >= '0' && ch <= '9') && !(ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return true
}

func latestComment(comments []Comment) Comment {
	var latest Comment
	for _, comment := range comments {
		if comment.ID > latest.ID {
			latest = comment
		}
	}
	return latest
}
func (c *Connector) isPlan(comment Comment, pr bool) bool {
	if comment.ID <= 0 || !strings.EqualFold(comment.User.Login, c.config.BotLogin) {
		return false
	}
	if pr {
		return strings.HasPrefix(comment.Body, "## Review response plan") && strings.Contains(comment.Body, "Reply with **yes**")
	}
	return (strings.Contains(comment.Body, "Does this approach work for you?") ||
		strings.HasPrefix(comment.Body, "Data is sufficient. Shall I proceed with implementation?")) &&
		strings.Contains(comment.Body, "Reply with **yes**")
}
func (c *Connector) phase(v View) string {
	if p := v.Pull; p != nil {
		if p.Merged {
			return "pull_request_merged"
		}
		if p.State == "closed" {
			return "pull_request_closed"
		}
		if c.isPlan(latestComment(v.PullComments), true) {
			return "review_plan_available"
		}
		if p.Draft {
			return "pull_request_draft"
		}
		return "pull_request_open"
	}
	if v.Issue.State == "closed" {
		return "issue_closed"
	}
	assigned := false
	for _, a := range v.Issue.Assignees {
		if strings.EqualFold(a.Login, c.config.BotLogin) {
			assigned = true
		}
	}
	if !assigned {
		return "bot_not_assigned"
	}
	if c.isPlan(latestComment(v.IssueComments), false) {
		return "plan_available"
	}
	return "waiting_for_coddy"
}

func (c *Connector) ApprovePlan(ctx context.Context, id, requestID, target, expectedView string, commentID int64, approved bool) (*Record, error) {
	return c.locked(id, func(r *Record) error {
		if target != "issue" && target != "pr" {
			return errors.New("plan target must be issue or pr")
		}
		fingerprint := digest([]any{"approve", target, expectedView, commentID})
		if done, err := replay(r, requestID, fingerprint); done || err != nil {
			return err
		}
		if err := writable(r, approved); err != nil {
			return err
		}
		g, err := newGitHub(c.config, c.lookup)
		if err != nil {
			return err
		}
		if err = g.verifyActor(ctx); err != nil {
			return err
		}
		observation, err := c.observe(ctx, g, r)
		if err != nil {
			return err
		}
		if expectedView == "" || observation.Digest != expectedView {
			return ErrConflict
		}
		comments, number := observation.View.IssueComments, r.IssueNumber
		if target == "pr" {
			p := observation.View.Pull
			if p == nil || p.State != "open" || p.Merged {
				return errors.New("open correlated pull request required")
			}
			comments, number = observation.View.PullComments, p.Number
		} else if observation.View.Issue.State != "open" || observation.View.Pull != nil {
			return errors.New("open issue without a pull request required")
		}
		if target == "issue" {
			if c.phase(observation.View) != "plan_available" {
				return errors.New("assigned bot confirmation request required")
			}
			var ref struct {
				Object struct {
					SHA string `json:"sha"`
				} `json:"object"`
			}
			if err = g.request(ctx, http.MethodGet, g.path("/git/ref/heads/")+url.PathEscape(c.config.BaseBranch), nil, &ref); err != nil {
				return err
			}
			if ref.Object.SHA != r.Job.SourceCommit {
				return errors.New("base branch changed before plan approval")
			}
		}
		comment := latestComment(comments)
		if comment.ID != commentID || !c.isPlan(comment, target == "pr") {
			return errors.New("latest bot plan must be selected")
		}
		r.Last = &observation
		body := "yes\n\nApproved plan comment: " + intString(commentID) + "\nObserved snapshot: " + expectedView + "\n" + operationMarker(r, requestID)
		return c.mutate(ctx, g, r, requestID, "comment", fingerprint, number, map[string]string{"body": body})
	})
}

func operationMarker(r *Record, requestID string) string {
	return "<!-- research-operation:v1:" + digest([]string{r.ID, requestID}) + " -->"
}

func (c *Connector) Reply(ctx context.Context, id, requestID, target, expectedView, body string, approved bool) (*Record, error) {
	return c.locked(id, func(r *Record) error {
		if target != "issue" && target != "pr" {
			return errors.New("reply target must be issue or pr")
		}
		if strings.TrimSpace(body) == "" || len(body) > 48*1024 {
			return errors.New("reply text (1..49152 bytes) required")
		}
		// Coddy interprets affirmative words even inside a longer comment.
		if affirmative.MatchString(body) {
			return errors.New("reply contains a Coddy confirmation phrase; use explicit plan approval")
		}
		fingerprint := digest([]string{"reply", target, expectedView, body})
		if done, err := replay(r, requestID, fingerprint); done || err != nil {
			return err
		}
		if err := writable(r, approved); err != nil {
			return err
		}
		g, err := newGitHub(c.config, c.lookup)
		if err != nil {
			return err
		}
		if err = g.verifyActor(ctx); err != nil {
			return err
		}
		observation, err := c.observe(ctx, g, r)
		if err != nil {
			return err
		}
		if expectedView == "" || observation.Digest != expectedView {
			return ErrConflict
		}
		number := r.IssueNumber
		if target == "pr" {
			p := observation.View.Pull
			if p == nil || p.State != "open" || p.Merged {
				return errors.New("open correlated pull request required")
			}
			number = p.Number
		} else if observation.View.Issue.State != "open" {
			return errors.New("open issue required")
		}
		r.Last = &observation
		return c.mutate(ctx, g, r, requestID, "comment", fingerprint, number, map[string]string{"body": body + "\n\n" + operationMarker(r, requestID)})
	})
}

func (c *Connector) RequestChanges(ctx context.Context, id, requestID, expectedView string, review ReviewRequest, approved bool) (*Record, error) {
	return c.locked(id, func(r *Record) error {
		data, _ := json.Marshal(review)
		if strings.TrimSpace(review.Body) == "" || len(data) > 48*1024 || len(review.Comments) < 1 || len(review.Comments) > 20 {
			return errors.New("review summary and 1..20 line comments required (up to 49152 bytes)")
		}
		for _, comment := range review.Comments {
			if comment.Path == "" || comment.Line < 1 || comment.Side != "RIGHT" || strings.TrimSpace(comment.Body) == "" {
				return errors.New("review comments require a path, positive line, RIGHT side and text")
			}
		}
		fingerprint := digest([]any{"request_changes", expectedView, review})
		if done, err := replay(r, requestID, fingerprint); done || err != nil {
			return err
		}
		if err := writable(r, approved); err != nil {
			return err
		}
		g, err := newGitHub(c.config, c.lookup)
		if err != nil {
			return err
		}
		if err = g.verifyActor(ctx); err != nil {
			return err
		}
		observation, err := c.observe(ctx, g, r)
		if err != nil {
			return err
		}
		if expectedView == "" || observation.Digest != expectedView {
			return ErrConflict
		}
		p := observation.View.Pull
		if p == nil || p.State != "open" || p.Merged || p.Draft {
			return errors.New("open non-draft pull request required")
		}
		files, err := list[ChangedFile](ctx, g, g.path("/pulls/"+intString(p.Number)+"/files"), nil)
		if err != nil {
			return err
		}
		if len(files) != p.ChangedFiles {
			return errors.New("incomplete changed-file list")
		}
		for _, comment := range review.Comments {
			found := false
			for _, file := range files {
				if file.Filename == comment.Path {
					found = true
				}
			}
			if !found {
				return errors.New("review path does not belong to the pull request")
			}
		}
		r.Last = &observation
		payload := map[string]any{"event": "REQUEST_CHANGES", "commit_id": p.Head.SHA, "body": review.Body + "\n\n" + operationMarker(r, requestID), "comments": review.Comments}
		return c.mutate(ctx, g, r, requestID, "review", fingerprint, p.Number, payload)
	})
}

func (c *Connector) Collect(ctx context.Context, id, expectedView string) (*Record, error) {
	return c.locked(id, func(r *Record) error {
		if r.Held {
			return ErrPaused
		}
		for _, op := range r.Operations {
			if op.State != "confirmed" {
				return ErrUnknown
			}
		}
		g, err := newGitHub(c.config, c.lookup)
		if err != nil {
			return err
		}
		observation, err := c.observe(ctx, g, r)
		if err != nil {
			return err
		}
		if expectedView == "" || observation.Digest != expectedView {
			return ErrConflict
		}
		p := observation.View.Pull
		if p == nil || p.Draft || p.ChangedFiles < 1 {
			return errors.New("non-draft pull request with changes required")
		}
		files, err := list[ChangedFile](ctx, g, g.path("/pulls/"+intString(p.Number)+"/files"), nil)
		if err != nil {
			return err
		}
		if len(files) != p.ChangedFiles {
			return errors.New("incomplete changed-file list")
		}
		seen := map[string]bool{}
		for _, file := range files {
			if file.Filename == "" || seen[file.Filename] || !regexpSHA(file.SHA) {
				return ErrProtocol
			}
			seen[file.Filename] = true
		}
		after, err := c.observe(ctx, g, r)
		if err != nil {
			return err
		}
		if after.Digest != observation.Digest {
			return ErrConflict
		}
		candidate := Candidate{Status: "candidate", DelegationID: r.ID, Job: r.Job,
			Observation: observation, Files: files, SourceCompatibility: "unverified"}
		hash, err := c.store.saveCandidate(candidate)
		if err != nil {
			return err
		}
		r.Candidates = append(r.Candidates, hash)
		r.Last = &after
		event(r, "candidate_collected", "")
		return c.store.save(r)
	})
}
