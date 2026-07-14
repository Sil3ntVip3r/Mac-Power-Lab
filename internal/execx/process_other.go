//go:build !unix

package execx

import "os/exec"

func configureProcess(cmd *exec.Cmd) {
	cmd.WaitDelay = defaultWaitDelay
}

func signalProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if err := cmd.Process.Kill(); err != nil {
		return err
	}
	return nil
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
