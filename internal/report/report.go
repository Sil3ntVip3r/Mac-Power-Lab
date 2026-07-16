// Package report aggregates session JSONL into stable JSON, Markdown, and HTML.
package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/model"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/store"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/version"
)

const maxIntegrationGap = 30 * time.Second

const reportsDirectoryName = "reports"

// Artifact describes one immutable, cumulative report snapshot. Every report
// has its own timestamped directory and files; latest.json is only a pointer to
// the newest artifact and never replaces historical output.
type Artifact struct {
	Schema       string               `json:"schema"`
	GeneratedAt  time.Time            `json:"generated_at"`
	DataThrough  time.Time            `json:"data_through"`
	SessionID    string               `json:"session_id"`
	Directory    string               `json:"directory"`
	SummaryPath  string               `json:"summary_path"`
	MarkdownPath string               `json:"markdown_path"`
	HTMLPath     string               `json:"html_path"`
	Summary      model.SessionSummary `json:"summary"`
}

type reportDocument struct {
	model.SessionSummary
	GeneratedAt time.Time
	DataThrough time.Time
}

type sampleAccumulator struct {
	first             time.Time
	last              time.Time
	previous          *model.PowerSample
	sumLoad           float64
	sumDraw           float64
	loadWattSeconds   float64
	drawWattSeconds   float64
	integratedSeconds float64
	dischargedWh      float64
	chargedWh         float64
	skippedGaps       int
}

// Generate creates an immutable timestamped report from a closed or externally
// selected session directory. The report is cumulative through the captured
// byte boundaries returned by store.SnapshotDir.
func Generate(sessionDir string) (Artifact, error) {
	snapshot, err := store.SnapshotDir(sessionDir)
	if err != nil {
		return Artifact{}, err
	}
	return GenerateSnapshot(snapshot)
}

// GenerateSnapshot aggregates exactly the records present at snapshot capture.
// The underlying JSONL files may continue growing while this function runs.
func GenerateSnapshot(snapshot store.SessionSnapshot) (Artifact, error) {
	var artifact Artifact
	if strings.TrimSpace(snapshot.Dir) == "" {
		return artifact, errors.New("session snapshot directory is required")
	}
	if snapshot.CapturedAt.IsZero() {
		return artifact, errors.New("session snapshot capture time is required")
	}

	summary, err := aggregateSnapshot(snapshot)
	if err != nil {
		return artifact, err
	}
	return writeArtifact(snapshot, summary)
}

