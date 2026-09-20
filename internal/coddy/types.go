package coddy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
)

const SourceRevision = "fc3b9491d76d5911aa78e51fc6494ddf3c21bb77"
const maxRecord = 32 << 20

var (
	ErrConflict  = errors.New("state or remote snapshot changed")
	ErrUnknown   = errors.New("remote outcome unknown; reconcile before another write")
	ErrApproval  = errors.New("explicit publication approval required")
	ErrPaused    = errors.New("delegation is held locally; Coddy may still be running")
	ErrProtocol  = errors.New("unexpected GitHub response")
	ErrLimit     = errors.New("connector data limit exceeded")
	idPattern    = regexp.MustCompile(`^[a-f0-9]{64}$`)
	repoPattern  = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9_.-]+$`)
	loginPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*(\[bot\])?$`)
	envPattern   = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
	affirmative  = regexp.MustCompile(`(?i)(^|[^\pL\pN_])(да|yes|устраивает|ок|ok|okay|go ahead|go|бери в работу|начинай|поехали|подходит|согласен|согласна|looks good|good|принято)($|[^\pL\pN_])`)
)

type Config struct {
	APIURL                string `json:"api_url"`
	APIVersion            string `json:"api_version"`
	Repository            string `json:"repository"`
	BotLogin              string `json:"bot_login"`
	RequesterLogin        string `json:"requester_login"`
	BaseBranch            string `json:"base_branch"`
	TokenEnv              string `json:"token_env"`
	CoddyRevision         string `json:"coddy_revision"`
	CoddyPatchset         string `json:"coddy_patchset"`
	AllowLoopbackHTTP     bool   `json:"allow_loopback_http,omitempty"`
	AllowPublicRepository bool   `json:"allow_public_repository,omitempty"`
	TimeoutSeconds        int    `json:"timeout_seconds"`
	MaxPages              int    `json:"max_pages"`
}

func (c Config) Validate() error {
	if !repoPattern.MatchString(c.Repository) || !loginPattern.MatchString(c.BotLogin) ||
		!loginPattern.MatchString(c.RequesterLogin) || strings.EqualFold(c.BotLogin, c.RequesterLogin) ||
		!envPattern.MatchString(c.TokenEnv) || c.CoddyRevision != SourceRevision || c.CoddyPatchset != "review-loop-v1" {
		return errors.New("repository, distinct logins, secret reference and reviewed Coddy revision required")
	}
	repoName := strings.Split(c.Repository, "/")[1]
	if repoName == "." || repoName == ".." {
		return errors.New("invalid repository name")
	}
	if c.BaseBranch == "" || strings.ContainsAny(c.BaseBranch, "?#\\ \t\r\n") || strings.Contains(c.BaseBranch, "..") ||
		c.TimeoutSeconds < 1 || c.TimeoutSeconds > 300 || c.MaxPages < 1 || c.MaxPages > 30 {
		return errors.New("invalid base branch or request limits")
	}
	if c.APIVersion != "2026-03-10" {
		return errors.New("unreviewed GitHub API version")
	}
	u, err := url.Parse(c.APIURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return errors.New("invalid GitHub API URL")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || !c.AllowLoopbackHTTP || ip == nil || !ip.IsLoopback() {
			return errors.New("HTTPS required except explicitly allowed loopback HTTP")
		}
	}
	return nil
}

type Job struct {
	TaskID        string            `json:"task_id"`
	AttemptID     string            `json:"attempt_id"`
	InputSnapshot string            `json:"input_snapshot"`
	LeaseEpoch    uint64            `json:"lease_epoch"`
	Title         string            `json:"title"`
	Objective     string            `json:"objective"`
	Context       string            `json:"context,omitempty"`
	SourceCommit  string            `json:"source_commit"`
	Acceptance    []string          `json:"acceptance"`
	Skills        []execution.Skill `json:"skills,omitempty"`
}

func (j Job) validate() error {
	if j.TaskID == "" || j.AttemptID == "" || j.InputSnapshot == "" || j.LeaseEpoch == 0 ||
		strings.TrimSpace(j.Title) == "" || strings.TrimSpace(j.Objective) == "" || len(j.Acceptance) == 0 ||
		!regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(j.SourceCommit) {
		return errors.New("pinned job identity, source commit, objective and acceptance required")
	}
	for _, v := range j.Acceptance {
		if strings.TrimSpace(v) == "" {
			return errors.New("empty acceptance condition")
		}
	}
	seen := map[string]bool{}
	for _, s := range j.Skills {
		if s.ID == "" || s.Version == "" || s.Instructions == "" || seen[s.ID] {
			return errors.New("invalid skill bundle")
		}
		seen[s.ID] = true
	}
	data, _ := json.Marshal(j)
	if len(data) > 48*1024 {
		return ErrLimit
	}
	return nil
}

