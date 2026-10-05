package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/snow-ghost/research-team/internal/researchweb"
)

type check struct {
	ID     string `json:"id"`
	Ready  bool   `json:"ready"`
	Detail string `json:"detail"`
}
type report struct {
	Ready       bool    `json:"ready"`
	Scope       string  `json:"scope"`
	ModelCalled bool    `json:"model_called"`
	Checks      []check `json:"checks"`
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, out, errOut io.Writer) int {
	flags := flag.NewFlagSet("research-preflight", flag.ContinueOnError)
	flags.SetOutput(errOut)
	config := flags.String("config", "examples/server/postgres.json", "Server configuration to inspect without starting it")
	binary := flags.String("coddy", "", "Installed Coddy to compare with the profile; defaults to PATH")
	if flags.Parse(args) != nil {
		return 2
	}
	options, err := researchweb.LoadOptions(*config)
	if err != nil {
		fmt.Fprintln(errOut, "Server configuration could not be validated; no executor was started.")
		return 2
	}
	if *binary == "" {
		*binary, _ = exec.LookPath("coddy")
	}
	r := inspect(options, *binary, probeVersion)
	if json.NewEncoder(out).Encode(r) != nil {
		return 1
	}
	if !r.Ready {
		return 2
	}
	return 0
}

func inspect(o researchweb.Options, binary string, probe func(string) (string, error)) report {
	r := report{Ready: true, Scope: "local_configuration_only", Checks: []check{}}
	add := func(id string, ok bool, detail string) {
		r.Checks = append(r.Checks, check{id, ok, detail})
		r.Ready = r.Ready && ok
	}
	ids := []string{}
	for id, p := range o.Profiles {
		if p.External != nil && p.External.Provider == "coddy-agent" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	add("coddy_profile", len(ids) > 0, "At least one Coddy Agent profile must be present.")
	if len(ids) == 0 {
		return r
	}
	version, err := probe(binary)
	add("installed_coddy", err == nil, "Only --version is executed, without account credentials.")
	for _, id := range ids {
		p := o.Profiles[id]
		e := p.External
		if err == nil {
			candidate := p
			external := *e
			external.ExpectedVersion = version
			candidate.External = &external
			add(id+":reviewed_version", candidate.Validate() == nil, "Installed Coddy: "+version+"; the adapter allowlist is unchanged.")
			add(id+":pinned_version", version == e.ExpectedVersion, "Installed and configured versions must match exactly.")
		}
		configured, configuredErr := filepath.EvalSymlinks(e.Executable)
		installed, installedErr := filepath.EvalSymlinks(binary)
		add(id+":configured_binary", configuredErr == nil && installedErr == nil && configured == installed,
			"The configured executable must be the inspected binary.")
		add(id+":provider_configuration", !strings.Contains(e.Model, "replace") && !strings.Contains(e.BaseURL, "provider.example"),
			"The model and API endpoint must be configured, not example placeholders; no network probe is made.")
		credentials := true
		for _, name := range e.SecretEnv {
			value, ok := o.Lookup(name)
			credentials = credentials && ok && strings.TrimSpace(value) != ""
		}
		add(id+":credentials_available", credentials,
			"Only credential resolver availability is checked; values are not printed, passed to the version probe or sent over the network.")
	}
	add("external_execution", o.Config.AllowExternalExecution, "External execution needs explicit operator configuration and isolation.")
	workspaces := len(o.Config.Workspaces) > 0
	for _, w := range o.Config.Workspaces {
		info, err := os.Stat(w.Path)
		workspaces = workspaces && err == nil && info != nil && info.IsDir()
	}
	add("materials_available", workspaces, "Configured material directories must exist; their content is not uploaded.")
	return r
}

type boundedOutput struct{ data []byte }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > 4096 {
		return 0, errors.New("version output exceeds limit")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

func probeVersion(binary string) (string, error) {
	if !filepath.IsAbs(binary) {
		return "", errors.New("absolute executable required")
	}
	home, err := os.MkdirTemp("", "research-preflight-")
	if err != nil {
		return "", errors.New("temporary directory unavailable")
	}
	defer os.RemoveAll(home)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--version")
	cmd.Dir = home
	cmd.Env = []string{"HOME=" + home, "CODDY_HOME=" + home, "PATH=/usr/bin:/bin", "NO_COLOR=1"}
	var output boundedOutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if cmd.Run() != nil {
		return "", errors.New("version probe failed")
	}
	version := strings.TrimSpace(string(output.data))
	if !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(version) {
		return "", errors.New("unexpected version response")
	}
	return version, nil
}