func aggregateSnapshot(snapshot store.SessionSnapshot) (model.SessionSummary, error) {
	var summary model.SessionSummary
	session, err := readSession(filepath.Join(snapshot.Dir, "session.json"))
	if err != nil {
		return summary, err
	}
	summary.Schema = version.SummarySchema
	summary.Session = session

	accumulator := sampleAccumulator{}
	warningSet := make(map[string]struct{})
	if err := readSnapshotJSONL[model.PowerSample](snapshot, "samples.jsonl", func(sample model.PowerSample) error {
		if sample.Schema != "" && sample.Schema != version.PowerSampleSchema {
			return fmt.Errorf("unsupported sample schema %q", sample.Schema)
		}
		if sample.SessionID != "" && sample.SessionID != session.ID {
			return fmt.Errorf("sample session %q does not match session %q", sample.SessionID, session.ID)
		}
		if !accumulator.last.IsZero() && sample.Timestamp.Before(accumulator.last) {
			return fmt.Errorf("sample timestamp moved backwards: %s before %s", sample.Timestamp, accumulator.last)
		}
		summary.SampleCount++
		accumulator.observe(sample)
		if sample.PrimaryLoadW > summary.PeakPrimaryLoadW {
			summary.PeakPrimaryLoadW = sample.PrimaryLoadW
		}
		draw := math.Max(0, -sample.Battery.NetWatts)
		if draw > summary.PeakBatteryDrawW {
			summary.PeakBatteryDrawW = draw
		}
		if sample.Battery.NetWatts > summary.PeakChargeW {
			summary.PeakChargeW = sample.Battery.NetWatts
		}
		if sample.Battery.TemperatureC > summary.MaxBatteryTempC {
			summary.MaxBatteryTempC = sample.Battery.TemperatureC
		}
		for _, warning := range sample.Warnings {
			warning = strings.TrimSpace(warning)
			if warning != "" {
				warningSet[warning] = struct{}{}
			}
		}
		return nil
	}); err != nil {
		return summary, fmt.Errorf("read power samples: %w", err)
	}

	summary.DurationSeconds = accumulator.durationSeconds()
	summary.AveragePrimaryLoadW = accumulator.averageLoad(summary.SampleCount)
	summary.AverageBatteryDrawW = accumulator.averageDraw(summary.SampleCount)
	summary.EnergyDischargedWh = accumulator.dischargedWh
	summary.EnergyChargedWh = accumulator.chargedWh
	if accumulator.skippedGaps > 0 {
		warningSet[fmt.Sprintf(
			"energy integration skipped %d sample gap(s) longer than %s",
			accumulator.skippedGaps,
			maxIntegrationGap,
		)] = struct{}{}
	}

	topApps, err := readTopApps(snapshot, 20)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return summary, fmt.Errorf("read application attribution: %w", err)
	}
	summary.TopApps = topApps

	testRuns, err := readFinalTestRuns(snapshot)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return summary, fmt.Errorf("read benchmark runs: %w", err)
	}
	summary.TestRuns = testRuns

	if err := readSnapshotJSONL[model.Event](snapshot, "events.jsonl", func(event model.Event) error {
		if event.SessionID != "" && event.SessionID != session.ID {
			return fmt.Errorf("event session %q does not match session %q", event.SessionID, session.ID)
		}
		if event.Type == "collector_error" || event.Type == "warning" {
			message := strings.TrimSpace(event.Message)
			if message != "" {
				warningSet[event.Type+": "+message] = struct{}{}
			}
		}
		return nil
	}); err != nil && !errors.Is(err, os.ErrNotExist) {
		return summary, fmt.Errorf("read session events: %w", err)
	}

	summary.Warnings = make([]string, 0, len(warningSet))
	for warning := range warningSet {
		summary.Warnings = append(summary.Warnings, warning)
	}
	sort.Strings(summary.Warnings)
	return summary, nil
}

func readSnapshotJSONL[T any](snapshot store.SessionSnapshot, name string, fn func(T) error) error {
	limit, ok := snapshot.Limit(name)
	if !ok {
		return os.ErrNotExist
	}
	return store.ReadJSONLPrefix(filepath.Join(snapshot.Dir, name), limit, fn)
}

func writeArtifact(snapshot store.SessionSnapshot, summary model.SessionSummary) (_ Artifact, retErr error) {
	reportsRoot := filepath.Join(snapshot.Dir, reportsDirectoryName)
	if err := os.MkdirAll(reportsRoot, 0o700); err != nil {
		return Artifact{}, fmt.Errorf("create reports directory: %w", err)
	}

	localCapture := snapshot.CapturedAt.Local()
	directoryStamp := localCapture.Format("20060102_150405.000000000-0700")
	fileStamp := localCapture.Format("20060102_150405-0700")
	finalDir, err := uniqueReportDirectory(reportsRoot, directoryStamp)
	if err != nil {
		return Artifact{}, err
	}
	tempDir, err := os.MkdirTemp(reportsRoot, ".building-*")
	if err != nil {
		return Artifact{}, fmt.Errorf("create report staging directory: %w", err)
	}
	defer func() {
		if retErr != nil {
			_ = os.RemoveAll(tempDir)
		}
	}()

	summaryName := "MacPowerLab_Summary_" + fileStamp + ".json"
	markdownName := "MacPowerLab_Report_" + fileStamp + ".md"
	htmlName := "MacPowerLab_Report_" + fileStamp + ".html"
	artifact := Artifact{
		Schema:       version.ReportArtifactSchema,
		GeneratedAt:  time.Now(),
		DataThrough:  snapshot.CapturedAt,
		SessionID:    summary.Session.ID,
		Directory:    finalDir,
		SummaryPath:  filepath.Join(finalDir, summaryName),
		MarkdownPath: filepath.Join(finalDir, markdownName),
		HTMLPath:     filepath.Join(finalDir, htmlName),
		Summary:      summary,
	}
	document := reportDocument{
		SessionSummary: summary,
		GeneratedAt:    artifact.GeneratedAt,
		DataThrough:    artifact.DataThrough,
	}

	if err := atomicJSON(filepath.Join(tempDir, summaryName), summary); err != nil {
		return Artifact{}, err
	}
	if err := writeMarkdown(filepath.Join(tempDir, markdownName), document); err != nil {
		return Artifact{}, err
	}
	if err := writeHTML(filepath.Join(tempDir, htmlName), document); err != nil {
		return Artifact{}, err
	}
	if err := atomicJSON(filepath.Join(tempDir, "artifact.json"), artifact); err != nil {
		return Artifact{}, err
	}
	if err := os.Rename(tempDir, finalDir); err != nil {
		return Artifact{}, fmt.Errorf("publish report snapshot: %w", err)
	}
	if err := atomicJSON(filepath.Join(reportsRoot, "latest.json"), artifact); err != nil {
		return artifact, fmt.Errorf("write latest report pointer: %w", err)
	}
	return artifact, nil
}

