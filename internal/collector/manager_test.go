package collector

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/config"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/model"
)

func TestStartFailureDoesNotPoisonManagerLifecycle(t *testing.T) {
	base := t.TempDir()
	file := filepath.Join(base, "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DataDir = file
	cfg.AppAttribution = false
	cfg.SQLite = false
	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background(), nil); err == nil {
		t.Fatal("expected first start to fail")
	}

	manager.cfg.DataDir = filepath.Join(base, "valid")
	if err := manager.Start(context.Background(), nil); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := manager.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestUniqueSessionIDDoesNotCollideWithinSameTimestamp(t *testing.T) {
	now := time.Unix(1_700_000_000, 123456789)
	left := uniqueSessionID(now)
	right := uniqueSessionID(now)
	if left == right {
		t.Fatalf("session IDs collided: %q", left)
	}
}

func TestLastSampleReturnsDeepCopy(t *testing.T) {
	manager := &Manager{
		latest: &model.PowerSample{
			Components: model.ComponentSample{
				Clusters: []model.ClusterSample{{Name: "P0", FrequencyMHz: 3000}},
			},
			Attribution: model.AttributionResult{
				Apps: []model.AppPower{{Key: "app", Name: "App"}},
			},
			CollectorStatus: map[string]string{"battery": "ok"},
			Warnings:        []string{"warning"},
		},
	}
	copyValue := manager.LastSample()
	copyValue.Components.Clusters[0].Name = "changed"
	copyValue.Attribution.Apps[0].Name = "changed"
	copyValue.CollectorStatus["battery"] = "changed"
	copyValue.Warnings[0] = "changed"

	original := manager.LastSample()
	if original.Components.Clusters[0].Name != "P0" ||
		original.Attribution.Apps[0].Name != "App" ||
		original.CollectorStatus["battery"] != "ok" ||
		original.Warnings[0] != "warning" {
		t.Fatalf("internal sample was mutated: %+v", original)
	}
}

func TestManagerCannotRestartAfterCompletedRun(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.AppAttribution = false
	cfg.SQLite = false
	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := manager.Start(ctx, nil); err != nil {
		t.Fatal(err)
	}
	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := manager.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background(), nil); err == nil {
		t.Fatal("expected a completed manager to reject restart")
	}
}

func TestSensorFreshness(t *testing.T) {
	now := time.Unix(100, 0)
	if sensorFresh(now, time.Time{}, time.Second) {
		t.Fatal("zero timestamp must be stale")
	}
	if !sensorFresh(now, now.Add(-4*time.Second), time.Second) {
		t.Fatal("sample within minimum freshness should be fresh")
	}
	if sensorFresh(now, now.Add(-6*time.Second), time.Second) {
		t.Fatal("old sample should be stale")
	}
	if sensorFresh(now, now.Add(time.Second), time.Second) {
		t.Fatal("future sample should be stale")
	}
}

func TestNewManagerMarksDisabledAttribution(t *testing.T) {
	cfg := config.Default()
	cfg.AppAttribution = false
	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if manager.processStatus != "disabled" {
		t.Fatalf("process status=%q want=disabled", manager.processStatus)
	}
}

func TestWarningsDoNotReportDisabledAttribution(t *testing.T) {
	warnings := warningsFor(
		model.BatterySample{},
		model.AdapterSample{},
		model.ThermalSample{},
		PowermetricsSnapshot{Timestamp: time.Now()},
		nil,
		false,
	)
	for _, warning := range warnings {
		if warning == "process attribution unavailable" {
			t.Fatal("disabled app attribution was reported as unavailable")
		}
	}
}
