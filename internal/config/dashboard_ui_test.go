package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readSwiftUIFile(t *testing.T, name string) string {
	t.Helper()
	root := filepath.Clean(filepath.Join("..", ".."))
	path := filepath.Join(
		root,
		"swiftui",
		"Sources",
		"MacPowerLabApp",
		name,
	)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestMonitorNavigationContainsSubDashboards(t *testing.T) {
	text := readSwiftUIFile(t, "ContentView.swift")
	for _, required := range []string{
		"Battery & Charging",
		"Performance",
		"Applications",
		"Full Monitor",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("ContentView missing %q", required)
		}
	}
}

func TestApplicationTableIsDedicatedAndNotNestedInDashboard(t *testing.T) {
	dashboard := readSwiftUIFile(t, "DashboardView.swift")
	apps := readSwiftUIFile(t, "ApplicationsView.swift")
	if strings.Contains(dashboard, "Table(") {
		t.Fatal("overview dashboard must not contain the application Table")
	}
	if !strings.Contains(apps, "Table(") {
		t.Fatal("ApplicationsView must contain a sortable Table")
	}
	if strings.Contains(apps, "ScrollView {") {
		t.Fatal("ApplicationsView must not wrap the Table in a vertical ScrollView")
	}
}

func TestFullMonitorOmitsApplicationRows(t *testing.T) {
	text := readSwiftUIFile(t, "FullMonitorView.swift")
	if strings.Contains(text, "ForEach(sample.attribution?.apps") ||
		strings.Contains(text, "Table(") {
		t.Fatal("Full Monitor must not render the application table")
	}
	if !strings.Contains(text, "Application Attribution Summary") {
		t.Fatal("Full Monitor should retain attribution summary totals")
	}
}

func TestExpandedModelsExposeBatteryAndAdapterFields(t *testing.T) {
	text := readSwiftUIFile(t, "Models.swift")
	for _, required := range []string{
		"estimatedRemainingWh",
		"cellVoltageMinMV",
		"contractVoltageV",
		"batteryAssistW",
		"cpuComponentPoolW",
		"collectorStatus",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("Models.swift missing %q", required)
		}
	}
}

func TestReportGenerationUXIsVisibleAndActionable(t *testing.T) {
	model := readSwiftUIFile(t, "AppModel.swift")
	content := readSwiftUIFile(t, "ContentView.swift")
	client := readSwiftUIFile(t, "APIClient.swift")

	for _, required := range []string{
		"isGeneratingReport",
		"latestReportURL",
		"artifact.htmlPath",
		"artifact.dataThrough",
		"NSWorkspace.shared.open",
		"activateFileViewerSelecting",
		"guard !isGeneratingReport else { return }",
	} {
		if !strings.Contains(model, required) {
			t.Fatalf("AppModel missing report UX behavior %q", required)
		}
	}

	for _, required := range []string{
		"Generating…",
		"Open Latest Report",
		"Show Report in Finder",
		"model.reportMessage",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("ContentView missing visible report control %q", required)
		}
	}

	for _, required := range []string{"timeout: 120", "timeout: 90", "/report/latest", "ReportArtifact"} {
		if !strings.Contains(client, required) {
			t.Fatalf("APIClient missing timestamped report behavior %q", required)
		}
	}
}

func TestLiveOnlyModeDisablesHistoricalReports(t *testing.T) {
	model := readSwiftUIFile(t, "AppModel.swift")
	content := readSwiftUIFile(t, "ContentView.swift")
	settings := readSwiftUIFile(t, "SettingsView.swift")
	for _, required := range []string{
		"runtimeSettings?.loggingEnabled != false",
		"Durable logging is off",
	} {
		if !strings.Contains(model, required) {
			t.Fatalf("AppModel missing Live-only report guard %q", required)
		}
	}
	for _, required := range []string{
		"model.runtimeSettings?.loggingEnabled == false",
		"Enable durable logging to generate historical reports",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("ContentView missing Live-only report behavior %q", required)
		}
	}
	if !strings.Contains(settings, "Historical reports are unavailable") {
		t.Fatal("SettingsView must explain Live-only report behavior")
	}
}