func uniqueReportDirectory(root, stamp string) (string, error) {
	for index := 0; index < 1000; index++ {
		name := stamp
		if index > 0 {
			name = fmt.Sprintf("%s-%03d", stamp, index)
		}
		path := filepath.Join(root, name)
		_, err := os.Stat(path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			return path, nil
		case err != nil:
			return "", err
		}
	}
	return "", errors.New("could not allocate unique report directory")
}

// Latest returns the newest published report artifact for a session.
func Latest(sessionDir string) (Artifact, error) {
	var artifact Artifact
	raw, err := os.ReadFile(filepath.Join(sessionDir, reportsDirectoryName, "latest.json"))
	if err != nil {
		return artifact, err
	}
	if err := json.Unmarshal(raw, &artifact); err != nil {
		return artifact, fmt.Errorf("decode latest report artifact: %w", err)
	}
	if artifact.HTMLPath == "" {
		return artifact, errors.New("latest report artifact has no HTML path")
	}
	if _, err := os.Stat(artifact.HTMLPath); err != nil {
		return artifact, fmt.Errorf("latest report HTML: %w", err)
	}
	return artifact, nil
}

func readSession(path string) (model.Session, error) {
	var session model.Session
	raw, err := os.ReadFile(path)
	if err != nil {
		return session, err
	}
	if err := json.Unmarshal(raw, &session); err != nil {
		return session, fmt.Errorf("decode %s: %w", path, err)
	}
	return session, nil
}

func (a *sampleAccumulator) observe(sample model.PowerSample) {
	if a.first.IsZero() {
		a.first = sample.Timestamp
	}
	a.last = sample.Timestamp
	a.sumLoad += sample.PrimaryLoadW
	a.sumDraw += math.Max(0, -sample.Battery.NetWatts)

	if a.previous != nil {
		delta := sample.Timestamp.Sub(a.previous.Timestamp)
		if delta > 0 && delta <= maxIntegrationGap {
			seconds := delta.Seconds()
			a.integratedSeconds += seconds
			a.loadWattSeconds += a.previous.PrimaryLoadW * seconds
			draw := math.Max(0, -a.previous.Battery.NetWatts)
			charge := math.Max(0, a.previous.Battery.NetWatts)
			a.drawWattSeconds += draw * seconds
			a.dischargedWh += draw * delta.Hours()
			a.chargedWh += charge * delta.Hours()
		} else if delta > maxIntegrationGap {
			a.skippedGaps++
		}
	}
	copyValue := sample
	a.previous = &copyValue
}

func (a sampleAccumulator) durationSeconds() float64 {
	if a.first.IsZero() || a.last.Before(a.first) {
		return 0
	}
	return a.last.Sub(a.first).Seconds()
}

func (a sampleAccumulator) averageLoad(count int64) float64 {
	if a.integratedSeconds > 0 {
		return a.loadWattSeconds / a.integratedSeconds
	}
	if count > 0 {
		return a.sumLoad / float64(count)
	}
	return 0
}

func (a sampleAccumulator) averageDraw(count int64) float64 {
	if a.integratedSeconds > 0 {
		return a.drawWattSeconds / a.integratedSeconds
	}
	if count > 0 {
		return a.sumDraw / float64(count)
	}
	return 0
}

