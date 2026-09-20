package researchweb

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/snow-ghost/research-team/internal/coddy"
	"github.com/snow-ghost/research-team/internal/execution"
)

type ProfileFile struct {
	File  string `json:"file"`
	Label string `json:"label"`
}
type Workspace struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Path  string `json:"path"`
}
type Config struct {
	Listen                 string        `json:"listen"`
	DataDir                string        `json:"data_dir"`
	WebDir                 string        `json:"web_dir"`
	TokenEnv               string        `json:"token_env,omitempty"`
	MaxParallel            int           `json:"max_parallel"`
	AllowExternalExecution bool          `json:"allow_external_execution,omitempty"`
	Profiles               []ProfileFile `json:"profiles"`
	Workspaces             []Workspace   `json:"workspaces"`
	CoddyConfig            string        `json:"coddy_config,omitempty"`
}
type Options struct {
	Config   Config
	Profiles map[string]execution.Profile
	Labels   map[string]string
	Coddy    *coddy.Config
	Lookup   func(string) (string, bool)
	Factory  func(execution.Profile, func(string) (string, bool)) (execution.Executor, error)
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
	return o, nil
}
