//go:build unix

package benchmark

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"
)

type lockMetadata struct {
	PID       int       `json:"pid"`
	CreatedAt time.Time `json:"created_at"`
}

// acquireLock holds an OS advisory lock for the complete benchmark lifetime.
// Unlike PID probing, flock is not vulnerable to stale files or PID reuse.
func acquireLock(path string) (func(), error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open benchmark lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, errors.New("another benchmark is already running")
		}
		return nil, fmt.Errorf("lock benchmark: %w", err)
	}
	metadata, _ := json.Marshal(lockMetadata{PID: os.Getpid(), CreatedAt: time.Now().UTC()})
	if err := file.Truncate(0); err != nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
		return nil, err
	}
	if _, err := file.WriteAt(metadata, 0); err != nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
		return nil, err
	}
	if err := file.Sync(); err != nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
		return nil, err
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
			_ = file.Close()
		})
	}, nil
}