func readTopApps(snapshot store.SessionSnapshot, limit int) ([]model.AppPower, error) {
	latest := make(map[string]model.AppPower)
	if err := readSnapshotJSONL[model.AppPower](snapshot, "apps.jsonl", func(app model.AppPower) error {
		old, exists := latest[app.Key]
		if !exists || app.EstimatedEnergyWh > old.EstimatedEnergyWh || app.Timestamp.After(old.Timestamp) {
			latest[app.Key] = app
		}
		return nil
	}); err != nil {
		return nil, err
	}
	apps := make([]model.AppPower, 0, len(latest))
	for _, app := range latest {
		apps = append(apps, app)
	}
	sort.SliceStable(apps, func(i, j int) bool {
		if apps[i].EstimatedEnergyWh == apps[j].EstimatedEnergyWh {
			return apps[i].Name < apps[j].Name
		}
		return apps[i].EstimatedEnergyWh > apps[j].EstimatedEnergyWh
	})
	if limit > 0 && len(apps) > limit {
		apps = apps[:limit]
	}
	return apps, nil
}

func readFinalTestRuns(snapshot store.SessionSnapshot) ([]model.TestRun, error) {
	byID := make(map[string]model.TestRun)
	if err := readSnapshotJSONL[model.TestRun](snapshot, "test_runs.jsonl", func(run model.TestRun) error {
		old, exists := byID[run.ID]
		if !exists || testRunNewer(run, old) {
			byID[run.ID] = run
		}
		return nil
	}); err != nil {
		return nil, err
	}
	runs := make([]model.TestRun, 0, len(byID))
	for _, run := range byID {
		runs = append(runs, run)
	}
	sort.SliceStable(runs, func(i, j int) bool {
		if runs[i].StartedAt.Equal(runs[j].StartedAt) {
			return runs[i].ID < runs[j].ID
		}
		return runs[i].StartedAt.Before(runs[j].StartedAt)
	})
	return runs, nil
}

func testRunNewer(candidate, current model.TestRun) bool {
	if current.Status == "running" && candidate.Status != "running" {
		return true
	}
	if candidate.EndedAt.After(current.EndedAt) {
		return true
	}
	return candidate.ActualSeconds > current.ActualSeconds
}

func writeMarkdown(path string, document reportDocument) error {
	summary := document.SessionSummary
	var builder strings.Builder
	fmt.Fprintf(
		&builder,
		"# MacPowerLab Session Report\n\n"+
			"- Generated: `%s`\n- Data through: `%s`\n"+
			"- Session: `%s`\n- Version: `%s`\n- OS: `%s` build `%s`\n"+
			"- Duration: %.1f minutes\n- Samples: %d\n\n## Power\n\n"+
			"- Peak primary load: %.2f W\n- Time-weighted average primary load: %.2f W\n"+
			"- Peak battery draw: %.2f W\n- Time-weighted average battery draw: %.2f W\n"+
			"- Peak charge: %.2f W\n- Energy discharged: %.2f Wh\n"+
			"- Energy charged: %.2f Wh\n- Max battery temperature: %.2f °C\n\n"+
			"## Top applications\n\n| App | Dynamic W | Energy Wh | Impact | Confidence |\n"+
			"|---|---:|---:|---:|---|\n",
		document.GeneratedAt.Format(time.RFC3339),
		document.DataThrough.Format(time.RFC3339),
		summary.Session.ID,
		summary.Session.Version,
		summary.Session.OSVersion,
		summary.Session.OSBuild,
		summary.DurationSeconds/60,
		summary.SampleCount,
		summary.PeakPrimaryLoadW,
		summary.AveragePrimaryLoadW,
		summary.PeakBatteryDrawW,
		summary.AverageBatteryDrawW,
		summary.PeakChargeW,
		summary.EnergyDischargedWh,
		summary.EnergyChargedWh,
		summary.MaxBatteryTempC,
	)
	for _, app := range summary.TopApps {
		fmt.Fprintf(
			&builder,
			"| %s | %.2f | %.4f | %.2f | %s |\n",
			markdownCell(app.Name),
			app.EstimatedDynamicW,
			app.EstimatedEnergyWh,
			app.EnergyImpact,
			markdownCell(app.Confidence),
		)
	}
	builder.WriteString(
		"\n## Benchmark runs\n\n" +
			"| Name | Status | Requested | Actual | Backend nice | Workload nice |\n" +
			"|---|---|---:|---:|---:|---|\n",
	)
	for _, run := range summary.TestRuns {
		fmt.Fprintf(
			&builder,
			"| %s | %s | %.0fs | %.1fs | %s | %s |\n",
			markdownCell(run.Name),
			markdownCell(run.Status),
			run.RequestedSeconds,
			run.ActualSeconds,
			markdownCell(benchmarkBackendNice(run)),
			markdownCell(benchmarkWorkloadNice(run)),
		)
	}
	if len(summary.Warnings) > 0 {
		builder.WriteString("\n## Warnings\n\n")
		for _, warning := range summary.Warnings {
			fmt.Fprintf(&builder, "- %s\n", strings.ReplaceAll(warning, "\n", " "))
		}
	}
	return atomicWrite(path, []byte(builder.String()), 0o600)
}

