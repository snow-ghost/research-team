package leancheck

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed Match.lean
var matchSource string

func MatcherDigest() string { return Digest(matchSource) }

type MissingArgument struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Type       string `json:"type"`
	ClosedType string `json:"closed_type,omitempty"`
}

type MatchReport struct {
	Status            string            `json:"status"`
	Bindings          []Argument        `json:"bindings"`
	Missing           []MissingArgument `json:"missing"`
	Source            string            `json:"source,omitempty"`
	Verification      *Report           `json:"verification,omitempty"`
	GoalSHA256        string            `json:"goal_sha256"`
	ArtifactSHA256    string            `json:"artifact_sha256"`
	EnvironmentSHA256 string            `json:"environment_sha256"`
	MatcherSHA256     string            `json:"matcher_sha256"`
	Diagnostics       string            `json:"diagnostics,omitempty"`
}

// Type unification proposes a term; only the separate kernel check marks it verified.
func (c DockerChecker) MatchModule(ctx context.Context, g Goal, moduleDir string, module ModuleReport, candidate, dir string) (r MatchReport, err error) {
	found := false
	for _, path := range c.Config.Libraries {
		found = found || path == moduleDir
	}
	if !found {
		c.Config.Libraries = append(append([]string{}, c.Config.Libraries...), moduleDir)
	}
	r = MatchReport{Status: "failed", Bindings: []Argument{}, Missing: []MissingArgument{}, GoalSHA256: Digest(g), ArtifactSHA256: module.ArtifactSHA256, EnvironmentSHA256: Digest(c.Config), MatcherSHA256: MatcherDigest()}
	if err = c.Config.Validate(); err != nil {
		return r, err
	}
	if err = g.Validate(); err != nil {
		return r, err
	}
	if !declaration.MatchString(candidate) {
		return r, errors.New("invalid candidate name")
	}
	if err = VerifyModuleArtifacts(moduleDir, module); err != nil {
		return r, err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	for _, name := range []string{"input", "goal", "trusted"} {
		if err = os.MkdirAll(filepath.Join(dir, name), 0700); err != nil {
			return r, err
		}
	}
	if err = os.WriteFile(filepath.Join(dir, "input", "Goal.lean"), []byte(g.Source), 0600); err != nil {
		return r, err
	}
	if err = os.WriteFile(filepath.Join(dir, "trusted", "Match.lean"), []byte(matchSource), 0600); err != nil {
		return r, err
	}
	bind := func(src, dst string, ro bool) string {
		b := "type=bind,src=" + src + ",dst=" + dst
		if ro {
			b += ",readonly"
		}
		return b
	}
	mounts := []string{bind(c.Config.Toolchain, "/lean", true), bind(moduleDir, "/module", true), bind(filepath.Join(dir, "input"), "/input", true), bind(filepath.Join(dir, "trusted"), "/trusted", true)}
	search := []string{"/module", "/goal"}
	for i, path := range c.Config.Libraries {
		dst := fmt.Sprintf("/library-%d", i)
		mounts = append(mounts, bind(path, dst, true))
		search = append(search, dst)
	}
	version, err := c.run(ctx, "/lean/bin/lean", []string{"--version"}, mounts, "", "/")
	if err != nil || strings.TrimSpace(version) != c.Config.ExpectedVersion {
		return r, errors.New("Lean version mismatch")
	}
	out, err := c.run(ctx, "/lean/bin/lean", []string{"-o", "/goal/Goal.olean", "Goal.lean"}, append(append([]string{}, mounts...), bind(filepath.Join(dir, "goal"), "/goal", false)), strings.Join(search, ":"), "/input")
	if err != nil {
		r.Diagnostics = preview(out, 24000)
		return r, err
	}
	out, err = c.run(ctx, "/lean/bin/lean", []string{"--run", "Match.lean", module.Module, g.Declaration, candidate}, append(append([]string{}, mounts...), bind(filepath.Join(dir, "goal"), "/goal", true)), strings.Join(search, ":"), "/trusted")
	if err != nil {
		r.Diagnostics = preview(out, 24000)
		return r, err
	}
	var proposed struct {
		Status   string            `json:"status"`
		Bindings []Argument        `json:"bindings"`
		Missing  []MissingArgument `json:"missing"`
		Term     string            `json:"term"`
	}
	if json.Unmarshal([]byte(out), &proposed) != nil || len(proposed.Bindings) > 128 || len(proposed.Missing) > 128 || len(proposed.Term) > 60000 || (proposed.Status != "no_match" && proposed.Status != "obligations" && proposed.Status != "proposed") {
		return r, errors.New("invalid typed match report")
	}
	r.Status, r.Bindings, r.Missing = proposed.Status, proposed.Bindings, proposed.Missing
	if proposed.Status == "proposed" {
		r.Source = "import Goal\ntheorem " + g.Candidate + " : " + g.Declaration + " :=\n  " + proposed.Term + "\n"
		checkDir := filepath.Join(dir, "verification")
		if err = os.Mkdir(checkDir, 0700); err != nil {
			return r, err
		}
		check, checkErr := c.Check(ctx, g, r.Source, checkDir)
		r.Verification = &check
		if checkErr == nil && check.Status == "verified" {
			r.Status = "verified"
		} else {
			r.Status = "candidate_failed"
			r.Diagnostics = check.Diagnostics
		}
	}
	return r, nil
}
