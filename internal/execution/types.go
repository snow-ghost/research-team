package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const maxPayload = 1 << 20

var (
	ErrLimit            = errors.New("execution limit reached")
	ErrProtocol         = errors.New("invalid executor response")
	ErrUnsupported      = errors.New("unsupported executor capability")
	ErrWorkflowRequired = errors.New("workflow connector required")
	envName             = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
)

// Profiles are trusted operator configuration, never model-generated commands.
type Profile struct {
	ID       string          `json:"id"`
	Kind     string          `json:"kind"`
	Skills   []Skill         `json:"skills,omitempty"`
	Limits   Limits          `json:"limits"`
	Model    *ModelConfig    `json:"model,omitempty"`
	External *ExternalConfig `json:"external,omitempty"`
}

type Skill struct {
	ID           string `json:"id"`
	Version      string `json:"version"`
	Instructions string `json:"instructions"`
}

type Limits struct {
	TimeoutSeconds  int `json:"timeout_seconds"`
	MaxSteps        int `json:"max_steps"`
	MaxToolCalls    int `json:"max_tool_calls"`
	MaxOutputTokens int `json:"max_output_tokens"`
	MaxOutputBytes  int `json:"max_output_bytes"`
}

type ModelConfig struct {
	Protocol          string   `json:"protocol"`
	BaseURL           string   `json:"base_url"`
	TokenEnv          string   `json:"token_env,omitempty"`
	Model             string   `json:"model"`
	AllowLoopbackHTTP bool     `json:"allow_loopback_http,omitempty"`
	TokenLimitField   string   `json:"token_limit_field,omitempty"`
	Tools             []string `json:"tools,omitempty"`
}

type ExternalConfig struct {
	Provider          string            `json:"provider"`
	Executable        string            `json:"executable"`
	ExpectedVersion   string            `json:"expected_version"`
	Model             string            `json:"model"`
	SearchPath        string            `json:"search_path"`
	SecretEnv         map[string]string `json:"secret_env,omitempty"`
	ExecutionBoundary string            `json:"execution_boundary"`
	BaseURL           string            `json:"base_url,omitempty"`
	AllowLoopbackHTTP bool              `json:"allow_loopback_http,omitempty"`
}

type Task struct {
	ID         string `json:"id"`
	AttemptID  string `json:"attempt_id"`
	Snapshot   string `json:"snapshot"`
	LeaseEpoch uint64 `json:"lease_epoch"`
	Workspace  string `json:"workspace"`
	Objective  string `json:"objective"`
	Context    string `json:"context,omitempty"`
}

type Event struct {
	Type string `json:"type"`
	Step int    `json:"step,omitempty"`
	Tool string `json:"tool,omitempty"`
}

type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

type Result struct {
	TaskID        string  `json:"task_id"`
	AttemptID     string  `json:"attempt_id"`
	Snapshot      string  `json:"snapshot"`
	LeaseEpoch    uint64  `json:"lease_epoch"`
	ProfileID     string  `json:"profile_id"`
	ProfileSHA256 string  `json:"profile_sha256"`
	PacketSHA256  string  `json:"packet_sha256"`
	Status        string  `json:"status"`
	Candidate     string  `json:"candidate,omitempty"`
	SessionID     string  `json:"session_id,omitempty"`
	Usage         *Usage  `json:"usage"`
	Events        []Event `json:"events"`
	// A stopped local process/request does not establish remote cancellation.
	RemoteOutcome string `json:"remote_outcome"`
}

type Executor interface {
	Run(context.Context, Task) (Result, error)
}

