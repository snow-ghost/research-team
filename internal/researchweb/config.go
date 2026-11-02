package researchweb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/snow-ghost/research-team/internal/coddy"
	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
)

type ProfileFile struct {
	File  string `json:"file"`
	Label string `json:"label"`
}
type Workspace struct {
	Graph        *ComputationGraphConfig `json:"graph,omitempty"`
	ContextFiles []string                `json:"context_files,omitempty"`
	ID           string                  `json:"id"`
	Label        string                  `json:"label"`
	Path         string                  `json:"path"`
}
type Config struct {
	Evaluations            []EvaluationWatch `json:"evaluations,omitempty"`
	Workers                []WorkerConfig    `json:"workers,omitempty"`
	Telegram               []TelegramChannel `json:"telegram,omitempty"`
	LeanConfig             string            `json:"lean_config,omitempty"`
	LeanServiceURL         string            `json:"lean_service_url,omitempty"`
	LeanServiceTokenEnv    string            `json:"lean_service_token_env,omitempty"`
	Database               DatabaseConfig    `json:"database,omitempty"`
	Listen                 string            `json:"listen"`
	DataDir                string            `json:"data_dir"`
	WebDir                 string            `json:"web_dir"`
	TokenEnv               string            `json:"token_env,omitempty"`
	SecretFile             string            `json:"secret_file,omitempty"`
	MaxParallel            int               `json:"max_parallel"`
	AllowExternalExecution bool              `json:"allow_external_execution,omitempty"`
	Profiles               []ProfileFile     `json:"profiles"`
	Workspaces             []Workspace       `json:"workspaces"`
	CoddyConfig            string            `json:"coddy_config,omitempty"`
}
type DatabaseConfig struct {
	Driver string `json:"driver"`
	DSNEnv string `json:"dsn_env,omitempty"`
}
type Options struct {
	Preparer       leancheck.GoalPreparer
	StopEvaluation func(context.Context, string) error
	Matcher        leancheck.ModuleMatcher
	ChainChecker   leancheck.Checker
	TelegramClient TelegramClient
	Lean           *leancheck.Config
	Checker        leancheck.Checker
	Config         Config
	Profiles       map[string]execution.Profile
	Labels         map[string]string
	Coddy          *coddy.Config
	Lookup         func(string) (string, bool)
	Factory        func(execution.Profile, func(string) (string, bool)) (execution.Executor, error)
}

