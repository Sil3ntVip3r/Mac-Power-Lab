package legacy

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestReadLatestCSVRow(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "..", "..", "testdata", "golden", "legacy.csv")
	s, err := ReadLatestCSVRow(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Battery.Percent != 80 || s.PrimaryLoadW != 48.2 {
		t.Fatalf("sample=%+v", s)
	}
}
