// Command macpowerlab is the production Go entry point for monitoring,
// attribution, benchmark control, reports, parity, packaging, and the local API.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/archive"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/benchmark"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/collector"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/config"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/execx"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/legacy"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/parity"
	plistx "github.com/Sil3ntVip3r/Mac-Power-Lab/internal/plist"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/report"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/server"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/store"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/tui"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	switch args[0] {
	case "monitor":
		return runMonitor(args[1:])
	case "apps":
		return runApps(args[1:])
	case "benchmark":
		return runBenchmark(args[1:])
	case "build-native":
		return runBuildNative(args[1:])
	case "report":
		return runReport(args[1:])
	case "compare":
		return runCompare(args[1:])
	case "sensors":
		return runSensors(args[1:])
	case "logs":
		return runLogs(args[1:])
	case "parity":
		return runParity(args[1:])
	case "parse":
		return runParse(args[1:])
	case "serve":
		return runServe(args[1:])
	case "legacy":
		return runLegacy(args[1:])
	case "version", "--version", "-v":
		fmt.Printf("MacPowerLab v%s (%s/%s)\n", version.Version, runtime.GOOS, runtime.GOARCH)
		return nil
	case "help", "--help", "-h":
		usage()
		return nil
	default:
		return fmt.Errorf("unknown command %q; run `macpowerlab help`", args[0])
	}
}

func commonFlags(fs *flag.FlagSet, cfg *config.Config) {
	fs.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "private data directory")
	fs.DurationVar(&cfg.SampleInterval, "interval", cfg.SampleInterval, "battery/UI sample interval")
	fs.DurationVar(&cfg.PowermetricsInterval, "powermetrics-interval", cfg.PowermetricsInterval, "system powermetrics sample interval")
	fs.DurationVar(&cfg.ProcessInterval, "process-interval", cfg.ProcessInterval, "process/app attribution sample interval")
	fs.IntVar(&cfg.TopApps, "top-apps", cfg.TopApps, "number of attributed apps retained")
	fs.BoolVar(&cfg.AppAttribution, "apps", cfg.AppAttribution, "enable app power attribution")
	fs.BoolVar(&cfg.SQLite, "sqlite", cfg.SQLite, "write optional SQLite mirror when sqlite3 is available")
	fs.StringVar(&cfg.NativeDir, "native-dir", cfg.NativeDir, "native workload source directory")
	fs.StringVar(&cfg.NativeBinDir, "native-bin-dir", cfg.NativeBinDir, "native workload binary directory")
	fs.BoolVar(&cfg.NoColor, "no-color", cfg.NoColor, "disable ANSI colors")
}

func runMonitor(args []string) error {
	cfg := config.Default()
	fs := flag.NewFlagSet("monitor", flag.ContinueOnError)
	commonFlags(fs, &cfg)
	duration := fs.Duration("duration", 0, "optional monitor duration; zero runs until Ctrl+C")
	safeMode := fs.Bool("safe", false, "disable app attribution and SQLite for minimum-overhead diagnostics")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *safeMode {
		cfg.AppAttribution = false
		cfg.SQLite = false
		if cfg.PowermetricsInterval < 3*time.Second {
			cfg.PowermetricsInterval = 3 * time.Second
		}
	}
	if err := ensureSudo(true); err != nil {
		return err
	}
	ctx, cancel := signalContext()
	defer cancel()
	if *duration > 0 {
		var timedCancel context.CancelFunc
		ctx, timedCancel = context.WithTimeout(ctx, *duration)
		defer timedCancel()
	}
	m, err := collector.NewManager(cfg)
	if err != nil {
		return err
	}
	if err := m.Start(ctx, map[string]string{"command": "monitor"}); err != nil {
		return err
	}
	err = tui.Run(ctx, m, nil, !cfg.NoColor)
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	stopErr := m.Stop(stopCtx)
	if stopErr != nil {
		return errors.Join(err, stopErr)
	}
	if dir := m.SessionDir(); dir != "" {
		if _, reportErr := report.Generate(dir); reportErr != nil {
			fmt.Fprintln(os.Stderr, "report warning:", reportErr)
		} else {
			fmt.Println("\nSession:", dir)
		}
	}
	return err
}

