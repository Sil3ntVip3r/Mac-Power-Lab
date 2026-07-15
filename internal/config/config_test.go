package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/model"
)

func TestDefaultProcessIntervalIsLowerFrequencyThanSystemSampler(t *testing.T) {
	c := Default()
	if c.Runtime.AppAttributionMS <= c.Runtime.PowermetricsMS {
		t.Fatalf(
			"app attribution interval %dms must be slower than system sampler %dms",
			c.Runtime.AppAttributionMS,
			c.Runtime.PowermetricsMS,
		)
	}
}

func TestRejectTooFrequentProcessSampling(t *testing.T) {
	c := Default()
	c.Runtime.Profile = ProfileCustom
	c.Runtime.AppAttributionMS = 1000
	if err := c.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestRuntimeProfilesAreCompleteAndValid(t *testing.T) {
	profiles := Profiles()
	want := []string{
		ProfileDefault,
		ProfileHighResponsiveness,
		ProfileBalanced,
		ProfileLowOverhead,
		ProfileLiveOnly,
		ProfileCustom,
	}
	if len(profiles) != len(want) {
		t.Fatalf("profiles=%d want=%d", len(profiles), len(want))
	}
	for index, profile := range profiles {
		if profile.ID != want[index] {
			t.Fatalf("profile[%d]=%q want=%q", index, profile.ID, want[index])
		}
		if profile.Name == "" || profile.Description == "" {
			t.Fatalf("incomplete profile: %+v", profile)
		}
		if err := ValidateRuntimeSettings(profile.Settings); err != nil {
			t.Fatalf("profile %s: %v", profile.ID, err)
		}
	}
	if profiles[4].Settings.LoggingEnabled || profiles[4].Settings.LogIntervalMS != 0 {
		t.Fatalf("live-only settings=%+v", profiles[4].Settings)
	}
}

func TestRuntimeSettingsStrictRanges(t *testing.T) {
	valid := DefaultRuntimeSettings()
	valid.Profile = ProfileCustom
	tests := []struct {
		name   string
		mutate func(*model.RuntimeSettings)
	}{
		{"ui too fast", func(s *model.RuntimeSettings) { s.UIRefreshMS = 499 }},
		{"ui too slow", func(s *model.RuntimeSettings) { s.UIRefreshMS = 60001 }},
		{"battery too fast", func(s *model.RuntimeSettings) { s.BatteryCollectionMS = 499 }},
		{"battery too slow", func(s *model.RuntimeSettings) { s.BatteryCollectionMS = 60001 }},
		{"powermetrics too fast", func(s *model.RuntimeSettings) { s.PowermetricsMS = 999 }},
		{"powermetrics too slow", func(s *model.RuntimeSettings) { s.PowermetricsMS = 60001 }},
		{"apps too fast", func(s *model.RuntimeSettings) { s.AppAttributionMS = 1999 }},
		{"apps too slow", func(s *model.RuntimeSettings) { s.AppAttributionMS = 60001 }},
		{"logging too fast", func(s *model.RuntimeSettings) { s.LogIntervalMS = 499 }},
		{"logging too slow", func(s *model.RuntimeSettings) { s.LogIntervalMS = 60001 }},
		{"nice too high", func(s *model.RuntimeSettings) { s.ProcessNice = 11 }},
		{"nice too low", func(s *model.RuntimeSettings) { s.ProcessNice = -6 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := valid
			test.mutate(&settings)
			if err := ValidateRuntimeSettings(settings); err == nil {
				t.Fatalf("settings unexpectedly valid: %+v", settings)
			}
		})
	}

	for _, value := range []int64{500, 60000} {
		settings := valid
		settings.UIRefreshMS = value
		settings.BatteryCollectionMS = value
		settings.LogIntervalMS = value
		if err := ValidateRuntimeSettings(settings); err != nil {
			t.Fatalf("inclusive boundary %d rejected: %v", value, err)
		}
	}
	for _, settings := range []model.RuntimeSettings{
		func() model.RuntimeSettings {
			value := valid
			value.PowermetricsMS = 1000
			value.AppAttributionMS = 2000
			value.ProcessNice = -5
			return value
		}(),
		func() model.RuntimeSettings {
			value := valid
			value.PowermetricsMS = 60000
			value.AppAttributionMS = 60000
			value.ProcessNice = 10
			return value
		}(),
	} {
		if err := ValidateRuntimeSettings(settings); err != nil {
			t.Fatalf("inclusive settings rejected: %+v: %v", settings, err)
		}
	}
}

func TestRuntimeSettingsRequireHonestProfileAndLoggingState(t *testing.T) {
	settings := DefaultRuntimeSettings()
	settings.UIRefreshMS++
	if err := ValidateRuntimeSettings(settings); err == nil || !strings.Contains(err.Error(), "use profile") {
		t.Fatalf("preset mismatch error=%v", err)
	}
	ReconcileProfile(&settings)
	if settings.Profile != ProfileCustom {
		t.Fatalf("profile=%q want custom", settings.Profile)
	}
	if err := ValidateRuntimeSettings(settings); err != nil {
		t.Fatal(err)
	}

	settings.LoggingEnabled = false
	if err := ValidateRuntimeSettings(settings); err == nil {
		t.Fatal("disabled logging with a non-zero interval must fail")
	}
	settings.LogIntervalMS = 0
	if err := ValidateRuntimeSettings(settings); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeSettingsPersistenceIsAtomicPrivateAndStrict(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "private")
	settings, err := SettingsForProfile(ProfileBalanced)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveRuntimeSettings(dataDir, settings); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(SettingsPath(dataDir))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("settings mode=%04o want=0600", got)
	}
	directoryInfo, err := os.Stat(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := directoryInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("settings directory mode=%04o want=0700", got)
	}
	loaded, found, err := LoadRuntimeSettings(dataDir)
	if err != nil || !found || !RuntimeSettingsEqual(loaded, settings) {
		t.Fatalf("loaded=%+v found=%t err=%v", loaded, found, err)
	}
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("temporary file remained: %s", entry.Name())
		}
	}

	settings, _ = SettingsForProfile(ProfileLowOverhead)
	if err := SaveRuntimeSettings(dataDir, settings); err != nil {
		t.Fatal(err)
	}
	loaded, found, err = LoadRuntimeSettings(dataDir)
	if err != nil || !found || loaded.Profile != ProfileLowOverhead {
		t.Fatalf("atomic replacement loaded=%+v found=%t err=%v", loaded, found, err)
	}
}

