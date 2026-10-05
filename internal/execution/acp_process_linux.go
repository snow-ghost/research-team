//go:build linux

package execution

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"
)

// Limits include protocol frames, not only the assistant's visible answer.
type acpBudget struct {
	remaining int
	exceeded  *atomic.Bool
	cancel    context.CancelFunc
}

func (b *acpBudget) consume(n int) bool {
	b.remaining -= n
	if b.remaining < 0 {
		b.exceeded.Store(true)
		b.cancel()
		return false
	}
	return true
}

type acpReader struct {
	reader io.Reader
	budget acpBudget
}

func (r *acpReader) Read(p []byte) (int, error) {
	if r.budget.remaining < 0 {
		return 0, ErrLimit
	}
	if len(p) > r.budget.remaining+1 {
		p = p[:r.budget.remaining+1]
	}
	n, err := r.reader.Read(p)
	if !r.budget.consume(n) {
		return 0, ErrLimit
	}
	return n, err
}

type acpDiagnostics struct {
	budget acpBudget
	tail   *diagnosticTail
}

func (w *acpDiagnostics) Write(p []byte) (int, error) {
	if w.tail != nil {
		w.tail.write(p)
	}
	w.budget.consume(len(p))
	return len(p), nil
}

func withACPProcess(parent context.Context, executable string, args, env []string, cwd string, limit int,
	exchange func(context.Context, io.Writer, io.Reader) error) error {
	return withACPProcessDiagnostics(parent, executable, args, env, cwd, limit, nil, exchange)
}

func withACPProcessDiagnostics(parent context.Context, executable string, args, env []string, cwd string, limit int,
	observe func(acpProcessReport), exchange func(context.Context, io.Writer, io.Reader) error) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	input, writer, err := os.Pipe()
	if err != nil {
		return errors.New("cannot create ACP input")
	}
	defer input.Close()
	defer writer.Close()
	reader, output, err := os.Pipe()
	if err != nil {
		return errors.New("cannot create ACP output")
	}
	defer reader.Close()
	defer output.Close()
	var exceeded atomic.Bool
	var stopRequested atomic.Bool
	tail := &diagnosticTail{}
	started := time.Now()
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir, cmd.Env, cmd.Stdin, cmd.Stdout = cwd, env, input, output
	cmd.Stderr = &acpDiagnostics{budget: acpBudget{limit, &exceeded, cancel}, tail: tail}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error {
		stopRequested.Store(true)
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	if err := cmd.Start(); err != nil {
		return errors.New("cannot start ACP process")
	}
	input.Close()
	output.Close()
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	// Closing both directions also unblocks a peer that stopped reading stdin.
	stop := context.AfterFunc(ctx, func() {
		_ = writer.Close()
		_ = reader.Close()
	})
	err = exchange(ctx, writer, &acpReader{reader, acpBudget{limit, &exceeded, cancel}})
	stopRequested.Store(true)
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	cancel()
	_ = writer.Close()
	_ = reader.Close()
	<-done
	stop()
	if observe != nil {
		text, size, truncated := tail.snapshot()
		report := acpProcessReport{stderr: text, stderrBytes: size, stderrTruncated: truncated,
			clientStopRequested: stopRequested.Load(), elapsedMS: time.Since(started).Milliseconds(), byteLimitReached: exceeded.Load()}
		if cmd.ProcessState != nil {
			code := cmd.ProcessState.ExitCode()
			report.exitCode = &code
			if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				report.signal = status.Signal().String()
			}
		}
		observe(report)
	}
	if exceeded.Load() {
		return ErrLimit
	}
	if parent.Err() != nil {
		return parent.Err()
	}
	return err
}
