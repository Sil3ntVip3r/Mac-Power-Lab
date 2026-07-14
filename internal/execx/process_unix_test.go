//go:build unix

package execx

import "testing"

func TestNormalizeSignalErrorPreservesSuccess(t *testing.T) {
	if err := normalizeSignalError(nil); err != nil {
		t.Fatalf("nil signal error became %v", err)
	}
}