func TestRuntimeSettingsLoadRejectsUnknownFieldsAndSymlinks(t *testing.T) {
	dataDir := t.TempDir()
	settings := DefaultRuntimeSettings()
	data := []byte(`{"schema":"` + settings.Schema + `","profile":"default","ui_refresh_ms":1000,"battery_collection_ms":1000,"powermetrics_ms":2000,"app_attribution_ms":10000,"logging_enabled":true,"log_interval_ms":1000,"process_nice":0,"unexpected":true}`)
	if err := os.WriteFile(SettingsPath(dataDir), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadRuntimeSettings(dataDir); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown-field error=%v", err)
	}

	if err := os.Remove(SettingsPath(dataDir)); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dataDir, "target.json")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, SettingsPath(dataDir)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadRuntimeSettings(dataDir); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("symlink error=%v", err)
	}
}

func TestMissingRuntimeSettingsUseDefaultWithoutCreatingFile(t *testing.T) {
	dataDir := t.TempDir()
	settings, found, err := LoadRuntimeSettings(dataDir)
	if err != nil || found || settings.Profile != ProfileDefault {
		t.Fatalf("settings=%+v found=%t err=%v", settings, found, err)
	}
	if _, err := os.Stat(SettingsPath(dataDir)); !os.IsNotExist(err) {
		t.Fatalf("default load unexpectedly created settings: %v", err)
	}
}
