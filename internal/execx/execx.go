// Package execx provides bounded, context-aware subprocess execution.
package execx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	defaultMaxOutput = 32 << 20
	maxErrorOutput   = 16 << 10
	defaultWaitDelay = 3 * time.Second
)

// Result is a completed process invocation.
//
// The output slices contain, at most, the configured capture limit. Callers
// must inspect the truncation fields before attempting to parse machine-readable
// output.
type Result struct {
	Stdout          []byte
	Stderr          []byte
	StdoutTruncated bool
	StderrTruncated bool
	ExitCode        int
	Duration        time.Duration
}

// OutputLimitError reports that a subprocess exceeded a configured capture
// limit. Partial output is available in Result, but must not be parsed as a
// complete document.
type OutputLimitError struct {
	Command         string
	StdoutTruncated bool
	StderrTruncated bool
}

func (e *OutputLimitError) Error() string {
	parts := make([]string, 0, 2)
	if e.StdoutTruncated {
		parts = append(parts, "stdout")
	}
	if e.StderrTruncated {
		parts = append(parts, "stderr")
	}
	return fmt.Sprintf("%s output exceeded capture limit: %s", e.Command, strings.Join(parts, ", "))
}

// LimitedBuffer bounds retained subprocess output while continuing to accept
// and discard bytes beyond the limit. Continuing to drain is essential: if the
// reader stops at the limit, a noisy child can block forever on a full pipe.
type LimitedBuffer struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

// NewLimitedBuffer creates a prefix-retaining writer. Non-positive limits use
// a conservative production default.
func NewLimitedBuffer(max int) *LimitedBuffer {
	if max <= 0 {
		max = defaultMaxOutput
	}
	return &LimitedBuffer{max: max}
}

// Write implements io.Writer. It reports all input bytes as consumed even when
// only a prefix is retained, allowing io.Copy and os/exec to keep draining.
func (b *LimitedBuffer) Write(p []byte) (int, error) {
	original := len(p)
	if b.buf.Len() >= b.max {
		b.truncated = true
		return original, nil
	}
	remain := b.max - b.buf.Len()
	if len(p) > remain {
		_, _ = b.buf.Write(p[:remain])
		b.truncated = true
		return original, nil
	}
	_, _ = b.buf.Write(p)
	return original, nil
}

// Bytes returns the retained prefix. The returned slice remains valid until the
// buffer is written again.
func (b *LimitedBuffer) Bytes() []byte { return b.buf.Bytes() }

// Truncated reports whether input was discarded after the retained prefix.
func (b *LimitedBuffer) Truncated() bool { return b.truncated }

// ReadAllLimited reads an entire stream while retaining at most max bytes. It
// differs from io.LimitReader by continuing to drain the source after the
// retention limit, preventing subprocess pipe deadlocks.
func ReadAllLimited(r io.Reader, max int) ([]byte, bool, error) {
	if r == nil {
		return nil, false, nil
	}
	buf := NewLimitedBuffer(max)
	_, err := io.Copy(buf, r)
	return bytes.Clone(buf.Bytes()), buf.Truncated(), err
}

// Run executes a command directly without a shell or glob expansion.
func Run(ctx context.Context, maxOutput int, name string, args ...string) (Result, error) {
	if name == "" {
		return Result{}, errors.New("command name must not be empty")
	}
	if ctx == nil {
		return Result{}, errors.New("context must not be nil")
	}

	start := time.Now()
	cmd := exec.CommandContext(ctx, name, args...)
	configureProcess(cmd)
	stdout := NewLimitedBuffer(maxOutput)
	stderr := NewLimitedBuffer(maxOutput)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	result := Result{
		Stdout:          bytes.Clone(stdout.Bytes()),
		Stderr:          bytes.Clone(stderr.Bytes()),
		StdoutTruncated: stdout.Truncated(),
		StderrTruncated: stderr.Truncated(),
		ExitCode:        exitCode(err),
		Duration:        time.Since(start),
	}

	if ctx.Err() != nil {
		// CommandContext invokes the configured group cancellation. Escalate once
		// more after Wait returns to avoid leaving grandchildren behind.
		killProcessGroup(cmd)
		return result, fmt.Errorf("%s cancelled: %w", name, ctx.Err())
	}
	if err != nil {
		return result, fmt.Errorf(
			"%s %v failed (exit %d): %w: %s",
			name,
			args,
			result.ExitCode,
			err,
			boundedString(result.Stderr, maxErrorOutput),
		)
	}
	if result.StdoutTruncated || result.StderrTruncated {
		return result, &OutputLimitError{
			Command:         name,
			StdoutTruncated: result.StdoutTruncated,
			StderrTruncated: result.StderrTruncated,
		}
	}
	return result, nil
}

// RunInteractive runs a command attached to the current terminal.
func RunInteractive(ctx context.Context, name string, args ...string) error {
	if name == "" {
		return errors.New("command name must not be empty")
	}
	if ctx == nil {
		return errors.New("context must not be nil")
	}
	cmd := exec.CommandContext(ctx, name, args...)
	configureProcess(cmd)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			killProcessGroup(cmd)
			return ctx.Err()
		}
		return fmt.Errorf("interactive command %s failed: %w", name, err)
	}
	return nil
}

// Start launches a long-running process and returns its command and pipes.
// The caller owns both pipes and must call Wait exactly once.
func Start(ctx context.Context, name string, args ...string) (*exec.Cmd, io.ReadCloser, io.ReadCloser, error) {
	if name == "" {
		return nil, nil, nil, errors.New("command name must not be empty")
	}
	if ctx == nil {
		return nil, nil, nil, errors.New("context must not be nil")
	}
	cmd := exec.CommandContext(ctx, name, args...)
	configureProcess(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdout.Close()
		return nil, nil, nil, fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, nil, nil, fmt.Errorf("start %s: %w", name, err)
	}
	return cmd, stdout, stderr, nil
}

// StopGroup requests graceful shutdown and escalates to kill after grace.
// It must only be used by the goroutine that owns cmd.Wait.
func StopGroup(cmd *exec.Cmd, grace time.Duration) error {
	if cmd == nil || cmd.Process == nil || cmd.ProcessState != nil {
		return nil
	}
	if grace <= 0 {
		grace = defaultWaitDelay
	}
	_ = signalProcessGroup(cmd)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case err := <-done:
		return normalizeWaitError(err)
	case <-timer.C:
		killProcessGroup(cmd)
		return normalizeWaitError(<-done)
	}
}

// EnsureSudo validates cached sudo credentials, prompting only when requested.
func EnsureSudo(ctx context.Context, interactive bool) error {
	if _, err := exec.LookPath("sudo"); err != nil {
		return fmt.Errorf("sudo not available: %w", err)
	}
	if _, err := Run(ctx, 1<<20, "sudo", "-n", "-v"); err == nil {
		return nil
	}
	if !interactive {
		return errors.New("sudo credentials are not cached; run `sudo -v` in a terminal")
	}
	return RunInteractive(ctx, "sudo", "-v")
}

func boundedString(value []byte, max int) string {
	value = bytes.TrimSpace(value)
	if len(value) <= max {
		return string(value)
	}
	return string(value[:max]) + "… [truncated]"
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func normalizeWaitError(err error) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		switch exitErr.ExitCode() {
		case -1, 130, 143:
			return nil
		}
	}
	return err
}