type User struct {
	Login string `json:"login"`
}
type Issue struct {
	Number      int64           `json:"number"`
	Title       string          `json:"title"`
	Body        string          `json:"body"`
	State       string          `json:"state"`
	User        User            `json:"user"`
	Assignees   []User          `json:"assignees"`
	UpdatedAt   string          `json:"updated_at"`
	PullRequest json.RawMessage `json:"pull_request,omitempty"`
}
type Comment struct {
	ID        int64  `json:"id"`
	Body      string `json:"body"`
	User      User   `json:"user"`
	UpdatedAt string `json:"updated_at"`
}
type Branch struct {
	Ref  string `json:"ref"`
	SHA  string `json:"sha"`
	Repo struct {
		FullName string `json:"full_name"`
	} `json:"repo"`
}
type Pull struct {
	Number       int64  `json:"number"`
	Title        string `json:"title"`
	Body         string `json:"body"`
	State        string `json:"state"`
	Draft        bool   `json:"draft"`
	Merged       bool   `json:"merged"`
	User         User   `json:"user"`
	Head         Branch `json:"head"`
	Base         Branch `json:"base"`
	ChangedFiles int    `json:"changed_files"`
	UpdatedAt    string `json:"updated_at"`
}
type Review struct {
	ID       int64  `json:"id"`
	Body     string `json:"body"`
	State    string `json:"state"`
	User     User   `json:"user"`
	CommitID string `json:"commit_id"`
}
type ChangedFile struct {
	SHA              string `json:"sha"`
	Filename         string `json:"filename"`
	PreviousFilename string `json:"previous_filename,omitempty"`
	Status           string `json:"status"`
	Patch            string `json:"patch,omitempty"`
}
type LineComment struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Side string `json:"side"`
	Body string `json:"body"`
}
type ReviewRequest struct {
	Body     string        `json:"body"`
	Comments []LineComment `json:"comments"`
}
type InlineComment struct {
	Comment
	Path        string `json:"path"`
	Line        int    `json:"line"`
	CommitID    string `json:"commit_id"`
	ReviewID    int64  `json:"pull_request_review_id"`
	InReplyToID int64  `json:"in_reply_to_id,omitempty"`
}
type View struct {
	Issue          Issue           `json:"issue"`
	IssueComments  []Comment       `json:"issue_comments"`
	Pull           *Pull           `json:"pull,omitempty"`
	PullComments   []Comment       `json:"pull_comments,omitempty"`
	Reviews        []Review        `json:"reviews,omitempty"`
	InlineComments []InlineComment `json:"inline_comments,omitempty"`
}
type Observation struct {
	Digest     string    `json:"digest"`
	ObservedAt time.Time `json:"observed_at"`
	Phase      string    `json:"phase"`
	View       View      `json:"view"`
}
type Operation struct {
	RequestID   string          `json:"request_id"`
	Kind        string          `json:"kind"`
	Fingerprint string          `json:"fingerprint"`
	Target      int64           `json:"target,omitempty"`
	Payload     json.RawMessage `json:"payload"`
	State       string          `json:"state"`
	RemoteID    int64           `json:"remote_id,omitempty"`
}
type Event struct {
	Time      time.Time `json:"time"`
	Type      string    `json:"type"`
	RequestID string    `json:"request_id,omitempty"`
}
type Record struct {
	Schema       int          `json:"schema"`
	ID           string       `json:"id"`
	ConfigSHA256 string       `json:"config_sha256"`
	Job          Job          `json:"job"`
	CreatedAt    time.Time    `json:"created_at"`
	Revision     uint64       `json:"revision"`
	Held         bool         `json:"held"`
	IssueNumber  int64        `json:"issue_number,omitempty"`
	Operations   []Operation  `json:"operations"`
	Last         *Observation `json:"last_observation,omitempty"`
	Candidates   []string     `json:"candidate_sha256,omitempty"`
	Events       []Event      `json:"events"`
}
type Candidate struct {
	Status              string        `json:"status"`
	DelegationID        string        `json:"delegation_id"`
	Job                 Job           `json:"job"`
	Observation         Observation   `json:"observation"`
	Files               []ChangedFile `json:"files"`
	SourceCompatibility string        `json:"source_compatibility"`
	FullSourceCollected bool          `json:"full_source_collected"`
	ChecksExecuted      bool          `json:"checks_executed"`
}

func digest(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func issueTitle(r *Record) string { return "research-" + r.ID[:32] }
func issueBody(r *Record) string {
	data, _ := json.MarshalIndent(r.Job, "", "  ")
	return "Задание исследовательской команды. Результат требует отдельной приемки.\n\n" + r.Job.Title +
		"\n\n" + string(data) + "\n\n<!-- research-delegation:v1:" + r.ID + " -->"
}
func branchName(r *Record) string { return intString(r.IssueNumber) + "-" + issueTitle(r) }
func event(r *Record, kind, requestID string) {
	r.Events = append(r.Events, Event{Time: time.Now().UTC(), Type: kind, RequestID: requestID})
}
