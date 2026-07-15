package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/model"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/version"
)

const (
	ProfileDefault            = "default"
	ProfileHighResponsiveness = "high_responsiveness"
	ProfileBalanced           = "balanced"
	ProfileLowOverhead        = "low_overhead"
	ProfileLiveOnly           = "live_only"
	ProfileCustom             = "custom"

	runtimeSettingsFile = "runtime-settings.json"
	maxSettingsBytes    = 1 << 20
)

// ProfileDefinition is the stable settings API representation of one preset.
type ProfileDefinition struct {
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Settings    model.RuntimeSettings `json:"settings"`
}

var profileDefinitions = []ProfileDefinition{
	{
		ID:          ProfileDefault,
		Name:        "Default",
		Description: "Compatibility profile matching the original one-second live and durable sample behavior.",
		Settings:    runtimeSettings(ProfileDefault, 1000, 1000, 2000, 10000, true, 1000, 0),
	},
	{
		ID:          ProfileHighResponsiveness,
		Name:        "High responsiveness",
		Description: "Fast live updates and collection with one-second durable logging and elevated ordinary process priority.",
		Settings:    runtimeSettings(ProfileHighResponsiveness, 500, 500, 1000, 2000, true, 1000, -5),
	},
	{
		ID:          ProfileBalanced,
		Name:        "Balanced",
		Description: "Responsive live monitoring with a five-second durable history cadence.",
		Settings:    runtimeSettings(ProfileBalanced, 1000, 2000, 3000, 10000, true, 5000, 0),
	},
	{
		ID:          ProfileLowOverhead,
		Name:        "Low overhead",
		Description: "Reduced collection frequency, thirty-second logging, and lower ordinary process priority.",
		Settings:    runtimeSettings(ProfileLowOverhead, 2000, 5000, 10000, 30000, true, 30000, 10),
	},
	{
		ID:          ProfileLiveOnly,
		Name:        "Live only",
		Description: "Default live monitoring without durable power or application sample records.",
		Settings:    runtimeSettings(ProfileLiveOnly, 1000, 1000, 2000, 10000, false, 0, 0),
	},
	{
		ID:          ProfileCustom,
		Name:        "Custom",
		Description: "Individually configured values within the supported safety bounds.",
		Settings:    runtimeSettings(ProfileCustom, 1000, 1000, 2000, 10000, true, 1000, 0),
	},
}

func runtimeSettings(
	profile string,
	uiRefreshMS, batteryCollectionMS, powermetricsMS, appAttributionMS int64,
	loggingEnabled bool,
	logIntervalMS int64,
	processNice int,
) model.RuntimeSettings {
	return model.RuntimeSettings{
		Schema:              version.RuntimeSettingsSchema,
		Profile:             profile,
		UIRefreshMS:         uiRefreshMS,
		BatteryCollectionMS: batteryCollectionMS,
		PowermetricsMS:      powermetricsMS,
		AppAttributionMS:    appAttributionMS,
		LoggingEnabled:      loggingEnabled,
		LogIntervalMS:       logIntervalMS,
		ProcessNice:         processNice,
	}
}

// DefaultRuntimeSettings preserves the v1.4 monitor's effective cadence.
func DefaultRuntimeSettings() model.RuntimeSettings {
	settings, _ := SettingsForProfile(ProfileDefault)
	return settings
}

// Profiles returns the presets in stable user-facing order.
func Profiles() []ProfileDefinition {
	return append([]ProfileDefinition(nil), profileDefinitions...)
}

// SettingsForProfile returns a complete preset by its stable identifier.
func SettingsForProfile(profile string) (model.RuntimeSettings, error) {
	profile = strings.TrimSpace(profile)
	for _, definition := range profileDefinitions {
		if definition.ID == profile {
			return definition.Settings, nil
		}
	}
	return model.RuntimeSettings{}, fmt.Errorf("unknown runtime profile %q", profile)
}

// ValidateRuntimeSettings applies strict schema, profile, range, and preset
// consistency checks. Non-custom profiles cannot claim preset semantics while
// carrying silently modified values.
func ValidateRuntimeSettings(settings model.RuntimeSettings) error {
	if settings.Schema != version.RuntimeSettingsSchema {
		return fmt.Errorf("schema must be %q", version.RuntimeSettingsSchema)
	}
	if _, err := SettingsForProfile(settings.Profile); err != nil {
		return err
	}
	if settings.UIRefreshMS < 500 || settings.UIRefreshMS > 60000 {
		return fmt.Errorf("UI refresh must be between 500ms and 60s: %dms", settings.UIRefreshMS)
	}
	if settings.BatteryCollectionMS < 500 || settings.BatteryCollectionMS > 60000 {
		return fmt.Errorf("battery collection must be between 500ms and 60s: %dms", settings.BatteryCollectionMS)
	}
	if settings.PowermetricsMS < 1000 || settings.PowermetricsMS > 60000 {
		return fmt.Errorf("powermetrics must be between 1s and 60s: %dms", settings.PowermetricsMS)
	}
	if settings.AppAttributionMS < 2000 || settings.AppAttributionMS > 60000 {
		return fmt.Errorf("app attribution must be between 2s and 60s: %dms", settings.AppAttributionMS)
	}
	if settings.LoggingEnabled {
		if settings.LogIntervalMS < 500 || settings.LogIntervalMS > 60000 {
			return fmt.Errorf("log interval must be between 500ms and 60s when logging is enabled: %dms", settings.LogIntervalMS)
		}
	} else if settings.LogIntervalMS != 0 {
		return errors.New("log interval must be 0 when logging is disabled")
	}
	if settings.ProcessNice < -5 || settings.ProcessNice > 10 {
		return fmt.Errorf("process nice must be between -5 and 10: %d", settings.ProcessNice)
	}
	if settings.Profile != ProfileCustom {
		preset, _ := SettingsForProfile(settings.Profile)
		if !RuntimeSettingsEqual(settings, preset) {
			return fmt.Errorf("profile %q values do not match its preset; use profile %q for overrides", settings.Profile, ProfileCustom)
		}
	}
	return nil
}

