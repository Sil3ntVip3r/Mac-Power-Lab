// Package config owns validated runtime configuration.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Config controls monitor sampling, persistence, attribution, and helpers.
//
// System powermetrics and process/task powermetrics intentionally have separate
// intervals. A full tasks/coalitions plist is much larger than a CPU/GPU/thermal
// sample and must not be streamed at dashboard refresh frequency.
type Config struct {
	DataDir              string
	SampleInterval       time.Duration
	PowermetricsInterval time.Duration
	ProcessInterval      time.Duration
	TopApps              int
	AppAttribution       bool
	SQLite               bool
	LegacyDir            string
	NativeDir            string
	NativeBinDir         string
	NoColor              bool
}

// Default returns safe production defaults for a local interactive run.
func Default() Config {
	home, _ := os.UserHomeDir()
	data := filepath.Join(home, "Library", "Application Support", "MacPowerLab")
	if runtime.GOOS != "darwin" {
		data = filepath.Join(os.TempDir(), "MacPowerLab")
	}
	return Config{
		DataDir:              data,
		SampleInterval:       time.Second,
		PowermetricsInterval: 2 * time.Second,
		ProcessInterval:      10 * time.Second,
		TopApps:              12,
		AppAttribution:       true,
		SQLite:               true,
		LegacyDir:            "legacy",
		NativeDir:            "native",
		NativeBinDir:         "bin/native",
	}
}

// Validate rejects unsafe or nonsensical settings before collection begins.
func (c Config) Validate() error {
	if c.DataDir == "" {
		return errors.New("data directory must not be empty")
	}
	if c.SampleInterval < 250*time.Millisecond || c.SampleInterval > time.Minute {
		return fmt.Errorf("sample interval must be between 250ms and 1m: %s", c.SampleInterval)
	}
	if c.PowermetricsInterval < time.Second || c.PowermetricsInterval > time.Minute {
		return fmt.Errorf("powermetrics interval must be between 1s and 1m: %s", c.PowermetricsInterval)
	}
	if c.ProcessInterval < 2*time.Second || c.ProcessInterval > 5*time.Minute {
		return fmt.Errorf("process interval must be between 2s and 5m: %s", c.ProcessInterval)
	}
	if c.TopApps < 1 || c.TopApps > 100 {
		return fmt.Errorf("top apps must be between 1 and 100: %d", c.TopApps)
	}
	if c.NativeDir == "" {
		return errors.New("native source directory must not be empty")
	}
	if c.NativeBinDir == "" {
		return errors.New("native binary directory must not be empty")
	}
	if filepath.Clean(c.DataDir) == string(filepath.Separator) {
		return errors.New("refusing to use filesystem root as data directory")
	}
	return nil
}

// Prepare creates the private data directory and validates permissions.
func (c Config) Prepare() error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(c.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	if err := os.Chmod(c.DataDir, 0o700); err != nil && !errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("set data directory permissions: %w", err)
	}
	return nil
}