func benchmarkBackendNice(run model.TestRun) string {
	if run.Priority == nil {
		return "n/a"
	}
	if !run.Priority.Supported {
		return "unsupported"
	}
	return signedNice(run.Priority.ObservedBackendNice)
}

func benchmarkWorkloadNice(run model.TestRun) string {
	if run.Priority == nil {
		return "n/a"
	}
	if !run.Priority.Supported {
		return "unsupported"
	}
	if len(run.Priority.Workloads) == 0 {
		if len(run.Priority.Errors) > 0 {
			return "capture failed"
		}
		return "none"
	}
	parts := make([]string, 0, len(run.Priority.Workloads))
	for _, workload := range run.Priority.Workloads {
		parts = append(parts, workload.Label+"="+signedNice(workload.Nice))
	}
	return strings.Join(parts, ", ")
}

func signedNice(value int) string {
	if value > 0 {
		return "+" + strconv.Itoa(value)
	}
	return strconv.Itoa(value)
}

func markdownCell(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	return strings.ReplaceAll(value, "\n", " ")
}

var htmlTemplate = template.Must(template.New("report").Funcs(template.FuncMap{
	"f":            func(value float64) string { return fmt.Sprintf("%.2f", value) },
	"mins":         func(value float64) string { return fmt.Sprintf("%.1f", value/60) },
	"date":         func(value time.Time) string { return value.Format(time.RFC3339) },
	"backendNice":  benchmarkBackendNice,
	"workloadNice": benchmarkWorkloadNice,
}).Parse(`<!doctype html><html><head><meta charset="utf-8"><title>MacPowerLab Report</title><style>body{font-family:-apple-system,BlinkMacSystemFont,sans-serif;background:#0b1016;color:#e6edf3;margin:0}main,header{max-width:1200px;margin:auto;padding:24px}header{background:#111b26}h2{color:#65e5ff;border-bottom:1px solid #263443;padding-bottom:8px}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(190px,1fr));gap:12px}.card{background:#111820;border:1px solid #253545;border-radius:12px;padding:15px}.value{font-size:24px;font-weight:700}table{width:100%;border-collapse:collapse;background:#111820}th,td{padding:9px;border-bottom:1px solid #263443;text-align:left}</style></head><body><header><h1>MacPowerLab v{{.Session.Version}}</h1><p>Session {{.Session.ID}} · {{.Session.OSVersion}} {{.Session.OSBuild}}</p><p>Generated {{date .GeneratedAt}} · Data through {{date .DataThrough}}</p></header><main><h2>Summary</h2><div class="grid"><div class="card">Peak load<div class="value">{{f .PeakPrimaryLoadW}} W</div></div><div class="card">Average load<div class="value">{{f .AveragePrimaryLoadW}} W</div></div><div class="card">Peak battery draw<div class="value">{{f .PeakBatteryDrawW}} W</div></div><div class="card">Discharged<div class="value">{{f .EnergyDischargedWh}} Wh</div></div><div class="card">Max battery temp<div class="value">{{f .MaxBatteryTempC}} °C</div></div><div class="card">Duration<div class="value">{{mins .DurationSeconds}} min</div></div></div><h2>Top applications</h2><table><tr><th>App</th><th>Dynamic W</th><th>Energy Wh</th><th>Impact</th><th>Confidence</th></tr>{{range .TopApps}}<tr><td>{{.Name}}</td><td>{{f .EstimatedDynamicW}}</td><td>{{f .EstimatedEnergyWh}}</td><td>{{f .EnergyImpact}}</td><td>{{.Confidence}}</td></tr>{{end}}</table><h2>Benchmark runs</h2><table><tr><th>Name</th><th>Status</th><th>Requested</th><th>Actual</th><th>Backend nice</th><th>Workload nice</th></tr>{{range .TestRuns}}<tr><td>{{.Name}}</td><td>{{.Status}}</td><td>{{f .RequestedSeconds}}s</td><td>{{f .ActualSeconds}}s</td><td>{{backendNice .}}</td><td>{{workloadNice .}}</td></tr>{{end}}</table></main></body></html>`))

