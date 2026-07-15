//go:build darwin

package priority

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"syscall"

	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/execx"
)

func currentNice() (int, error) {
	value, err := syscall.Getpriority(syscall.PRIO_PROCESS, 0)
	if err != nil {
		return 0, fmt.Errorf("read process nice: %w", err)
	}
	return value, nil
}

func setCurrent(ctx context.Context, value int) error {
	current, err := currentNice()
	if err != nil {
		return err
	}
	if current == value {
		return nil
	}
	if value < current {
		_, err = execx.Run(
			ctx,
			1<<20,
			"/usr/bin/sudo",
			reniceArguments(value, os.Getpid())...,
		)
	} else {
		err = syscall.Setpriority(syscall.PRIO_PROCESS, 0, value)
	}
	if err != nil {
		return fmt.Errorf("set process nice to %d: %w", value, err)
	}
	effective, err := currentNice()
	if err != nil {
		return err
	}
	if effective != value {
		return fmt.Errorf("process nice is %d after requesting %d", effective, value)
	}
	return nil
}

func reniceArguments(value, pid int) []string {
	// BSD renice's -n form applies an increment. Use the positional priority
	// form so restoring +10 -> 0 and other non-zero transitions are absolute.
	return []string{
		"/usr/bin/renice",
		strconv.Itoa(value),
		"-p",
		strconv.Itoa(pid),
	}
}
