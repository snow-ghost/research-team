package execution

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func containerProfile(t *testing.T) Profile {
	t.Helper()
	p := coddyProfile(filepath.Join(t.TempDir(), "coddy"))
	if err := os.WriteFile(p.External.Executable, []byte("fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	p.External.ExpectedVersion = "1.2.1"
	p.External.BaseURL, p.External.AllowLoopbackHTTP = "https://example.com/v1", false
	p.External.ExecutionBoundary = "docker"
	p.External.Container = &ContainerConfig{Runtime: "/usr/bin/docker", Image: "sha256:" + strings.Repeat("a", 64), MemoryMB: 512, CPUs: 1}
	return p
}

func TestContainerBDD_MountsAndCredentialsAreRestricted(t *testing.T) {
	p := containerProfile(t)
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	home, workspace := t.TempDir(), t.TempDir()
	program, args, _, err := coddyContainer(*p.External, home, workspace,
		[]string{"OPENAI_API_KEY=do-not-expose", "HOME=" + home}, []string{"acp"})
	if err != nil || program != "/usr/bin/docker" {
		t.Fatalf("%s %v", program, err)
	}
	joined := strings.Join(args, "\n")
	for _, required := range []string{"--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges",
		"--pull=never", "--user", "--pids-limit=128", "--network=bridge",
		"type=bind,src=" + workspace + ",dst=" + workspace + ",readonly", "--env\nOPENAI_API_KEY"} {
		if !strings.Contains(joined, required) {
			t.Errorf("missing %s", required)
		}
	}
	if strings.Contains(joined, "do-not-expose") || strings.Contains(joined, "docker.sock") ||
		strings.Count(joined, "type=bind") != 3 {
		t.Fatal("unexpected credential or mount exposure")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(workspace, link); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := coddyContainer(*p.External, home, link, nil, nil); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestContainerBDD_InvalidIsolationFailsClosed(t *testing.T) {
	for _, mutate := range []func(*ExternalConfig){
		func(e *ExternalConfig) { e.Container.Image = "latest" },
		func(e *ExternalConfig) { e.Container.MemoryMB = 0 },
		func(e *ExternalConfig) { e.Container.CPUs = 100 },
		func(e *ExternalConfig) { e.Container.Runtime = "docker" },
		func(e *ExternalConfig) { e.ExecutionBoundary = "operator_managed" },
		func(e *ExternalConfig) { e.AllowLoopbackHTTP = true },
		func(e *ExternalConfig) { e.Container = nil },
		func(e *ExternalConfig) { e.ExpectedVersion = "1.2.2" },
		func(e *ExternalConfig) { e.ExpectedVersion = "1.2.55" },
	} {
		p := containerProfile(t)
		mutate(p.External)
		if p.Validate() == nil {
			t.Fatal("invalid isolation accepted")
		}
	}
}

func TestContainerBDD_RealDockerBoundaryAndCancellation(t *testing.T) {
	image := os.Getenv("RESEARCH_TEST_CONTAINER_IMAGE")
	if image == "" {
		t.Skip("set RESEARCH_TEST_CONTAINER_IMAGE to a locally built image ID; no model is called")
	}
	for _, cancelRun := range []bool{false, true} {
		p := containerProfile(t)
		p.External.Container.Image = image
		home, workspace := t.TempDir(), t.TempDir()
		if err := os.WriteFile(filepath.Join(workspace, "input"), []byte("visible"), 0600); err != nil {
			t.Fatal(err)
		}
		program, args, cleanup, err := coddyContainer(*p.External, home, workspace, []string{"HOME=" + home}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cleanup() })
		name := ""
		for i := range args {
			if args[i] == "--entrypoint" {
				args[i+1] = "/bin/sh"
			}
			if args[i] == "--name" {
				name = args[i+1]
			}
		}
		script := "test -r input && ! touch input && ! touch /etc/forbidden && test ! -e /var/run/docker.sock && touch \"$HOME/ok\""
		if cancelRun {
			script = "touch \"$HOME/started\"; sleep 60"
		}
		args = append(args, "-c", script)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		cmd := exec.CommandContext(ctx, program, args...)
		cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + home}
		if !cancelRun {
			out, err := cmd.CombinedOutput()
			cancel()
			if err != nil {
				t.Fatalf("container boundary: %v: %s", err, out)
			}
		} else {
			if err := cmd.Start(); err != nil {
				cancel()
				t.Fatal(err)
			}
			for {
				_, err := os.Stat(filepath.Join(home, "started"))
				if err == nil {
					break
				}
				if ctx.Err() != nil {
					cancel()
					_ = cmd.Wait()
					t.Fatal("container did not start")
				}
				time.Sleep(25 * time.Millisecond)
			}
			cancel()
			_ = cmd.Wait()
		}
		if err := cleanup(); err != nil {
			t.Fatal(err)
		}
		if err := cleanup(); err != nil {
			t.Fatalf("repeated cleanup: %v", err)
		}
		inspect := exec.Command(program, "container", "inspect", name)
		if inspect.Run() == nil {
			t.Fatal("container survived cleanup")
		}
	}
}