func (p Profile) Validate() error {
	if p.ID == "" || p.Limits.TimeoutSeconds < 1 || p.Limits.TimeoutSeconds > 86400 ||
		p.Limits.MaxOutputBytes < 1024 || p.Limits.MaxOutputBytes > 16*maxPayload {
		return errors.New("profile id, timeout (1..86400), output bytes (1024..16777216) required")
	}
	seen := map[string]bool{}
	for _, skill := range p.Skills {
		if skill.ID == "" || skill.Version == "" || skill.Instructions == "" || seen[skill.ID] {
			return errors.New("skills require distinct ids, versions and instructions")
		}
		seen[skill.ID] = true
	}
	switch p.Kind {
	case "model":
		if p.Model == nil || p.External != nil {
			return errors.New("model configuration required exclusively")
		}
		m := p.Model
		if m.Protocol != "chat_completions" {
			return ErrUnsupported
		}
		if m.Model == "" || (m.TokenEnv != "" && !envName.MatchString(m.TokenEnv)) {
			return errors.New("model id and valid token environment reference required")
		}
		if _, err := modelEndpoint(*m); err != nil {
			return err
		}
		if m.TokenLimitField != "" && m.TokenLimitField != "max_tokens" && m.TokenLimitField != "max_completion_tokens" {
			return ErrUnsupported
		}
		if p.Limits.MaxSteps < 1 || p.Limits.MaxSteps > 100 || p.Limits.MaxToolCalls < 0 ||
			p.Limits.MaxToolCalls > 100 || p.Limits.MaxOutputTokens < 1 || p.Limits.MaxOutputTokens > 1000000 {
			return errors.New("invalid model limits")
		}
		allowed := map[string]bool{}
		for _, name := range m.Tools {
			if name != "read_file" || allowed[name] {
				return ErrUnsupported
			}
			allowed[name] = true
		}
	case "external":
		if p.External == nil || p.Model != nil {
			return errors.New("external configuration required exclusively")
		}
		e := p.External
		if e.Provider == "coddy" {
			return fmt.Errorf("%w: %w: coddy-bot manages repository tasks; direct process execution is unavailable",
				ErrUnsupported, ErrWorkflowRequired)
		}
		if e.Provider != "codex" && e.Provider != "claude" && e.Provider != "opencode" && e.Provider != "coddy-agent" {
			return ErrUnsupported
		}
		if e.ExpectedVersion != supportedVersions[e.Provider] {
			return errors.New("external version requires a reviewed adapter revision")
		}
		if !filepath.IsAbs(e.Executable) || e.ExpectedVersion == "" || e.Model == "" || e.SearchPath == "" {
			return errors.New("absolute executable, pinned version, model and search path required")
		}
		for _, path := range filepath.SplitList(e.SearchPath) {
			if !filepath.IsAbs(path) {
				return errors.New("search path entries must be absolute")
			}
		}
		if e.ExecutionBoundary != "operator_managed" {
			return errors.New("external execution requires operator-managed isolation")
		}
		if e.Provider == "coddy-agent" {
			if strings.ContainsAny(e.BaseURL, "$\r\n\x00") {
				return errors.New("coddy-agent base_url must be a literal URL")
			}
			if _, err := modelEndpoint(ModelConfig{BaseURL: e.BaseURL, AllowLoopbackHTTP: e.AllowLoopbackHTTP}); err != nil {
				return err
			}
			if p.Limits.MaxSteps < 1 || p.Limits.MaxSteps > 100 ||
				p.Limits.MaxOutputTokens < 1 || p.Limits.MaxOutputTokens > 1000000 || p.Limits.MaxToolCalls != 0 {
				return errors.New("coddy-agent requires turn and per-response token limits; tool-call limits unsupported")
			}
			if strings.TrimSpace(e.Model) != e.Model || strings.ContainsAny(e.Model, "\r\n\x00$") {
				return errors.New("invalid coddy-agent model id")
			}
		} else if e.BaseURL != "" || e.AllowLoopbackHTTP || p.Limits.MaxSteps != 0 ||
			p.Limits.MaxToolCalls != 0 || p.Limits.MaxOutputTokens != 0 {
			return ErrUnsupported
		}
		for dest, source := range e.SecretEnv {
			if !envName.MatchString(dest) || !envName.MatchString(source) || !credentialName(dest) {
				return errors.New("invalid credential environment mapping")
			}
			if e.Provider == "coddy-agent" && dest != "OPENAI_API_KEY" {
				return errors.New("coddy-agent accepts only the OPENAI_API_KEY credential mapping")
			}
		}
	default:
		return ErrUnsupported
	}
	return nil
}

