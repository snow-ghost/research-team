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

//go:embed Inspect.lean
var inspectSource string

type Binder struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Kind     string `json:"kind"`
	Implicit bool   `json:"implicit"`
}

type SignatureReport struct {
	Status            string   `json:"status"`
	Binders           []Binder `json:"binders"`
	Conclusion        string   `json:"conclusion"`
	FullType          string   `json:"full_type"`
	Universes         []string `json:"universes"`
	GoalSHA256        string   `json:"goal_sha256"`
	ArtifactSHA256    string   `json:"artifact_sha256"`
	EnvironmentSHA256 string   `json:"environment_sha256"`
	InspectorSHA256   string   `json:"inspector_sha256"`
	Diagnostics       string   `json:"diagnostics,omitempty"`
}

func InspectorDigest() string { return Digest(inspectSource) }

func (c DockerChecker) InspectModule(ctx context.Context, g Goal, moduleDir string, module ModuleReport, dir string) (r SignatureReport, err error) {
	r = SignatureReport{Status: "failed", GoalSHA256: Digest(g), ArtifactSHA256: module.ArtifactSHA256, EnvironmentSHA256: Digest(c.Config), InspectorSHA256: InspectorDigest()}
	if err = c.Config.Validate(); err != nil {
		return r, err
	}
	if err = g.Validate(); err != nil {
		return r, err
	}
	if err = VerifyModuleArtifacts(moduleDir, module); err != nil {
		return r, err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err = os.MkdirAll(dir, 0700); err != nil {
		return r, err
	}
	if err = os.WriteFile(filepath.Join(dir, "Inspect.lean"), []byte(inspectSource), 0600); err != nil {
		return r, err
	}
	bind := func(src, dst string) string { return "type=bind,src=" + src + ",dst=" + dst + ",readonly" }
	mounts := []string{bind(c.Config.Toolchain, "/lean"), bind(moduleDir, "/module"), bind(dir, "/trusted")}
	search := []string{"/module"}
	for i, path := range c.Config.Libraries {
		dest := fmt.Sprintf("/library-%d", i)
		mounts = append(mounts, bind(path, dest))
		search = append(search, dest)
	}
	version, err := c.run(ctx, "/lean/bin/lean", []string{"--version"}, mounts, "", "/")
	if err != nil || strings.TrimSpace(version) != c.Config.ExpectedVersion {
		return r, errors.New("Lean version mismatch")
	}
	out, err := c.run(ctx, "/lean/bin/lean", []string{"--run", "Inspect.lean", module.Module, g.Declaration, g.Candidate}, mounts, strings.Join(search, ":"), "/trusted")
	if err != nil {
		r.Diagnostics = preview(out, 24000)
		return r, err
	}
	var extracted struct {
		Binders    []Binder `json:"binders"`
		Conclusion string   `json:"conclusion"`
		FullType   string   `json:"full_type"`
		Universes  []string `json:"universes"`
	}
	if json.Unmarshal([]byte(out), &extracted) != nil || len(extracted.Binders) > 128 || extracted.Conclusion == "" {
		return r, errors.New("invalid extracted signature")
	}
	r.Status, r.Binders, r.Conclusion, r.FullType, r.Universes = "verified", extracted.Binders, extracted.Conclusion, extracted.FullType, extracted.Universes
	return r, nil
}