func runApps(args []string) error {
	cfg := config.Default()
	fs := flag.NewFlagSet("apps", flag.ContinueOnError)
	commonFlags(fs, &cfg)
	duration := fs.Duration("duration", 8*time.Second, "sampling duration")
	jsonOutput := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *duration < 2*time.Second || *duration > 10*time.Minute {
		return errors.New("duration must be between 2s and 10m")
	}
	if err := ensureSudo(true); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()
	m, err := collector.NewManager(cfg)
	if err != nil {
		return err
	}
	if err := m.Start(ctx, map[string]string{"command": "apps"}); err != nil {
		return err
	}
	<-ctx.Done()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	if err := m.Stop(stopCtx); err != nil {
		return err
	}
	sample := m.LastSample()
	if sample == nil {
		return errors.New("no sample collected")
	}
	if *jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(sample.Attribution)
	}
	fmt.Printf("Model: %s, confidence: %s, total %.2f W, baseline %.2f W\n", sample.Attribution.Method, sample.Attribution.Confidence, sample.PrimaryLoadW, sample.Attribution.BaselineWatts)
	fmt.Printf("%-3s %-28s %10s %10s %10s %s\n", "#", "Application", "Total W", "Dynamic W", "Energy Wh", "Confidence")
	for i, app := range sample.Attribution.Apps {
		fmt.Printf("%-3d %-28.28s %10.2f %10.2f %10.4f %s\n", i+1, app.Name, app.EstimatedShareW, app.EstimatedDynamicW, app.EstimatedEnergyWh, app.Confidence)
	}
	return nil
}

func runBenchmark(args []string) error {
	if len(args) == 0 {
		return errors.New("benchmark requires a preset name, custom, or list")
	}
	kind := args[0]
	if kind == "list" {
		for _, definition := range benchmark.Catalog() {
			fmt.Printf(
				"%-12s %-28s %6.1f min  %-13s %s\n",
				definition.ID,
				definition.Name,
				definition.TypicalDurationSeconds/60,
				powerRequirementLabel(definition.RequiredPowerSource),
				definition.Summary,
			)
		}
		return nil
	}

	cfg := config.Default()
	fs := flag.NewFlagSet("benchmark "+kind, flag.ContinueOnError)
	commonFlags(fs, &cfg)
	duration := fs.Duration("duration", 0, "preset duration override or custom workload duration")
	memoryMB := fs.Int("memory-mb", 0, "memory stress allocation in MB; zero uses native default")
	profile := fs.String("gpu-profile", "high", "GPU profile: normal, high, extreme")
	pack := fs.Bool("pack", true, "create compressed session archive after benchmark")

	customCPU := fs.Bool("cpu", false, "custom mode: include CPU stress")
	customGPU := fs.Bool("gpu", false, "custom mode: include GPU stress")
	customMemory := fs.Bool("memory", false, "custom mode: include memory stress")
	customBaseline := fs.Duration("baseline", 0, "custom mode: optional idle baseline duration")
	customCooldown := fs.Duration("cooldown", 0, "custom mode: optional cooldown duration")
	customPower := fs.String("power-source", "any", "custom mode: any, battery, or ac")
	customName := fs.String("name", "Custom workload", "custom mode display name")

	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *duration < 0 || *duration > 24*time.Hour {
		return errors.New("duration must be between 0 and 24h")
	}
	if *memoryMB < 0 || *memoryMB > 262144 {
		return errors.New("memory-mb must be between 0 and 262144")
	}
	if *profile != "normal" && *profile != "high" && *profile != "extreme" {
		return errors.New("gpu-profile must be normal, high, or extreme")
	}
	if *customBaseline < 0 || *customBaseline > time.Hour {
		return errors.New("baseline must be between 0 and 1h")
	}
	if *customCooldown < 0 || *customCooldown > time.Hour {
		return errors.New("cooldown must be between 0 and 1h")
	}

	var (
		plan benchmark.Plan
		err  error
	)
	if kind == "custom" {
		workloadDuration := *duration
		if workloadDuration == 0 {
			workloadDuration = 5 * time.Minute
		}
		plan, err = benchmark.CustomPlan(benchmark.CustomSpec{
			DisplayName:         *customName,
			RequiredPowerSource: *customPower,
			CPU:                 *customCPU,
			GPU:                 *customGPU,
			Memory:              *customMemory,
			GPUProfile:          *profile,
			MemoryMB:            *memoryMB,
			WorkloadDuration:    workloadDuration,
			BaselineDuration:    *customBaseline,
			CooldownDuration:    *customCooldown,
		})
	} else {
		plan, err = benchmark.PlanByID(kind, *duration)
	}
	if err != nil {
		return err
	}

	if err := ensureSudo(true); err != nil {
		return err
	}
	ctx, cancel := signalContext()
	defer cancel()
	m, err := collector.NewManager(cfg)
	if err != nil {
		return err
	}
	if err := m.Start(ctx, map[string]string{"command": "benchmark", "plan": kind}); err != nil {
		return err
	}
	controller := benchmark.New(m)
	root := projectRoot()
	options := benchmark.Options{
		NativeDir:  filepath.Join(root, "native"),
		BinDir:     filepath.Join(root, "bin", "native"),
		MemoryMB:   *memoryMB,
		GPUProfile: *profile,
	}

	tuiDone := make(chan error, 1)
	go func() { tuiDone <- tui.Run(ctx, m, controller, !cfg.NoColor) }()
	runErr := controller.Run(ctx, plan, options)
	cancel()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 15*time.Second)
	stopErr := m.Stop(stopCtx)
	stopCancel()
	var tuiErr error
	select {
	case tuiErr = <-tuiDone:
	case <-time.After(2 * time.Second):
		tuiErr = errors.New("terminal UI did not stop within 2 seconds")
	}
	if stopErr != nil || tuiErr != nil {
		return errors.Join(runErr, stopErr, tuiErr)
	}

	dir := m.SessionDir()
	if dir != "" {
		if _, err := report.Generate(dir); err != nil {
			fmt.Fprintln(os.Stderr, "report warning:", err)
		}
		if *pack {
			out := filepath.Join(cfg.DataDir, "exports", archive.DefaultName("macpowerlab_"+kind))
			if err := archive.Create(dir, out); err != nil {
				fmt.Fprintln(os.Stderr, "archive warning:", err)
			} else {
				fmt.Println("Archive:", out)
			}
		}
	}
	return runErr
}

