package execx

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func TestRunAndLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r, err := Run(ctx, 4, "/bin/sh", "-c", "printf 123456")
	var limitErr *OutputLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("err=%v, want OutputLimitError", err)
	}
	if string(r.Stdout) != "1234" {
		t.Fatalf("stdout=%q", r.Stdout)
	}
}
func TestTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := Run(ctx, 1024, "/bin/sh", "-c", "sleep 2"); err == nil {
		t.Fatal("expected timeout")
	}
}

func TestReadAllLimitedContinuesDraining(t *testing.T) {
	reader := &countingReader{remaining: 1024}
	data, truncated, err := ReadAllLimited(reader, 16)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || len(data) != 16 {
		t.Fatalf("len=%d truncated=%v", len(data), truncated)
	}
	if reader.remaining != 0 {
		t.Fatalf("reader was not fully drained: %d bytes remain", reader.remaining)
	}
}

type countingReader struct{ remaining int }

func (r *countingReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := len(p)
	if n > r.remaining {
		n = r.remaining
	}
	for i := 0; i < n; i++ {
		p[i] = 'x'
	}
	r.remaining -= n
	return n, nil
}

func TestRunRejectsNilContext(t *testing.T) {
	if _, err := Run(nil, 1024, "/bin/true"); err == nil {
		t.Fatal("expected nil-context error")
	}
}
