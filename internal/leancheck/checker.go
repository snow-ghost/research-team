package leancheck

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

//go:embed Audit.lean
var auditSource string

type Config struct {
	Runtime         string   `json:"runtime"`
	Image           string   `json:"image"`
	Toolchain       string   `json:"toolchain"`
	ExpectedVersion string   `json:"expected_version"`
	Libraries       []string `json:"libraries,omitempty"`
	LibraryRevision string   `json:"library_revision,omitempty"`
	TimeoutSeconds  int      `json:"timeout_seconds"`
	MemoryMB        int      `json:"memory_mb"`
}
type Goal struct {
	Source      string `json:"source"`
	Declaration string `json:"declaration"`
	Candidate   string `json:"candidate"`
}
type Report struct {
	Status            string   `json:"status"`
	Phase             string   `json:"phase"`
	Diagnostics       string   `json:"diagnostics,omitempty"`
	Axioms            []string `json:"axioms"`
	GoalSHA256        string   `json:"goal_sha256"`
	SourceSHA256      string   `json:"source_sha256"`
	EnvironmentSHA256 string   `json:"environment_sha256"`
	LeanVersion       string   `json:"lean_version"`
	ArtifactSHA256    string   `json:"artifact_sha256,omitempty"`
	AuditSHA256       string   `json:"audit_sha256,omitempty"`
}
type Checker interface {
	Check(context.Context, Goal, string, string) (Report, error)
}
type DockerChecker struct{ Config Config }

var declaration = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)