func writeHTML(path string, document reportDocument) error {
	var buffer bytes.Buffer
	if err := htmlTemplate.Execute(&buffer, document); err != nil {
		return err
	}
	return atomicWrite(path, buffer.Bytes(), 0o600)
}

func atomicJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, data, 0o600)
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// CompareLatest writes a Markdown comparison of the two most recent summaries.
func CompareLatest(base string) (string, error) {
	entries, err := os.ReadDir(filepath.Join(base, "sessions"))
	if err != nil {
		return "", err
	}
	type item struct {
		path string
		time time.Time
	}
	items := make([]item, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sessionDir := filepath.Join(base, "sessions", entry.Name())
		path, statErr := latestSummaryPath(sessionDir)
		if statErr != nil {
			continue
		}
		info, statErr := os.Stat(path)
		if statErr == nil {
			items = append(items, item{path: path, time: info.ModTime()})
		}
	}
	if len(items) < 2 {
		return "", errors.New("at least two session summaries are required")
	}
	sort.Slice(items, func(i, j int) bool { return items[i].time.After(items[j].time) })
	previous, err := readSummary(items[1].path)
	if err != nil {
		return "", err
	}
	latest, err := readSummary(items[0].path)
	if err != nil {
		return "", err
	}
	output := filepath.Join(base, "comparison_"+time.Now().Format("20060102_150405")+".md")
	content := fmt.Sprintf(
		"# MacPowerLab Comparison\n\n| Metric | Previous | Latest | Delta |\n|---|---:|---:|---:|\n"+
			"| Peak load W | %.2f | %.2f | %+.2f |\n"+
			"| Average load W | %.2f | %.2f | %+.2f |\n"+
			"| Peak battery draw W | %.2f | %.2f | %+.2f |\n"+
			"| Energy discharged Wh | %.2f | %.2f | %+.2f |\n"+
			"| Max temperature C | %.2f | %.2f | %+.2f |\n",
		previous.PeakPrimaryLoadW, latest.PeakPrimaryLoadW, latest.PeakPrimaryLoadW-previous.PeakPrimaryLoadW,
		previous.AveragePrimaryLoadW, latest.AveragePrimaryLoadW, latest.AveragePrimaryLoadW-previous.AveragePrimaryLoadW,
		previous.PeakBatteryDrawW, latest.PeakBatteryDrawW, latest.PeakBatteryDrawW-previous.PeakBatteryDrawW,
		previous.EnergyDischargedWh, latest.EnergyDischargedWh, latest.EnergyDischargedWh-previous.EnergyDischargedWh,
		previous.MaxBatteryTempC, latest.MaxBatteryTempC, latest.MaxBatteryTempC-previous.MaxBatteryTempC,
	)
	return output, atomicWrite(output, []byte(content), 0o600)
}

func latestSummaryPath(sessionDir string) (string, error) {
	if artifact, err := Latest(sessionDir); err == nil && artifact.SummaryPath != "" {
		return artifact.SummaryPath, nil
	}

	// Backward compatibility for sessions reported before timestamped artifacts
	// were introduced.
	legacy := filepath.Join(sessionDir, "summary.json")
	if _, err := os.Stat(legacy); err == nil {
		return legacy, nil
	}
	return "", os.ErrNotExist
}

func readSummary(path string) (model.SessionSummary, error) {
	var summary model.SessionSummary
	raw, err := os.ReadFile(path)
	if err != nil {
		return summary, err
	}
	if err := json.Unmarshal(raw, &summary); err != nil {
		return summary, err
	}
	return summary, nil
}