func TestApplicationPowerDistinguishesZeroFromUnavailable(t *testing.T) {
	apps := readSwiftUIFile(t, "ApplicationsView.swift")
	for _, required := range []string{
		"attributedWatts",
		"value ?? 0",
		"0.00 W is a valid zero allocation",
		"n/a means attribution is unavailable",
	} {
		if !strings.Contains(apps, required) {
			t.Fatalf("ApplicationsView missing zero-attribution behavior %q", required)
		}
	}
}

func TestRuntimeSettingsControlsCoverAPIAndSafetySemantics(t *testing.T) {
	models := readSwiftUIFile(t, "Models.swift")
	client := readSwiftUIFile(t, "APIClient.swift")
	appModel := readSwiftUIFile(t, "AppModel.swift")
	settings := readSwiftUIFile(t, "SettingsView.swift")

	for _, required := range []string{
		"macpowerlab.runtime_settings.v1",
		"ui_refresh_ms",
		"battery_collection_ms",
		"powermetrics_ms",
		"app_attribution_ms",
		"logging_enabled",
		"log_interval_ms",
		"process_nice",
	} {
		if !strings.Contains(models, required) {
			t.Fatalf("Models.swift missing runtime settings field %q", required)
		}
	}

	for _, required := range []string{
		"/settings",
		"/settings/profiles",
		"method: \"PUT\"",
		"updateRuntimeSettings",
	} {
		if !strings.Contains(client, required) {
			t.Fatalf("APIClient missing runtime settings behavior %q", required)
		}
	}

	for _, required := range []string{
		"api.runtimeSettings()",
		"api.runtimeProfiles()",
		"api.updateRuntimeSettings(settings)",
		"Task.sleep(for: .milliseconds(refreshMS))",
	} {
		if !strings.Contains(appModel, required) {
			t.Fatalf("AppModel missing runtime settings behavior %q", required)
		}
	}

	for _, required := range []string{
		"Runtime profile",
		"UI refresh",
		"Battery collection",
		"powermetrics",
		"App attribution",
		"Log power and app samples",
		"Ordinary nice value",
		"never kernel real-time scheduling",
		"starts a fresh session",
	} {
		if !strings.Contains(settings, required) {
			t.Fatalf("SettingsView missing runtime control or safety copy %q", required)
		}
	}
}

func TestCadenceDiagnosticsAndBenchmarkPriorityAreVisible(t *testing.T) {
	models := readSwiftUIFile(t, "Models.swift")
	settings := readSwiftUIFile(t, "SettingsView.swift")
	full := readSwiftUIFile(t, "FullMonitorView.swift")
	appModel := readSwiftUIFile(t, "AppModel.swift")

	for _, required := range []string{
		"CadenceDiagnostics",
		"CadenceMetric",
		"BenchmarkPriorityObservation",
		"ProcessPriorityObservation",
	} {
		if !strings.Contains(models, required) {
			t.Fatalf("Models.swift missing %q", required)
		}
	}
	for _, required := range []string{
		"Cadence diagnostics",
		"SwiftUI status polling",
		"Replaced stream frames",
	} {
		if !strings.Contains(settings, required) {
			t.Fatalf("SettingsView.swift missing %q", required)
		}
	}
	for _, required := range []string{
		"Cadence Diagnostics",
		"Backend nice",
		"Workload nice",
	} {
		if !strings.Contains(full, required) {
			t.Fatalf("FullMonitorView.swift missing %q", required)
		}
	}
	for _, required := range []string{"observedUIRefreshMS", "missedLivePublications"} {
		if !strings.Contains(appModel, required) {
			t.Fatalf("AppModel.swift missing client cadence diagnostic %q", required)
		}
	}
}