func (g Goal) Validate() error {
	if len(g.Source) == 0 || len(g.Source) > 65536 || !declaration.MatchString(g.Declaration) ||
		!declaration.MatchString(g.Candidate) || g.Declaration == g.Candidate {
		return errors.New("formal goal requires source and distinct declaration names")
	}
	return nil
}
func (c Config) Validate() error {
	if !filepath.IsAbs(c.Runtime) || !filepath.IsAbs(c.Toolchain) || c.ExpectedVersion == "" ||
		!regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(c.Image) ||
		c.TimeoutSeconds < 1 || c.TimeoutSeconds > 600 || c.MemoryMB < 256 || c.MemoryMB > 8192 || len(c.Libraries) > 20 {
		return errors.New("invalid pinned Lean container configuration")
	}
	for _, path := range append([]string{c.Toolchain}, c.Libraries...) {
		real, err := filepath.EvalSymlinks(path)
		if err != nil || real != filepath.Clean(path) || !filepath.IsAbs(path) || strings.ContainsAny(path, ",\r\n\x00:") {
			return errors.New("Lean dependencies require existing absolute paths without symlinks")
		}
	}
	return nil
}
func Digest(value any) string {
	body, _ := json.Marshal(value)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
func AuditDigest() string { return Digest(auditSource) }
func (c DockerChecker) Check(ctx context.Context, g Goal, source, dir string) (report Report, err error) {
	report = Report{Status: "failed", Axioms: []string{}, GoalSHA256: Digest(g), SourceSHA256: Digest(source),
		EnvironmentSHA256: Digest(c.Config), LeanVersion: c.Config.ExpectedVersion, AuditSHA256: AuditDigest()}
	if err = g.Validate(); err != nil {
		return report, err
	}
	if err = c.Config.Validate(); err != nil {
		return report, err
	}
	if len(source) == 0 || len(source) > 65536 {
		return report, errors.New("candidate source is empty or too large")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.Config.TimeoutSeconds)*time.Second)
	defer cancel()
	for _, name := range []string{"input", "goal", "candidate", "checked", "trusted"} {
		if err = os.Mkdir(filepath.Join(dir, name), 0700); err != nil {
			return report, err
		}
	}
	for path, body := range map[string]string{"input/Goal.lean": g.Source, "input/Candidate.lean": source, "trusted/Audit.lean": auditSource} {
		if err = os.WriteFile(filepath.Join(dir, path), []byte(body), 0600); err != nil {
			return report, err
		}
	}
	bind := func(source, dest string, ro bool) string {
		value := "type=bind,src=" + source + ",dst=" + dest
		if ro {
			value += ",readonly"
		}
		return value
	}
	baseMounts := []string{bind(c.Config.Toolchain, "/lean", true)}
	search := []string{"/goal", "/candidate"}
	for i, path := range c.Config.Libraries {
		dest := fmt.Sprintf("/library-%d", i)
		baseMounts = append(baseMounts, bind(path, dest, true))
		search = append(search, dest)
	}
	input := bind(filepath.Join(dir, "input"), "/input", true)
	goal := bind(filepath.Join(dir, "goal"), "/goal", true)
	candidate := bind(filepath.Join(dir, "checked"), "/candidate", true)
	trusted := bind(filepath.Join(dir, "trusted"), "/trusted", true)
	phases := []struct {
		name, program string
		args, mounts  []string
		path, workdir string
	}{
		{"version", "/lean/bin/lean", []string{"--version"}, baseMounts, "", "/"},
		{"goal", "/lean/bin/lean", []string{"-o", "/output/Goal.olean", "Goal.lean"},
			appendCopy(baseMounts, input, bind(filepath.Join(dir, "goal"), "/output", false)), strings.Join(search[2:], ":"), "/input"},
		{"compile", "/lean/bin/lean", []string{"-o", "/output/Candidate.olean", "Candidate.lean"},
			appendCopy(baseMounts, input, goal, bind(filepath.Join(dir, "candidate"), "/output", false)), strings.Join(search, ":"), "/input"},
		{"kernel", "/lean/bin/leanchecker", []string{"Candidate"},
			appendCopy(baseMounts, goal, candidate), strings.Join(search, ":"), "/"},
		{"audit", "/lean/bin/lean", []string{"--run", "Audit.lean", g.Declaration, g.Candidate},
			appendCopy(baseMounts, goal, candidate, trusted), strings.Join(search, ":"), "/trusted"},
	}
	for _, phase := range phases {
		report.Phase = phase.name
		output, runErr := c.run(ctx, phase.program, phase.args, phase.mounts, phase.path, phase.workdir)
		if runErr != nil {
			report.Diagnostics = preview(output, 24000)
			if ctx.Err() != nil {
				report.Status = "timed_out"
				return report, ctx.Err()
			}
			return report, runErr
		}
		if phase.name == "version" && strings.TrimSpace(output) != c.Config.ExpectedVersion {
			return report, errors.New("Lean version mismatch")
		}
		if phase.name == "compile" {
			if err = copyArtifacts(filepath.Join(dir, "candidate"), filepath.Join(dir, "checked")); err != nil {
				return report, err
			}
		}
		if phase.name == "audit" {
			var result struct {
				Matches bool     `json:"matches_goal"`
				Axioms  []string `json:"axioms"`
			}
			if json.Unmarshal([]byte(output), &result) != nil || !result.Matches {
				return report, errors.New("invalid kernel audit")
			}
			report.Axioms = result.Axioms
			allowed := map[string]bool{"propext": true, "Classical.choice": true, "Quot.sound": true}
			for _, axiom := range result.Axioms {
				if !allowed[axiom] {
					report.Diagnostics = "Disallowed axiom: " + axiom
					report.Status = "rejected"
					return report, nil
				}
			}
		}
	}
	body, err := os.ReadFile(filepath.Join(dir, "checked", "Candidate.olean"))
	if err != nil {
		return report, err
	}
	report.ArtifactSHA256 = Digest(body)
	report.Status, report.Phase = "verified", "complete"
	return report, nil
}
func appendCopy(base []string, extra ...string) []string {
	return append(append([]string{}, base...), extra...)
}
func preview(value string, limit int) string {
	if len(value) > limit {
		return value[:limit]
	}
	return value
}
func copyArtifacts(from, to string) error {
	total := 0
	for _, name := range []string{"Candidate.olean", "Candidate.olean.private", "Candidate.olean.server"} {
		path := filepath.Join(from, name)
		stat, err := os.Lstat(path)
		if os.IsNotExist(err) && name != "Candidate.olean" {
			continue
		}
		if err != nil || !stat.Mode().IsRegular() || stat.Size() > 16<<20 {
			return errors.New("invalid Lean artifact")
		}
		total += int(stat.Size())
		if total > 16<<20 {
			return errors.New("Lean artifacts exceed limit")
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(to, name), body, 0600); err != nil {
			return err
		}
	}
	return nil
}

