package archive

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCreate(t *testing.T) {
	root := filepath.Join(t.TempDir(), "session")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out.tar.gz")
	if err := Create(root, out); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(out); err != nil || info.Size() == 0 {
		t.Fatalf("archive err=%v info=%v", err, info)
	}
}

func TestCreateIsReproducible(t *testing.T) {
	root := filepath.Join(t.TempDir(), "session")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(t.TempDir(), "first.tar.gz")
	second := filepath.Join(t.TempDir(), "second.tar.gz")
	if err := Create(root, first); err != nil {
		t.Fatal(err)
	}
	if err := Create(root, second); err != nil {
		t.Fatal(err)
	}
	left, _ := os.ReadFile(first)
	right, _ := os.ReadFile(second)
	if !bytes.Equal(left, right) {
		t.Fatal("archives differ for identical input")
	}
}

func TestCreateRejectsSymlinkWithoutReplacingOutput(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "session")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(base, "out.tar.gz")
	if err := os.WriteFile(out, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Create(root, out); err == nil {
		t.Fatal("expected symlink rejection")
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "existing" {
		t.Fatalf("existing output was replaced: %q", data)
	}
}

func TestOpenExpectedRegularRejectsPathReplacement(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "entry")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	expected, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if file, _, err := openExpectedRegular(path, expected); err == nil {
		_ = file.Close()
		t.Fatal("expected identity mismatch")
	}
}