func powerRequirementLabel(source string) string {
	if source == "" {
		return "Any power"
	}
	return source
}

func runBuildNative(args []string) error {
	fs := flag.NewFlagSet("build-native", flag.ContinueOnError)
	root := projectRoot()
	native := fs.String("native-dir", filepath.Join(root, "native"), "native source directory")
	bin := fs.String("bin-dir", filepath.Join(root, "bin", "native"), "native binary directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return benchmark.BuildNative(context.Background(), *native, *bin)
}

func runReport(args []string) error {
	cfg := config.Default()
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	commonFlags(fs, &cfg)
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir := ""
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	} else {
		var err error
		dir, err = store.LatestSessionDir(cfg.DataDir)
		if err != nil {
			return err
		}
	}
	summary, err := report.Generate(dir)
	if err != nil {
		return err
	}
	fmt.Printf("Report: %s\nPeak load: %.2f W; discharged: %.2f Wh; top apps: %d\n", filepath.Join(dir, "report.html"), summary.PeakPrimaryLoadW, summary.EnergyDischargedWh, len(summary.TopApps))
	return nil
}

func runCompare(args []string) error {
	cfg := config.Default()
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	commonFlags(fs, &cfg)
	if err := fs.Parse(args); err != nil {
		return err
	}
	out, err := report.CompareLatest(cfg.DataDir)
	if err != nil {
		return err
	}
	fmt.Println("Comparison:", out)
	return nil
}

func runSensors(args []string) error {
	if len(args) == 0 || args[0] != "scan" {
		return errors.New("usage: macpowerlab sensors scan")
	}
	if err := ensureSudo(true); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	sample, err := collector.CollectOnce(ctx)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(sample)
}

func runLogs(args []string) error {
	if len(args) == 0 || args[0] != "pack" {
		return errors.New("usage: macpowerlab logs pack [session-dir]")
	}
	cfg := config.Default()
	fs := flag.NewFlagSet("logs pack", flag.ContinueOnError)
	commonFlags(fs, &cfg)
	output := fs.String("output", "", "archive output path")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	dir := ""
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	} else {
		var err error
		dir, err = store.LatestSessionDir(cfg.DataDir)
		if err != nil {
			return err
		}
	}
	if *output == "" {
		*output = filepath.Join(cfg.DataDir, "exports", archive.DefaultName("macpowerlab_logs"))
	}
	if err := archive.Create(dir, *output); err != nil {
		return err
	}
	fmt.Println(*output)
	return nil
}

func runParity(args []string) error {
	cfg := config.Default()
	fs := flag.NewFlagSet("parity", flag.ContinueOnError)
	commonFlags(fs, &cfg)
	iterations := fs.Int("iterations", 3, "number of live comparisons")
	output := fs.String("output", filepath.Join(cfg.DataDir, "parity_report.json"), "report path")
	legacyDir := fs.String("legacy-dir", filepath.Join(projectRoot(), "legacy"), "legacy implementation directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *iterations < 1 || *iterations > 100 {
		return errors.New("iterations must be between 1 and 100")
	}
	if err := ensureSudo(true); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0o700); err != nil {
		return err
	}
	reportValue, err := parity.Run(context.Background(), *legacyDir, *output, *iterations)
	if err != nil {
		return err
	}
	fmt.Printf("Parity passed: %t; report: %s\n", reportValue.Passed, *output)
	if !reportValue.Passed {
		return errors.New("parity differences exceeded tolerances")
	}
	return nil
}

