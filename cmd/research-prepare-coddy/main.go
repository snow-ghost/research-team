// Prepares a private profile from an explicitly selected local Coddy installation.
// It performs no model requests and does not modify the source installation.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/researchweb"
	"gopkg.in/yaml.v3"
)

type localConfig struct {
	Agent     struct{ Model string } `yaml:"agent"`
	Providers []struct {
		Name string `yaml:"name"`
		Type string `yaml:"type"`
		Base string `yaml:"api_base"`
		Key  string `yaml:"api_key"`
	} `yaml:"providers"`
}

func main() {
	home, _ := os.UserHomeDir()
	config := flag.String("coddy-config", filepath.Join(home, ".coddy", "config.yaml"), "trusted local Coddy config")
	binary := flag.String("binary", filepath.Join(home, ".local/bin/coddy"), "reviewed static Coddy binary")
	version := flag.String("version", "1.2.54", "exact reviewed Coddy version")
	image := flag.String("image", "", "local Docker image ID (sha256:...)")
	base := flag.String("server-config", "examples/server/postgres.json", "base server configuration")
	flag.Parse()
	path, err := prepareConfig(*config, *binary, *image, *base, *version)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Prepared private configuration:", path)
	fmt.Println("No model requests. Run research-preflight before starting the server.")
}

func prepareConfig(source, binary, image, base, version string) (string, error) {
	opts, err := researchweb.LoadOptions(base)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(source)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return "", errors.New("local Coddy configuration unavailable")
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return "", errors.New("cannot read local Coddy configuration")
	}
	var c localConfig
	if yaml.Unmarshal(data, &c) != nil {
		return "", errors.New("invalid local Coddy configuration")
	}
	key, endpoint, model, err := selectedCredential(c, filepath.Dir(source))
	if err != nil {
		return "", err
	}
	executable, err := filepath.Abs(binary)
	if err != nil {
		return "", errors.New("invalid binary path")
	}
	p := execution.Profile{
		ID: "coddy-first-research", Kind: "external",
		Skills: []execution.Skill{{ID: "proof-candidate", Version: "1", Instructions: "Read TASK.md and Goal.lean. Give a concise proof and a Lean candidate with the same statement. Check boundary cases. Do not claim compilation unless a checker actually ran. Do not install dependencies, create remote tasks, or publish. Return the complete candidate in the response for independent review."}},
		Limits: execution.Limits{TimeoutSeconds: 300, MaxSteps: 6, MaxOutputTokens: 4096, MaxOutputBytes: 4 << 20},
		External: &execution.ExternalConfig{Provider: "coddy-agent", Executable: executable, ExpectedVersion: version,
			Model: model, BaseURL: endpoint, SearchPath: "/usr/local/bin:/usr/bin:/bin",
			SecretEnv:         map[string]string{"OPENAI_API_KEY": "RESEARCH_CODDY_AGENT_TOKEN"},
			ExecutionBoundary: "docker", Container: &execution.ContainerConfig{
				Runtime: "/usr/bin/docker", Image: image, MemoryMB: 1024, CPUs: 2}},
	}
	if err := p.Validate(); err != nil {
		return "", err
	}
	dir := filepath.Join(opts.Config.DataDir, "coddy-launch")
	if err := os.Mkdir(dir, 0700); err != nil {
		return "", errors.New("private launch directory already exists or cannot be created; no files overwritten")
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(dir)
		}
	}()
	cfg := opts.Config
	cfg.SecretFile = filepath.Join(dir, "secrets.json")
	cfg.AllowExternalExecution, cfg.MaxParallel = true, 1
	// Only the explicitly prepared executor is enabled by this launch config.
	cfg.CoddyConfig = ""
	cfg.Profiles = []researchweb.ProfileFile{{File: filepath.Join(dir, "profile.json"), Label: "Coddy: первое исследование"}}
	root, _ := filepath.Abs(".")
	cfg.Workspaces = []researchweb.Workspace{{ID: "wide-kernel", Label: "Lean Pool: ядро матрицы",
		Path: filepath.Join(root, "examples/research/wide-kernel")}}
	for name, value := range map[string]any{
		"profile.json": p, "server.json": cfg, "secrets.json": map[string]string{"RESEARCH_CODDY_AGENT_TOKEN": key},
	} {
		body, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return "", errors.New("cannot serialize private configuration")
		}
		path := filepath.Join(dir, name)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return "", errors.New("cannot create private configuration")
		}
		_, writeErr := f.Write(append(body, '\n'))
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			return "", errors.New("cannot persist private configuration")
		}
	}
	path := filepath.Join(dir, "server.json")
	if _, err := researchweb.LoadOptions(path); err != nil {
		return "", err
	}
	ok = true
	return path, nil
}

func selectedCredential(c localConfig, home string) (string, string, string, error) {
	provider, model, ok := strings.Cut(c.Agent.Model, "/")
	if !ok || model == "" || !regexp.MustCompile("^[a-zA-Z0-9_-]+$").MatchString(provider) {
		return "", "", "", errors.New("select a named Coddy provider and model")
	}
	matches := 0
	key, endpoint := "", ""
	for _, p := range c.Providers {
		if p.Name != provider {
			continue
		}
		matches++
		if p.Type != "neuraldeep" || p.Key != "" {
			return "", "", "", errors.New("automatic import supports only stored NeuralDeep login without inline api_key")
		}
		endpoint = strings.TrimRight(p.Base, "/")
		if endpoint == "" {
			endpoint = "https://api.neuraldeep.ru/v1"
		}
		hubs := map[string]string{"https://api.neuraldeep.ru/v1": "https://hub.neuraldeep.ru",
			"https://api.neuraldeep.tech/v1": "https://hub.neuraldeep.tech"}
		hub, allowed := hubs[endpoint]
		if !allowed {
			return "", "", "", errors.New("unsupported NeuralDeep endpoint")
		}
		path := filepath.Join(home, "providers", provider, "neuraldeep-auth.json")
		stat, err := os.Lstat(path)
		if err != nil || !stat.Mode().IsRegular() || stat.Size() > 65536 {
			return "", "", "", errors.New("stored Coddy login unavailable")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", "", "", errors.New("cannot read stored Coddy login")
		}
		var auth struct {
			Key string `json:"api_key"`
			Hub string `json:"hub"`
		}
		if json.Unmarshal(data, &auth) != nil || strings.TrimRight(auth.Hub, "/") != hub ||
			auth.Key == "" || len(auth.Key) > 16384 || strings.ContainsAny(auth.Key, "\x00\r\n") {
			return "", "", "", errors.New("stored Coddy login is invalid or belongs to another endpoint")
		}
		key = auth.Key
	}
	if matches != 1 {
		return "", "", "", errors.New("selected provider must occur exactly once")
	}
	return key, endpoint, model, nil
}
