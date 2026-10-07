package execution

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Only trusted operator profiles can choose the runtime, image and binary.
func coddyContainer(spec ExternalConfig, home, workspace string, env, agentArgs []string) (string, []string, func() error, error) {
	for _, path := range []string{spec.Executable, home, workspace} {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || resolved != filepath.Clean(path) || !filepath.IsAbs(path) || strings.ContainsAny(path, ",\r\n\x00") {
			return "", nil, nil, errors.New("container mounts require absolute paths without symlinks or delimiters")
		}
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", nil, nil, errors.New("cannot allocate container identity")
	}
	name := "research-attempt-" + hex.EncodeToString(id[:])
	c := spec.Container
	args := []string{"run", "--rm", "--pull=never", "--interactive", "--init", "--name", name,
		"--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--network=bridge",
		"--user", strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid()),
		"--pids-limit=128", "--memory", strconv.Itoa(c.MemoryMB) + "m",
		"--memory-swap", strconv.Itoa(c.MemoryMB) + "m", "--cpus", strconv.Itoa(c.CPUs),
		"--tmpfs", "/tmp:rw,nosuid,nodev,noexec,size=128m,mode=1777",
		"--mount", "type=bind,src=" + home + ",dst=/research/home",
		"--mount", "type=bind,src=" + workspace + ",dst=/research/workspace,readonly",
		"--mount", "type=bind,src=" + spec.Executable + ",dst=/usr/local/bin/coddy,readonly",
		"--workdir", "/research/workspace", "--entrypoint", "/usr/local/bin/coddy"}
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || !envName.MatchString(key) {
			return "", nil, nil, errors.New("invalid container environment")
		}
		args = append(args, "--env", key)
	}
	args = append(args, c.Image)
	for _, arg := range agentArgs {
		args = append(args, containerPath(arg, home, workspace))
	}
	cleanup := func() error {
		// Killing an attached Docker client alone does not stop the container.
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, c.Runtime, "rm", "--force", name)
		cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + home}
		if cmd.Run() != nil {
			// --rm can remove the container concurrently with the explicit cleanup.
			check := exec.CommandContext(ctx, c.Runtime, "container", "ls", "--all",
				"--filter", "name=^/"+name+"$", "--format", "{{.Names}}")
			check.Env = cmd.Env
			output, err := check.Output()
			if err == nil && strings.TrimSpace(string(output)) == "" {
				return nil
			}
			return errors.New("container cleanup failed; operator inspection required")
		}
		return nil
	}
	return c.Runtime, args, cleanup, nil
}

func containerPath(value, home, workspace string) string {
	if value == home || strings.HasPrefix(value, home+"/") {
		return "/research/home" + strings.TrimPrefix(value, home)
	}
	if value == workspace || strings.HasPrefix(value, workspace+"/") {
		return "/research/workspace" + strings.TrimPrefix(value, workspace)
	}
	return value
}