func ReadJSON(path string, value any) error {
	f, err := os.Open(path)
	if err != nil {
		return errors.New("configuration file unavailable")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return errors.New("configuration too large")
	}
	return decodeJSON(data, value)
}
func decodeJSON(data []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(value) != nil || d.Decode(new(any)) != io.EOF {
		return RuleError("Некорректный JSON или неизвестное поле.")
	}
	return nil
}
func LoadOptions(path string) (Options, error) {
	o := Options{Profiles: map[string]execution.Profile{}, Labels: map[string]string{}, Lookup: os.LookupEnv, Factory: execution.Build}
	if err := ReadJSON(path, &o.Config); err != nil {
		return o, err
	}
	base, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return o, err
	}
	resolve := func(p string) string {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(base, p)
	}
	c := &o.Config
	if err := validateWorkers(c.Workers); err != nil {
		return o, err
	}
	if err := validateTelegram(c.Telegram); err != nil {
		return o, err
	}
	if c.Database.Driver != "" && c.Database.Driver != "sqlite" && c.Database.Driver != "postgres" {
		return o, errors.New("unsupported database driver")
	}
	if c.Database.Driver == "postgres" && c.Database.DSNEnv == "" {
		return o, errors.New("postgres requires dsn_env")
	}
	host, port, err := net.SplitHostPort(c.Listen)
	p, nErr := strconv.Atoi(port)
	ip := net.ParseIP(host)
	if err != nil || nErr != nil || p < 0 || p > 65535 || ip == nil || !ip.IsLoopback() {
		return o, errors.New("listen must be a numeric loopback address and port")
	}
	if c.DataDir == "" || c.WebDir == "" || c.MaxParallel < 1 || c.MaxParallel > 4 || len(c.Profiles) > 20 || len(c.Workspaces) > 20 {
		return o, errors.New("invalid server directories or limits")
	}
	c.DataDir, c.WebDir = resolve(c.DataDir), resolve(c.WebDir)
	if err := resolveEvaluationWatches(c, base); err != nil {
		return o, err
	}
	if c.SecretFile != "" {
		c.SecretFile = resolve(c.SecretFile)
		values, err := readPrivateSecrets(c.SecretFile, c.WebDir)
		if err != nil {
			return o, err
		}
		o.Lookup = func(key string) (string, bool) {
			if value, ok := values[key]; ok {
				return value, true
			}
			return os.LookupEnv(key)
		}
	}
	relative, err := filepath.Rel(c.WebDir, c.DataDir)
	if err != nil || relative == "." || (!filepath.IsAbs(relative) && relative != ".." && !bytes.HasPrefix([]byte(relative), []byte(".."+string(filepath.Separator)))) {
		return o, errors.New("private state must be outside web directory")
	}
	for _, spec := range c.Profiles {
		var profile execution.Profile
		if err := ReadJSON(resolve(spec.File), &profile); err != nil {
			return o, err
		}
		if err := profile.Validate(); err != nil {
			return o, err
		}
		if _, exists := o.Profiles[profile.ID]; exists {
			return o, errors.New("duplicate profile")
		}
		o.Profiles[profile.ID] = profile
		o.Labels[profile.ID] = spec.Label
	}
	seen := map[string]bool{}
	for i := range c.Workspaces {
		w := &c.Workspaces[i]
		if w.ID == "" || w.Label == "" || w.Path == "" || seen[w.ID] {
			return o, errors.New("invalid workspace")
		}
		w.Path = resolve(w.Path)
		seen[w.ID] = true
		if len(w.ContextFiles) > 16 {
			return o, errors.New("too many workspace context files")
		}
		contextSeen := map[string]bool{}
		for _, name := range w.ContextFiles {
			if len(name) == 0 || len(name) > 64 || name[0] == '.' || filepath.Base(name) != name || strings.ContainsRune(name, '\\') || contextSeen[name] || name == "TASK.md" || name == "Goal.lean" || name == "Candidate.template.lean" {
				return o, errors.New("invalid workspace context file")
			}
			contextSeen[name] = true
		}
	}
	if c.CoddyConfig != "" {
		c.CoddyConfig = resolve(c.CoddyConfig)
		o.Coddy = &coddy.Config{}
		if err := ReadJSON(c.CoddyConfig, o.Coddy); err != nil {
			return o, err
		}
		if err := o.Coddy.Validate(); err != nil {
			return o, err
		}
	}
	if c.LeanConfig != "" {
		c.LeanConfig = resolve(c.LeanConfig)
		o.Lean = &leancheck.Config{}
		if err := ReadJSON(c.LeanConfig, o.Lean); err != nil {
			return o, err
		}
		if err := o.Lean.Validate(); err != nil {
			return o, err
		}
		o.Checker = leancheck.DockerChecker{Config: *o.Lean}
	}
	if c.LeanServiceURL != "" {
		if o.Lean == nil || c.LeanServiceTokenEnv == "" {
			return o, errors.New("lean service requires lean_config and lean_service_token_env")
		}
		endpoint, err := url.Parse(c.LeanServiceURL)
		if err != nil || endpoint.Scheme != "http" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Path != "" && endpoint.Path != "/" {
			return o, errors.New("lean service requires a loopback HTTP endpoint")
		}
		host := endpoint.Hostname()
		if host != "127.0.0.1" && host != "localhost" && host != "::1" && host != "host.docker.internal" {
			return o, errors.New("lean service requires a loopback HTTP endpoint")
		}
		token, ok := o.Lookup(c.LeanServiceTokenEnv)
		if !ok || token == "" {
			return o, errors.New("lean service token is unavailable")
		}
		checker := leancheck.NewHTTPChecker(c.LeanServiceURL, token)
		o.Checker, o.Matcher = checker, checker
	}
	return o, nil
}
