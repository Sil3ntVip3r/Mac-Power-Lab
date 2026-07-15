// Package priority controls ordinary macOS process niceness. It deliberately
// does not expose real-time, deadline, or kernel scheduling policies.
package priority

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/execx"
)

const (
	MinimumNice = -5
	MaximumNice = 10
)

var processPriorityMu sync.Mutex

// ValidateNice enforces the product-supported ordinary nice range.
func ValidateNice(value int) error {
	if value < MinimumNice || value > MaximumNice {
		return fmt.Errorf("process nice must be between %d and %d: %d", MinimumNice, MaximumNice, value)
	}
	return nil
}

// Set changes the current process's ordinary nice value on macOS. Other
// platforms validate and no-op so parser/report CI remains portable.
func Set(ctx context.Context, value int) error {
	if ctx == nil {
		return errors.New("priority context must not be nil")
	}
	if err := ValidateNice(value); err != nil {
		return err
	}
	processPriorityMu.Lock()
	defer processPriorityMu.Unlock()
	return setCurrent(ctx, value)
}

type setter func(context.Context, int) error
type starter func(context.Context, string, ...string) (*exec.Cmd, io.ReadCloser, io.ReadCloser, error)

// StartNormalized launches one benchmark child at ordinary nice 0 even when
// the backend itself is configured above or below zero. The process-wide
// priority transition is serialized and restored immediately after fork/exec.
func StartNormalized(
	ctx context.Context,
	configuredNice int,
	name string,
	args ...string,
) (*exec.Cmd, io.ReadCloser, io.ReadCloser, error) {
	return startNormalizedWith(ctx, configuredNice, name, args, setCurrent, execx.Start)
}

func startNormalizedWith(
	ctx context.Context,
	configuredNice int,
	name string,
	args []string,
	set setter,
	start starter,
) (*exec.Cmd, io.ReadCloser, io.ReadCloser, error) {
	if ctx == nil {
		return nil, nil, nil, errors.New("priority context must not be nil")
	}
	if err := ValidateNice(configuredNice); err != nil {
		return nil, nil, nil, err
	}
	if configuredNice == 0 {
		return start(ctx, name, args...)
	}

	processPriorityMu.Lock()
	defer processPriorityMu.Unlock()
	if err := set(ctx, 0); err != nil {
		return nil, nil, nil, fmt.Errorf("normalize backend priority for benchmark child: %w", err)
	}

	cmd, stdout, stderr, startErr := start(ctx, name, args...)
	restoreCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	restoreErr := set(restoreCtx, configuredNice)
	cancel()
	if restoreErr == nil {
		return cmd, stdout, stderr, startErr
	}

	if stdout != nil {
		_ = stdout.Close()
	}
	if stderr != nil {
		_ = stderr.Close()
	}
	if cmd != nil {
		_ = execx.StopGroup(cmd, time.Second)
	}
	return nil, nil, nil, errors.Join(
		startErr,
		fmt.Errorf("restore configured backend priority %d: %w", configuredNice, restoreErr),
	)
}