// RuntimeSettingsEqual compares the complete persisted/API representation.
func RuntimeSettingsEqual(left, right model.RuntimeSettings) bool {
	return left == right
}

// ReconcileProfile marks a modified preset as Custom while preserving an
// explicitly custom profile even when its values happen to match a preset.
func ReconcileProfile(settings *model.RuntimeSettings) {
	if settings == nil || settings.Profile == ProfileCustom {
		return
	}
	preset, err := SettingsForProfile(settings.Profile)
	if err != nil || !RuntimeSettingsEqual(*settings, preset) {
		settings.Profile = ProfileCustom
	}
}

// SettingsPath returns the single private persisted-settings location.
func SettingsPath(dataDir string) string {
	return filepath.Join(dataDir, runtimeSettingsFile)
}

// LoadRuntimeSettings strictly loads a saved settings document. A missing file
// is not an error and returns the compatibility-preserving default.
func LoadRuntimeSettings(dataDir string) (model.RuntimeSettings, bool, error) {
	if strings.TrimSpace(dataDir) == "" {
		return model.RuntimeSettings{}, false, errors.New("data directory must not be empty")
	}
	path := SettingsPath(dataDir)
	expected, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultRuntimeSettings(), false, nil
	}
	if err != nil {
		return model.RuntimeSettings{}, false, fmt.Errorf("inspect runtime settings: %w", err)
	}
	if !expected.Mode().IsRegular() {
		return model.RuntimeSettings{}, false, errors.New("runtime settings must be a regular file")
	}
	if expected.Mode().Perm()&0o077 != 0 {
		return model.RuntimeSettings{}, false, fmt.Errorf("runtime settings must not grant group or other access, got %04o", expected.Mode().Perm())
	}
	if expected.Size() > maxSettingsBytes {
		return model.RuntimeSettings{}, false, fmt.Errorf("runtime settings exceed %d bytes", maxSettingsBytes)
	}

	file, err := os.Open(path)
	if err != nil {
		return model.RuntimeSettings{}, false, fmt.Errorf("open runtime settings: %w", err)
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil {
		return model.RuntimeSettings{}, false, fmt.Errorf("stat runtime settings: %w", err)
	}
	if !os.SameFile(expected, actual) {
		return model.RuntimeSettings{}, false, errors.New("runtime settings changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxSettingsBytes+1))
	if err != nil {
		return model.RuntimeSettings{}, false, fmt.Errorf("read runtime settings: %w", err)
	}
	if len(data) > maxSettingsBytes {
		return model.RuntimeSettings{}, false, fmt.Errorf("runtime settings exceed %d bytes", maxSettingsBytes)
	}

	var settings model.RuntimeSettings
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&settings); err != nil {
		return model.RuntimeSettings{}, false, fmt.Errorf("decode runtime settings: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return model.RuntimeSettings{}, false, errors.New("runtime settings must contain exactly one JSON value")
	}
	if err := ValidateRuntimeSettings(settings); err != nil {
		return model.RuntimeSettings{}, false, fmt.Errorf("validate runtime settings: %w", err)
	}
	return settings, true, nil
}

// SaveRuntimeSettings atomically publishes a private settings document and
// synchronizes both the file and containing directory before returning.
func SaveRuntimeSettings(dataDir string, settings model.RuntimeSettings) error {
	if err := ValidateRuntimeSettings(settings); err != nil {
		return err
	}
	if strings.TrimSpace(dataDir) == "" {
		return errors.New("data directory must not be empty")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("create settings directory: %w", err)
	}
	if err := os.Chmod(dataDir, 0o700); err != nil {
		return fmt.Errorf("set settings directory permissions: %w", err)
	}
	directoryInfo, err := os.Stat(dataDir)
	if err != nil {
		return fmt.Errorf("inspect settings directory: %w", err)
	}
	if !directoryInfo.IsDir() || directoryInfo.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("settings directory must be private (0700), got %04o", directoryInfo.Mode().Perm())
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("encode runtime settings: %w", err)
	}
	data = append(data, '\n')

	path := SettingsPath(dataDir)
	tmp, err := os.CreateTemp(dataDir, "."+runtimeSettingsFile+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create runtime settings temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	closeWithError := func(cause error) error {
		return errors.Join(cause, tmp.Close())
	}
	if err := tmp.Chmod(0o600); err != nil {
		return closeWithError(err)
	}
	if _, err := tmp.Write(data); err != nil {
		return closeWithError(err)
	}
	if err := tmp.Sync(); err != nil {
		return closeWithError(err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("publish runtime settings: %w", err)
	}
	directory, err := os.Open(dataDir)
	if err != nil {
		return fmt.Errorf("open settings directory for sync: %w", err)
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if err := errors.Join(syncErr, closeErr); err != nil {
		return fmt.Errorf("sync settings directory: %w", err)
	}
	return nil
}
