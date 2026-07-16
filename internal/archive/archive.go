// Package archive creates deterministic, maximum-compression log bundles.
package archive

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var reproducibleTime = time.Unix(0, 0).UTC()

type Entry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type ExcludedEntry struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type Manifest struct {
	CreatedAt time.Time       `json:"created_at"`
	Root      string          `json:"root"`
	Files     []Entry         `json:"files"`
	Excluded  []ExcludedEntry `json:"excluded,omitempty"`
}

// Create writes a reproducible tar.gz with BestCompression and a SHA-256
// manifest. Symlinks are rejected rather than followed so a session cannot
// accidentally archive files outside its root.
func Create(root, out string) (retErr error) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(out) == "" {
		return errors.New("archive root and output path are required")
	}
	root, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return fmt.Errorf("resolve archive root: %w", err)
	}
	info, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("stat archive root: %w", err)
	}
	if !info.IsDir() {
		return errors.New("archive root must be a directory")
	}

	out, err = filepath.Abs(filepath.Clean(out))
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}
	if within(root, out) {
		return errors.New("archive output must be outside the archived root")
	}

	paths, excluded, err := regularFiles(root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
		return fmt.Errorf("create archive output directory: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(out), "."+filepath.Base(out)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create archive temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		if retErr != nil {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}

	gzipWriter, err := gzip.NewWriterLevel(tmp, gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("create gzip writer: %w", err)
	}
	gzipWriter.Name = filepath.Base(root) + ".tar"
	gzipWriter.ModTime = reproducibleTime
	tarWriter := tar.NewWriter(gzipWriter)

	manifest := Manifest{
		CreatedAt: reproducibleTime,
		Root:      filepath.Base(root),
		Files:     make([]Entry, 0, len(paths)),
		Excluded:  excluded,
	}

	closeWriters := func() error {
		return errors.Join(tarWriter.Close(), gzipWriter.Close(), tmp.Sync(), tmp.Close())
	}

	for _, path := range paths {
		entry, err := writeRegularFile(tarWriter, root, path)
		if err != nil {
			_ = closeWriters()
			return err
		}
		manifest.Files = append(manifest.Files, entry)
	}

	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = closeWriters()
		return fmt.Errorf("marshal archive manifest: %w", err)
	}
	manifestHeader := &tar.Header{
		Name:     "MANIFEST_macpowerlab.json",
		Mode:     0o600,
		Size:     int64(len(manifestJSON)),
		ModTime:  reproducibleTime,
		Typeflag: tar.TypeReg,
		Format:   tar.FormatPAX,
	}
	if err := tarWriter.WriteHeader(manifestHeader); err != nil {
		_ = closeWriters()
		return fmt.Errorf("write manifest header: %w", err)
	}
	if _, err := tarWriter.Write(manifestJSON); err != nil {
		_ = closeWriters()
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := closeWriters(); err != nil {
		return fmt.Errorf("finalize archive: %w", err)
	}
	if err := os.Rename(tmpPath, out); err != nil {
		return fmt.Errorf("publish archive: %w", err)
	}
	return nil
}

func regularFiles(root string) ([]string, []ExcludedEntry, error) {
	paths := make([]string, 0, 64)
	excluded := make([]ExcludedEntry, 0, 8)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink in archive root: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing non-regular archive entry: %s", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if reason := archiveExclusionReason(relative); reason != "" {
			excluded = append(excluded, ExcludedEntry{
				Path:   filepath.ToSlash(relative),
				Reason: reason,
			})
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(paths)
	sort.Slice(excluded, func(i, j int) bool {
		return excluded[i].Path < excluded[j].Path
	})
	return paths, excluded, nil
}

func archiveExclusionReason(relative string) string {
	base := strings.ToLower(filepath.Base(relative))
	switch {
	case base == "api.token", base == "api.token.address":
		return "local API credential"
	case strings.Contains(base, "token"):
		return "potential credential"
	case base == "launch macpowerlab backend.command":
		return "local launcher may contain private paths or credentials"
	case base == ".ds_store":
		return "Finder metadata"
	case strings.HasSuffix(base, ".sqlite3-wal"), strings.HasSuffix(base, ".sqlite3-shm"):
		return "transient SQLite sidecar"
	case strings.HasSuffix(base, ".pem"), strings.HasSuffix(base, ".key"), strings.HasSuffix(base, ".p12"):
		return "private key material"
	default:
		return ""
	}
}

func writeRegularFile(writer *tar.Writer, root, path string) (Entry, error) {
	expected, err := os.Lstat(path)
	if err != nil {
		return Entry{}, err
	}
	if !expected.Mode().IsRegular() {
		return Entry{}, fmt.Errorf("archive entry changed type during collection: %s", path)
	}

	source, opened, err := openExpectedRegular(path, expected)
	if err != nil {
		return Entry{}, err
	}
	defer source.Close()

	relative, err := filepath.Rel(filepath.Dir(root), path)
	if err != nil {
		return Entry{}, err
	}
	header := &tar.Header{
		Name:     filepath.ToSlash(relative),
		Mode:     int64(opened.Mode().Perm()),
		Size:     opened.Size(),
		ModTime:  reproducibleTime,
		Typeflag: tar.TypeReg,
		Format:   tar.FormatPAX,
	}
	if err := writer.WriteHeader(header); err != nil {
		return Entry{}, err
	}

	hash := sha256.New()
	// Capture exactly the size observed after opening. Active JSONL and log files
	// may continue growing while a support bundle is created; later bytes belong
	// to a future bundle and must not make this snapshot fail or become unbounded.
	written, copyErr := io.CopyN(io.MultiWriter(writer, hash), source, opened.Size())
	if copyErr != nil {
		return Entry{}, copyErr
	}
	if written != opened.Size() {
		return Entry{}, fmt.Errorf("archive entry changed size while reading: %s", path)
	}
	return Entry{
		Path:   header.Name,
		Size:   written,
		SHA256: hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

func openExpectedRegular(path string, expected os.FileInfo) (*os.File, os.FileInfo, error) {
	source, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	opened, statErr := source.Stat()
	if statErr != nil {
		_ = source.Close()
		return nil, nil, statErr
	}
	if !opened.Mode().IsRegular() || !os.SameFile(expected, opened) {
		_ = source.Close()
		return nil, nil, fmt.Errorf("archive entry changed identity before open: %s", path)
	}
	return source, opened, nil
}

func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

// DefaultName produces a timestamped archive name.
func DefaultName(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "macpowerlab_logs"
	}
	return fmt.Sprintf("%s_%s.tar.gz", prefix, time.Now().Format("20060102_150405"))
}
