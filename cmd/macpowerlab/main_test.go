package main

import (
	"flag"
	"testing"

	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/config"
)

func TestPositiveInt(t *testing.T) {
	if v, err := parsePositiveInt("12"); err != nil || v != 12 {
		t.Fatal(v, err)
	}
	if _, err := parsePositiveInt("0"); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestRuntimeFlagsApplyProfileAndIndependentOverrides(t *testing.T) {
	cfg := config.Default()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	bindings := commonFlags(fs, &cfg)
	if err := fs.Parse([]string{
		"--profile=balanced",
		"--ui-refresh=750ms",
		"--battery-interval=3s",
		"--process-nice=2",
	}); err != nil {
		t.Fatal(err)
	}
	if err := bindings.apply(fs); err != nil {
		t.Fatal(err)
	}
	if cfg.Runtime.Profile != config.ProfileCustom {
		t.Fatalf("profile=%q want=%q", cfg.Runtime.Profile, config.ProfileCustom)
	}
	if cfg.Runtime.UIRefreshMS != 750 || cfg.Runtime.BatteryCollectionMS != 3000 {
		t.Fatalf("independent intervals were not applied: %+v", cfg.Runtime)
	}
	if cfg.Runtime.PowermetricsMS != 3000 || cfg.Runtime.LogIntervalMS != 5000 {
		t.Fatalf("unmodified balanced values were not retained: %+v", cfg.Runtime)
	}
	if cfg.Runtime.ProcessNice != 2 {
		t.Fatalf("process nice=%d want=2", cfg.Runtime.ProcessNice)
	}
}

func TestLegacyIntervalOverridesUIAndBattery(t *testing.T) {
	cfg := config.Default()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	bindings := commonFlags(fs, &cfg)
	if err := fs.Parse([]string{"--interval=2500ms"}); err != nil {
		t.Fatal(err)
	}
	if err := bindings.apply(fs); err != nil {
		t.Fatal(err)
	}
	if cfg.Runtime.UIRefreshMS != 2500 || cfg.Runtime.BatteryCollectionMS != 2500 {
		t.Fatalf("legacy interval did not preserve coupled behavior: %+v", cfg.Runtime)
	}
	if cfg.Runtime.Profile != config.ProfileCustom {
		t.Fatalf("profile=%q want custom", cfg.Runtime.Profile)
	}
}

func TestCustomProfilePreservesLoadedValues(t *testing.T) {
	cfg := config.Default()
	loaded, err := config.SettingsForProfile(config.ProfileLowOverhead)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Runtime = loaded
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	bindings := commonFlags(fs, &cfg)
	if err := fs.Parse([]string{"--profile=custom", "--ui-refresh=1500ms"}); err != nil {
		t.Fatal(err)
	}
	if err := bindings.apply(fs); err != nil {
		t.Fatal(err)
	}
	if cfg.Runtime.Profile != config.ProfileCustom || cfg.Runtime.UIRefreshMS != 1500 {
		t.Fatalf("custom profile was not applied: %+v", cfg.Runtime)
	}
	if cfg.Runtime.BatteryCollectionMS != loaded.BatteryCollectionMS ||
		cfg.Runtime.LogIntervalMS != loaded.LogIntervalMS ||
		cfg.Runtime.ProcessNice != loaded.ProcessNice {
		t.Fatalf("custom profile discarded loaded values: got=%+v loaded=%+v", cfg.Runtime, loaded)
	}
}

func TestLiveOnlyProfileAndLoggingOverride(t *testing.T) {
	t.Run("preset", func(t *testing.T) {
		cfg := config.Default()
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		bindings := commonFlags(fs, &cfg)
		if err := fs.Parse([]string{"--profile=live-only"}); err != nil {
			t.Fatal(err)
		}
		if err := bindings.apply(fs); err != nil {
			t.Fatal(err)
		}
		if cfg.Runtime.Profile != config.ProfileLiveOnly || cfg.Runtime.LoggingEnabled || cfg.Runtime.LogIntervalMS != 0 {
			t.Fatalf("unexpected live-only settings: %+v", cfg.Runtime)
		}
	})

	t.Run("enable logging", func(t *testing.T) {
		cfg := config.Default()
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		bindings := commonFlags(fs, &cfg)
		if err := fs.Parse([]string{"--profile=live-only", "--logging=true"}); err != nil {
			t.Fatal(err)
		}
		if err := bindings.apply(fs); err != nil {
			t.Fatal(err)
		}
		if cfg.Runtime.Profile != config.ProfileCustom || !cfg.Runtime.LoggingEnabled || cfg.Runtime.LogIntervalMS != 1000 {
			t.Fatalf("unexpected logging override: %+v", cfg.Runtime)
		}
	})
}

func TestRuntimeFlagsRejectInvalidValues(t *testing.T) {
	for _, args := range [][]string{
		{"--profile=unknown"},
		{"--ui-refresh=499ms"},
		{"--powermetrics-interval=1000us"},
		{"--logging=false", "--log-interval=1s"},
		{"--process-nice=11"},
	} {
		cfg := config.Default()
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		bindings := commonFlags(fs, &cfg)
		if err := fs.Parse(args); err != nil {
			t.Fatalf("parse %v: %v", args, err)
		}
		if err := bindings.apply(fs); err == nil {
			t.Fatalf("expected %v to fail validation", args)
		}
	}
}

func TestLoadRuntimeConfigUsesSelectedDataDirectory(t *testing.T) {
	dataDir := t.TempDir()
	want, err := config.SettingsForProfile(config.ProfileLowOverhead)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.SaveRuntimeSettings(dataDir, want); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadRuntimeConfig([]string{"--data-dir", dataDir})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != dataDir || !config.RuntimeSettingsEqual(cfg.Runtime, want) {
		t.Fatalf("loaded config=%+v want dataDir=%q settings=%+v", cfg, dataDir, want)
	}
}
