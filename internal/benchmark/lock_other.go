//go:build !unix

package benchmark

import (
	"errors"
	"os"
)

func acquireLock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, errors.New("benchmark lock exists")
	}
	_ = f.Close()
	return func() { _ = os.Remove(path) }, nil
}
