//go:build linux

package execution

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func fixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func externalProfile(provider, exe string) Profile {
	return Profile{ID: "external-reader", Kind: "external", Limits: Limits{TimeoutSeconds: 5, MaxOutputBytes: 8192},
		External: &ExternalConfig{Provider: provider, Executable: exe, ExpectedVersion: supportedVersions[provider],
			Model: "test-model", SearchPath: "/usr/bin:/bin", ExecutionBoundary: "operator_managed",
			SecretEnv: map[string]string{"ANTHROPIC_API_KEY": "TEST_TOKEN"}},
	}
}

func TestExternalBDD_ProviderFramesBecomeCandidates(t *testing.T) {
	frames := map[string]string{
		"codex": `{"type":"thread.started","thread_id":"s1"}
{"type":"turn.started"}
{"type":"item.completed","item":{"type":"agent_message","text":"candidate private-test-token"}}
{"type":"turn.completed"}`,
		"claude": `{"type":"result","subtype":"success","is_error":false,"session_id":"s1","result":"candidate private-test-token"}`,
		"opencode": `{"type":"step_start","sessionID":"s1"}
{"type":"text","sessionID":"s1","part":{"text":"candidate private-test-token"}}
{"type":"step_finish","sessionID":"s1","part":{"reason":"stop"}}`,
	}
	t.Setenv("UNRELATED_SECRET", "must-not-leak")
	for provider, output := range frames {
		t.Run(provider, func(t *testing.T) {
			script := "if [ \"$1\" = '--version' ]; then\nprintf '%s\\n' '" + supportedVersions[provider] + "'\nexit 0\nfi\n" +
				"test -z \"$UNRELATED_SECRET\" || exit 9\n" +
				"test \"$ANTHROPIC_API_KEY\" = private-test-token || exit 8\n" +
				"cat >/dev/null\nprintf '%s\\n' '" + output + "'\n"
			p := externalProfile(provider, fixture(t, script))
			task := taskFor(t)
			task.Objective = "Literal $(touch SHOULD_NOT_EXIST); `exit 1`"
			result, err := buildTest(t, p).Run(context.Background(), task)
			if err != nil || result.Status != "candidate" || result.SessionID != "s1" || result.Usage != nil ||
				result.Candidate != "candidate [REDACTED]" {
				t.Fatalf("%+v %v", result, err)
			}
			if _, err := os.Stat(filepath.Join(task.Workspace, "SHOULD_NOT_EXIST")); !os.IsNotExist(err) {
				t.Fatal("prompt was executed")
			}
			args := strings.Join(externalArgs(*p.External), " ")
			if strings.Contains(args, task.Objective) || strings.Contains(args, "private-test-token") ||
				strings.Contains(args, "dangerously") {
				t.Fatal("unsafe command arguments")
			}
		})
	}
}

func TestExternalBDD_VersionMismatchPreventsResearchLaunch(t *testing.T) {
	p := externalProfile("codex", fixture(t, "printf 'wrong-version\\n'\n"))
	var lookups int
	e, err := Build(p, func(string) (string, bool) { lookups++; return "secret", true })
	if err != nil {
		t.Fatal(err)
	}
	result, err := e.Run(context.Background(), taskFor(t))
	if err == nil || lookups != 0 || result.RemoteOutcome != "not_started" {
		t.Fatalf("%+v %v lookups=%d", result, err, lookups)
	}
}

func TestExternalBDD_ExitZeroIsNotEnough(t *testing.T) {
	for _, tt := range []struct{ provider, output string }{
		{"codex", `{"type":"item.completed","item":{"type":"agent_message","text":"unfinished"}}`},
		{"codex", "not JSON"},
		{"codex", `{"type":"error","message":"failed"}`},
		{"claude", `{"type":"result","subtype":"error_max_turns","is_error":true,"result":"partial"}`},
		{"opencode", `{"type":"text","part":{"text":"partial"}}
{"type":"step_finish","part":{"reason":"length"}}`},
		{"codex", `{"type":"thread.started","thread_id":"s1"}
{"type":"thread.started","thread_id":"s2"}`},
	} {
		if _, _, err := parseExternal(tt.provider, []byte(tt.output)); err == nil {
			t.Errorf("accepted %s", tt.output)
		}
	}
}

func TestExternalBDD_UnsupportedConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Profile)
	}{
		{"unknown_provider", func(p *Profile) { p.External.Provider = "unknown-agent" }},
		{"unreviewed_version", func(p *Profile) { p.External.ExpectedVersion = "2.0.0" }},
		{"relative_search_path", func(p *Profile) { p.External.SearchPath = ".:/usr/bin" }},
		{"uncontrolled_boundary", func(p *Profile) { p.External.ExecutionBoundary = "" }},
		{"unenforceable_token_budget", func(p *Profile) { p.Limits.MaxOutputTokens = 10 }},
		{"dangerous_environment", func(p *Profile) { p.External.SecretEnv = map[string]string{"LD_PRELOAD": "TEST_TOKEN"} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := externalProfile("codex", "/bin/false")
			tt.change(&p)
			if p.Validate() == nil {
				t.Fatal("unsupported profile accepted")
			}
		})
	}
}

func TestProcessBDD_OutputIsBounded(t *testing.T) {
	for _, redirect := range []string{"", " >&2"} {
		exe := fixture(t, "head -c 20000 /dev/zero"+redirect+"\n")
		_, err := runProcess(context.Background(), exe, nil, []string{"PATH=/usr/bin:/bin"}, t.TempDir(), "", 1024)
		if !errors.Is(err, ErrLimit) {
			t.Fatalf("output overflow: %v", err)
		}
	}
}

func TestProcessBDD_CancellationKillsProcessGroup(t *testing.T) {
	dir := t.TempDir()
	exe := fixture(t, "sleep 60 &\nprintf '%s' \"$!\" > child.pid\nwait\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := runProcess(ctx, exe, nil, []string{"PATH=/usr/bin:/bin"}, dir, "", 1024)
		done <- err
	}()
	var pid int
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(filepath.Join(dir, "child.pid"))
		pid, _ = strconv.Atoi(string(data))
		if pid > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("process did not stop")
	}
	if pid == 0 {
		t.Fatal("child did not start")
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if os.IsNotExist(err) || strings.Contains(string(data), ") Z ") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Fatal("child remained running")
}

func TestWorkspaceBDD_NamedPipeDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := readWorkspaceFile(root, "pipe"); err == nil {
		t.Fatal("accepted named pipe")
	}
}
