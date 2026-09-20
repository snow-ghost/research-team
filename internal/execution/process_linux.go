//go:build linux

package execution

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

type boundedOutput struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
	cancel   context.CancelFunc
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if n > b.limit-b.buffer.Len() {
		b.exceeded = true
		p = p[:b.limit-b.buffer.Len()]
		b.cancel()
	}
	_, _ = b.buffer.Write(p)
	return n, nil
}

func runProcess(ctx context.Context, executable string, args, env []string, cwd, input string, limit int) ([]byte, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir, cmd.Env, cmd.Stdin = cwd, env, bytes.NewBufferString(input)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	stdout := &boundedOutput{limit: limit, cancel: cancel}
	stderr := &boundedOutput{limit: limit, cancel: cancel}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	// Also clean up children retaining inherited pipes after the parent exits.
	// Detached sessions require a container/cgroup supervisor outside this runner.
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	if stdout.exceeded || stderr.exceeded {
		return nil, ErrLimit
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, errors.New("external process failed; diagnostic output withheld")
	}
	return stdout.buffer.Bytes(), nil
}

func openRegular(root *os.Root, path string) (*os.File, error) {
	return root.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
