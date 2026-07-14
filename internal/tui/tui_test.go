package tui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/model"
)

func TestRendererIncludesPowerAndApplications(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output, false)
	sample := &model.PowerSample{
		SessionID:         "session",
		Timestamp:         time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC),
		PrimaryLoadW:      42,
		PrimaryLoadSource: "battery discharge watts",
		Battery: model.BatterySample{
			PowerSource: "Battery Power",
			State:       "Discharging",
			Percent:     80,
			NetWatts:    -42,
		},
		Attribution: model.AttributionResult{
			Method:     "powermetrics-task-component-allocation",
			Confidence: "medium",
			Apps: []model.AppPower{{
				Name:              "Example",
				EstimatedShareW:   10,
				EstimatedDynamicW: 8,
				Confidence:        "medium",
			}},
		},
	}

	renderer.render(sample, model.BenchmarkProgress{})
	text := output.String()
	for _, expected := range []string{
		"MacPowerLab",
		"42.00 W",
		"battery discharge watts",
		"Example",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output missing %q:\n%s", expected, text)
		}
	}
}

func TestBarClampsRange(t *testing.T) {
	if got := bar(-1, 4); got != "░░░░" {
		t.Fatalf("negative bar=%q", got)
	}
	if got := bar(200, 4); got != "████" {
		t.Fatalf("overfull bar=%q", got)
	}
}
