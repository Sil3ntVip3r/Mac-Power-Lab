//go:build unix

package benchmark

import (
	"path/filepath"
	"testing"
)

func TestAcquireLockUsesAdvisoryLifetimeLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "benchmark.lock")
	release, err := acquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acquireLock(path); err == nil {
		t.Fatal("expected second lock acquisition to fail")
	}
	release()
	releaseAgain, err := acquireLock(path)
	if err != nil {
		t.Fatalf("lock could not be reacquired after release: %v", err)
	}
	releaseAgain()
}
