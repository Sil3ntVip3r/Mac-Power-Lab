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

	for _, required := range []string{"timeout: 120", "/report/latest", "ReportArtifact"} {
		if !strings.Contains(client, required) {
			t.Fatalf("APIClient missing timestamped report behavior %q", required)
		}
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