func credentialName(s string) bool {
	return s == "CODEX_API_KEY" || s == "OPENAI_API_KEY" || s == "ANTHROPIC_API_KEY"
}

func modelEndpoint(m ModelConfig) (string, error) {
	u, err := url.Parse(m.BaseURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", errors.New("invalid model base_url")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || !m.AllowLoopbackHTTP || ip == nil || !ip.IsLoopback() {
			return "", errors.New("HTTPS required; explicit loopback HTTP allowed for local servers")
		}
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/chat/completions"
	u.RawPath = ""
	return u.String(), nil
}

func Build(p Profile, lookup func(string) (string, bool)) (Executor, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if lookup == nil {
		return nil, errors.New("secret resolver required")
	}
	// Own a copy so caller mutations cannot alter an executor mid-attempt.
	data, _ := json.Marshal(p)
	var frozen Profile
	_ = json.Unmarshal(data, &frozen)
	p = frozen
	if p.Kind == "external" && p.External.Provider == "coddy-agent" {
		return &coddyAgentExecutor{profile: p, lookup: lookup}, nil
	}
	if p.Kind == "external" {
		return &externalExecutor{profile: p, lookup: lookup}, nil
	}
	return newModelExecutor(p, lookup)
}

func prepare(p Profile, t Task) (Result, string, error) {
	r := Result{TaskID: t.ID, AttemptID: t.AttemptID, Snapshot: t.Snapshot, LeaseEpoch: t.LeaseEpoch,
		ProfileID: p.ID, Status: "failed", RemoteOutcome: "not_started", Events: []Event{}}
	if t.ID == "" || t.AttemptID == "" || t.Snapshot == "" || t.LeaseEpoch == 0 ||
		t.Objective == "" || !filepath.IsAbs(t.Workspace) {
		return r, "", errors.New("task identity, snapshot, lease epoch, objective and absolute workspace required")
	}
	// The workspace path is local execution metadata, not model input.
	input := t
	input.Workspace = ""
	packet, err := json.Marshal(struct {
		Task   Task    `json:"task"`
		Skills []Skill `json:"skills"`
	}{input, p.Skills})
	if err != nil || len(packet) > maxPayload {
		return r, "", ErrLimit
	}
	hash := sha256.Sum256(packet)
	r.PacketSHA256 = hex.EncodeToString(hash[:])
	profileBytes, _ := json.Marshal(p)
	profileHash := sha256.Sum256(profileBytes)
	r.ProfileSHA256 = hex.EncodeToString(profileHash[:])
	return r, string(packet), nil
}

func finish(r *Result, err error) {
	if err == nil {
		r.Status = "candidate"
		r.RemoteOutcome = "response_received"
	} else {
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			r.Status = "timed_out"
		case errors.Is(err, context.Canceled):
			r.Status = "cancelled"
		case errors.Is(err, ErrLimit):
			r.Status = "limit_reached"
		default:
			r.Status = "failed"
		}
		r.Candidate = ""
	}
	r.Events = append(r.Events, Event{Type: r.Status})
}

func duration(p Profile) time.Duration { return time.Duration(p.Limits.TimeoutSeconds) * time.Second }

func secret(lookup func(string) (string, bool), key string) (string, error) {
	if key == "" {
		return "", nil
	}
	value, ok := lookup(key)
	if !ok || value == "" {
		return "", fmt.Errorf("configured secret is unavailable")
	}
	if strings.ContainsAny(value, "\r\n\x00") {
		return "", errors.New("invalid secret value")
	}
	return value, nil
}