func runParse(args []string) error {
	if len(args) < 2 {
		return errors.New("usage: macpowerlab parse plist|powermetrics|legacy-csv FILE")
	}
	kind, path := args[0], args[1]
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	switch kind {
	case "plist":
		v, err := plistx.Parse(data)
		if err != nil {
			return err
		}
		return encoder.Encode(v)
	case "powermetrics":
		values, err := plistx.ParseNUL(data)
		if err != nil {
			return err
		}
		var out []collector.PowermetricsSnapshot
		for _, v := range values {
			m, ok := v.(map[string]any)
			if ok {
				out = append(out, collector.ParsePowermetrics(m))
			}
		}
		return encoder.Encode(out)
	case "legacy-csv":
		v, err := legacy.ReadLatestCSVRow(path)
		if err != nil {
			return err
		}
		return encoder.Encode(v)
	default:
		return fmt.Errorf("unsupported parse type %q", kind)
	}
}

func runServe(args []string) error {
	cfg := config.Default()
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	commonFlags(fs, &cfg)
	addr := fs.String("addr", "127.0.0.1:0", "loopback listen address")
	tokenFile := fs.String("token-file", "", "private API token file")
	autoMonitor := fs.Bool("auto-monitor", false, "start monitoring when server starts")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := cfg.Prepare(); err != nil {
		return err
	}
	// The bundled SwiftUI app may launch the backend from the user's home
	// directory. Move to the resource root so native and legacy assets resolve.
	if root := projectRoot(); root != "." {
		if err := os.Chdir(root); err != nil {
			return fmt.Errorf("change to MacPowerLab resource root: %w", err)
		}
	}
	ctx, cancel := signalContext()
	defer cancel()
	return server.Serve(ctx, *addr, *tokenFile, server.NewEngine(cfg), *autoMonitor)
}

func runLegacy(args []string) error {
	if len(args) == 0 {
		return errors.New("legacy requires a script name")
	}
	root := projectRoot()
	ctx, cancel := signalContext()
	defer cancel()
	return legacy.RunScript(ctx, filepath.Join(root, "legacy"), args[0], args[1:]...)
}

func ensureSudo(interactive bool) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return execx.EnsureSudo(ctx, interactive)
}

func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-ch:
			cancel()
		case <-ctx.Done():
		}
		signal.Stop(ch)
	}()
	return ctx, cancel
}

func projectRoot() string {
	if hasProjectFiles(".") {
		root, _ := filepath.Abs(".")
		return root
	}
	exe, _ := os.Executable()
	candidates := []string{
		filepath.Dir(exe),
		filepath.Dir(filepath.Dir(exe)),
		filepath.Join(filepath.Dir(exe), "..", "Resources"),
	}
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if hasProjectFiles(candidate) {
			return candidate
		}
	}
	return "."
}

func hasProjectFiles(dir string) bool {
	for _, name := range []string{"native", "legacy", "contracts"} {
		if info, err := os.Stat(filepath.Join(dir, name)); err != nil || !info.IsDir() {
			return false
		}
	}
	return true
}

func usage() {
	fmt.Print(`MacPowerLab v` + version.Version + `

Usage:
  macpowerlab monitor [flags]
  macpowerlab apps [--duration 8s] [--json]
  macpowerlab benchmark list
  macpowerlab benchmark quick|idle|cpu|gpu|memory|mixed|app-audit|battery|ac|thermal|extreme [flags]
  macpowerlab benchmark custom --cpu|--gpu|--memory [flags]
  macpowerlab build-native
  macpowerlab report [session-dir]
  macpowerlab compare
  macpowerlab sensors scan
  macpowerlab logs pack [session-dir]
  macpowerlab parity [--iterations 3]
  macpowerlab parse plist|powermetrics|legacy-csv FILE
  macpowerlab serve [--auto-monitor]
  macpowerlab legacy SCRIPT [args...]
  macpowerlab version

Examples:
  macpowerlab monitor
  macpowerlab monitor --safe
  macpowerlab benchmark list
  macpowerlab benchmark battery
  macpowerlab benchmark extreme --duration 30m
  macpowerlab benchmark custom --cpu --gpu --duration 10m --power-source battery
  macpowerlab apps --duration 10s
`)
}

// parsePositiveInt is retained for compatibility wrapper validation.
func parsePositiveInt(s string) (int, error) {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("expected positive integer, got %q", s)
	}
	return v, nil
}
