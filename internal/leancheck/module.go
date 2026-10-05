package leancheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type ModuleReport struct {
	Artifacts         map[string]string `json:"artifacts,omitempty"`
	Status            string            `json:"status"`
	Module            string            `json:"module"`
	SourceSHA256      string            `json:"source_sha256"`
	EnvironmentSHA256 string            `json:"environment_sha256"`
	ArtifactSHA256    string            `json:"artifact_sha256,omitempty"`
	Diagnostics       string            `json:"diagnostics,omitempty"`
	Axioms            []string          `json:"axioms,omitempty"`
}

func ModuleSource(g Goal, source string) (string, error) {
	lines := strings.SplitN(source, "\n", 2)
	if len(lines) != 2 || strings.TrimSpace(lines[0]) != "import Goal" {
		return "", errors.New("library candidate must start with import Goal")
	}
	return g.Source + "\n" + lines[1], nil
}
func (c DockerChecker) BuildModule(ctx context.Context, g Goal, source, module, dir string) (r ModuleReport, err error) {
	if !regexp.MustCompile(`^ResearchLemma_[a-f0-9]{32}$`).MatchString(module) {
		return r, errors.New("invalid library module name")
	}
	combined, err := ModuleSource(g, source)
	if err != nil {
		return r, err
	}
	r = ModuleReport{Status: "failed", Module: module, SourceSHA256: Digest(combined), EnvironmentSHA256: Digest(c.Config), Artifacts: map[string]string{}}
	if err = c.Config.Validate(); err != nil {
		return r, err
	}
	if err = g.Validate(); err != nil {
		return r, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.Config.TimeoutSeconds)*time.Second)
	defer cancel()
	for _, p := range []string{"input", "compiled", "checked", "trusted"} {
		if err = os.Mkdir(filepath.Join(dir, p), 0700); err != nil {
			return r, err
		}
	}
	if err = os.WriteFile(filepath.Join(dir, "input", module+".lean"), []byte(combined), 0600); err != nil {
		return r, err
	}
	if err = os.WriteFile(filepath.Join(dir, "trusted", "Audit.lean"), []byte(auditSource), 0600); err != nil {
		return r, err
	}
	bind := func(src, dst string, ro bool) string {
		v := "type=bind,src=" + src + ",dst=" + dst
		if ro {
			v += ",readonly"
		}
		return v
	}
	mounts := []string{bind(c.Config.Toolchain, "/lean", true)}
	search := []string{"/module"}
	for i, p := range c.Config.Libraries {
		dst := fmt.Sprintf("/library-%d", i)
		mounts = append(mounts, bind(p, dst, true))
		search = append(search, dst)
	}
	version, e := c.run(ctx, "/lean/bin/lean", []string{"--version"}, mounts, "", "/")
	if e != nil || strings.TrimSpace(version) != c.Config.ExpectedVersion {
		return r, errors.New("Lean version mismatch")
	}
	out, e := c.run(ctx, "/lean/bin/lean", []string{"-o", "/output/" + module + ".olean", module + ".lean"}, appendCopy(mounts, bind(filepath.Join(dir, "input"), "/input", true), bind(filepath.Join(dir, "compiled"), "/output", false)), strings.Join(search[1:], ":"), "/input")
	if e != nil {
		r.Diagnostics = preview(out, 24000)
		return r, e
	}
	total := int64(0)
	for _, suffix := range []string{".olean", ".olean.private", ".olean.server"} {
		p := filepath.Join(dir, "compiled", module+suffix)
		info, e := os.Lstat(p)
		if os.IsNotExist(e) && suffix != ".olean" {
			continue
		}
		if e != nil || !info.Mode().IsRegular() || info.Size() > 16<<20 {
			return r, errors.New("invalid compiled module")
		}
		total += info.Size()
		if total > 16<<20 {
			return r, errors.New("module artifacts exceed limit")
		}
		body, e := os.ReadFile(p)
		if e != nil {
			return r, e
		}
		if e = os.WriteFile(filepath.Join(dir, "checked", module+suffix), body, 0600); e != nil {
			return r, e
		}
		if suffix == ".olean" {
			r.ArtifactSHA256 = Digest(body)
		}
		r.Artifacts[module+suffix] = Digest(body)
	}
	checked := appendCopy(mounts, bind(filepath.Join(dir, "checked"), "/module", true))
	out, e = c.run(ctx, "/lean/bin/leanchecker", []string{module}, checked, strings.Join(search, ":"), "/")
	if e != nil {
		r.Diagnostics = preview(out, 24000)
		return r, e
	}
	out, e = c.run(ctx, "/lean/bin/lean", []string{"--run", "Audit.lean", g.Declaration, g.Candidate, module}, appendCopy(checked, bind(filepath.Join(dir, "trusted"), "/trusted", true)), strings.Join(search, ":"), "/trusted")
	if e != nil {
		r.Diagnostics = preview(out, 24000)
		return r, e
	}
	var audit struct {
		Matches bool     `json:"matches_goal"`
		Axioms  []string `json:"axioms"`
	}
	if json.Unmarshal([]byte(out), &audit) != nil || !audit.Matches {
		return r, errors.New("invalid module audit")
	}
	for _, a := range audit.Axioms {
		if a != "propext" && a != "Classical.choice" && a != "Quot.sound" {
			return r, errors.New("disallowed module axiom")
		}
	}
	r.Status = "ready"
	r.Axioms = audit.Axioms
	return r, nil
}

func VerifyModuleArtifacts(dir string, r ModuleReport) error {
	if r.Status != "ready" || len(r.Artifacts) < 1 || len(r.Artifacts) > 3 || r.Artifacts[r.Module+".olean"] != r.ArtifactSHA256 {
		return errors.New("missing module manifest")
	}
	for name, pin := range r.Artifacts {
		if name != r.Module+".olean" && name != r.Module+".olean.private" && name != r.Module+".olean.server" {
			return errors.New("invalid module artifact name")
		}
		p := filepath.Join(dir, name)
		info, err := os.Lstat(p)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 16<<20 {
			return errors.New("invalid module artifact")
		}
		body, err := os.ReadFile(p)
		if err != nil || Digest(body) != pin {
			return errors.New("library artifact integrity mismatch")
		}
	}
	return nil
}
