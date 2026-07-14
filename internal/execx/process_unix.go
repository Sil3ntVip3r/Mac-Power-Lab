//go:build unix

package execx

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = defaultWaitDelay
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return normalizeSignalError(syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM))
	}
}

func signalProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return normalizeSignalError(syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM))
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = normalizeSignalError(syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL))
}

func normalizeSignalError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