type cappedOutput struct {
	buffer bytes.Buffer
	limit  int
}

func (w *cappedOutput) Write(body []byte) (int, error) {
	if w.buffer.Len()+len(body) > w.limit {
		return 0, errors.New("Lean output limit reached")
	}
	return w.buffer.Write(body)
}
func (c DockerChecker) run(ctx context.Context, program string, args, mounts []string, search, workdir string) (string, error) {
	var identity [16]byte
	if _, err := rand.Read(identity[:]); err != nil {
		return "", err
	}
	name := "research-lean-" + hex.EncodeToString(identity[:])
	dockerArgs := []string{"run", "--rm", "--pull=never", "--name", name, "--read-only", "--network=none", "--cap-drop=ALL",
		"--security-opt=no-new-privileges", "--pids-limit=128", "--memory", fmt.Sprintf("%dm", c.Config.MemoryMB),
		"--memory-swap", fmt.Sprintf("%dm", c.Config.MemoryMB), "--cpus=2", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"--tmpfs", "/tmp:rw,nosuid,nodev,size=128m", "--env", "HOME=/tmp", "--env", "LEAN_PATH=" + search,
		"--env", "PATH=/lean/bin:/usr/bin:/bin",
		"--workdir", workdir, "--entrypoint", program}
	for _, mount := range mounts {
		dockerArgs = append(dockerArgs, "--mount", mount)
	}
	dockerArgs = append(dockerArgs, c.Config.Image)
	dockerArgs = append(dockerArgs, args...)
	cmd := exec.CommandContext(ctx, c.Config.Runtime, dockerArgs...)
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	output := &cappedOutput{limit: 1 << 20}
	cmd.Stdout, cmd.Stderr = output, output
	err := cmd.Run()
	cleanCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	remove := exec.CommandContext(cleanCtx, c.Config.Runtime, "rm", "--force", name)
	remove.Env = cmd.Env
	if remove.Run() != nil {
		check := exec.CommandContext(cleanCtx, c.Config.Runtime, "container", "ls", "--all", "--filter", "name=^/"+name+"$", "--format", "{{.Names}}")
		check.Env = cmd.Env
		state, checkErr := check.Output()
		if checkErr != nil || strings.TrimSpace(string(state)) != "" {
			err = errors.Join(err, errors.New("Lean container cleanup failed"))
		}
	}
	if err != nil {
		return output.buffer.String(), errors.New("Lean phase failed")
	}
	return output.buffer.String(), nil
}

func ExtractSource(text string) (string, error) {
	lines := strings.Split(text, "\n")
	var blocks []string
	collecting := false
	var block strings.Builder
	for _, line := range lines {
		if !collecting && (strings.TrimSpace(line) == "```lean" || strings.TrimSpace(line) == "```lean4") {
			collecting = true
			block.Reset()
			continue
		}
		if collecting && strings.TrimSpace(line) == "```" {
			blocks = append(blocks, block.String())
			collecting = false
			continue
		}
		if collecting {
			block.WriteString(line)
			block.WriteByte('\n')
		}
	}
	if collecting || len(blocks) != 1 || len(blocks[0]) > 65536 {
		return "", errors.New("return exactly one closed Lean code block")
	}
	return blocks[0], nil
}
