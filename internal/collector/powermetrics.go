package collector

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/execx"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/model"
	plistx "github.com/Sil3ntVip3r/Mac-Power-Lab/internal/plist"
)

const (
	maxPowermetricsFrame = 16 << 20
	maxTasksOutput       = 32 << 20
)

// PowermetricsSnapshot is one machine-readable powermetrics sample.
type PowermetricsSnapshot struct {
	Timestamp  time.Time
	Components model.ComponentSample
	Thermal    model.ThermalSample
	Processes  []model.ProcessActivity
	Status     string
}

// PowermetricsCollector supervises a long-running, lightweight plist stream.
// Full task/coalition collection is intentionally separated into
// CollectTasksOnce because task samples are significantly larger.
type PowermetricsCollector struct {
	Interval time.Duration
}

// NewPowermetricsCollector returns a collector with a validated runtime
// interval. Config validation normally enforces the same bound.
func NewPowermetricsCollector(interval time.Duration) *PowermetricsCollector {
	if interval < time.Second {
		interval = time.Second
	}
	return &PowermetricsCollector{Interval: interval}
}

// Start streams samples until ctx is cancelled. Collection errors are isolated
// on errCh so a failed sampler never terminates battery collection.
func (p *PowermetricsCollector) Start(ctx context.Context) (<-chan PowermetricsSnapshot, <-chan error) {
	out := make(chan PowermetricsSnapshot, 1)
	errCh := make(chan error, 8)
	go func() {
		defer close(out)
		defer close(errCh)
		p.run(ctx, out, errCh)
	}()
	return out, errCh
}

func (p *PowermetricsCollector) run(
	ctx context.Context,
	out chan PowermetricsSnapshot,
	errCh chan<- error,
) {
	if runtime.GOOS != "darwin" {
		nonblockErr(errCh, errors.New("powermetrics collector requires macOS"))
		return
	}

	backoff := time.Second
	candidateIndex := 0
	for ctx.Err() == nil {
		candidates := p.commandCandidates()
		args := candidates[candidateIndex%len(candidates)]
		cmd, stdout, stderr, err := execx.Start(ctx, "/usr/bin/sudo", args...)
		if err != nil {
			nonblockErr(errCh, err)
			sleepContext(ctx, backoff)
			backoff = minDuration(backoff*2, 30*time.Second)
			continue
		}

		type stderrResult struct {
			data      []byte
			truncated bool
			err       error
		}
		stderrDone := make(chan stderrResult, 1)
		go func() {
			data, truncated, readErr := execx.ReadAllLimited(stderr, 4<<20)
			stderrDone <- stderrResult{data: data, truncated: truncated, err: readErr}
		}()

		reader := bufio.NewReaderSize(stdout, 256<<10)
		samples := 0
		for ctx.Err() == nil {
			frame, readErr := readNULFrame(reader, maxPowermetricsFrame)
			if len(bytes.TrimSpace(frame)) > 0 {
				value, parseErr := plistx.Parse(frame)
				if parseErr != nil {
					nonblockErr(errCh, fmt.Errorf("parse powermetrics plist: %w", parseErr))
				} else if root := asMap(value); root != nil {
					snapshot := ParsePowermetrics(root)
					samples++
					publishLatest(ctx, out, snapshot)
				}
			}
			if readErr != nil {
				if !errors.Is(readErr, io.EOF) && ctx.Err() == nil {
					nonblockErr(errCh, fmt.Errorf("powermetrics stream: %w", readErr))
				}
				break
			}
		}

		waitErr := cmd.Wait()
		stderrState := <-stderrDone
		if ctx.Err() != nil {
			return
		}
		if stderrState.err != nil {
			nonblockErr(errCh, fmt.Errorf("read powermetrics stderr: %w", stderrState.err))
		}
		if stderrState.truncated {
			nonblockErr(errCh, errors.New("powermetrics stderr exceeded 4 MiB and was truncated"))
		}
		if waitErr != nil {
			nonblockErr(
				errCh,
				fmt.Errorf(
					"powermetrics exited: %w: %s",
					waitErr,
					strings.TrimSpace(string(stderrState.data)),
				),
			)
		}

		if samples > 0 {
			backoff = time.Second
			candidateIndex = 0
		} else {
			candidateIndex++
			backoff = minDuration(backoff*2, 30*time.Second)
		}
		sleepContext(ctx, backoff)
	}
}

func readNULFrame(reader *bufio.Reader, max int) ([]byte, error) {
	if max <= 0 {
		return nil, errors.New("frame limit must be positive")
	}
	frame := make([]byte, 0, minInt(max, 256<<10))
	tooLarge := false
	for {
		fragment, err := reader.ReadSlice(0)
		if !tooLarge {
			if len(frame)+len(fragment) > max {
				frame = nil
				tooLarge = true
			} else {
				frame = append(frame, fragment...)
			}
		}

		switch {
		case err == nil:
			if tooLarge {
				return nil, fmt.Errorf("powermetrics plist frame exceeded %d bytes", max)
			}
			return bytes.TrimSuffix(frame, []byte{0}), nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			if tooLarge {
				return nil, fmt.Errorf("powermetrics plist frame exceeded %d bytes before EOF", max)
			}
			return frame, io.EOF
		default:
			return frame, err
		}
	}
}

func (p *PowermetricsCollector) commandArgs() []string {
	return p.commandCandidates()[0]
}

func (p *PowermetricsCollector) commandCandidates() [][]string {
	interval := int(p.Interval / time.Millisecond)
	if interval < 1000 {
		interval = 1000
	}
	return [][]string{
		{"-n", "/usr/bin/powermetrics", "-f", "plist", "-i", fmt.Sprint(interval), "--samplers", "cpu_power,gpu_power,thermal"},
		{"-n", "/usr/bin/powermetrics", "-f", "plist", "-i", fmt.Sprint(interval), "--samplers", "cpu_power,gpu_power"},
		{"-n", "/usr/bin/powermetrics", "-f", "plist", "-i", fmt.Sprint(interval), "--samplers", "cpu_power"},
	}
}

// ParsePowermetrics converts a plist dictionary into stable models.
func ParsePowermetrics(root map[string]any) PowermetricsSnapshot {
	snapshot := PowermetricsSnapshot{Timestamp: time.Now(), Status: "ok"}
	elapsedNS := asFloat(direct(root, "elapsed_ns"))
	elapsedS := elapsedNS / 1e9
	if elapsedS <= 0 {
		elapsedS = 1
	}

	processor := asMap(direct(root, "processor"))
	if processor == nil {
		processor = root
	}
	snapshot.Components.CPUWatts = componentPower(processor, elapsedS, "cpu_power", "cpu_energy")
	snapshot.Components.GPUWatts = componentPower(processor, elapsedS, "gpu_power", "gpu_energy")
	snapshot.Components.ANEWatts = componentPower(processor, elapsedS, "ane_power", "ane_energy")
	snapshot.Components.DRAMWatts = componentPower(processor, elapsedS, "dram_power", "dram_energy")
	snapshot.Components.PackageWatts = componentPower(processor, elapsedS, "package_power", "package_energy")
	snapshot.Components.GPUMHz = frequencyMHzFromMap(
		processor,
		[]string{"gpu_frequency_mhz"},
		[]string{"gpu_frequency_khz"},
		[]string{"gpu_freq_hz", "gpu_frequency_hz"},
	)

	clusters := asSlice(direct(processor, "clusters"))
	var weightedFrequency float64
	var activeWeight float64
	for _, item := range clusters {
		clusterMap := asMap(item)
		if clusterMap == nil {
			continue
		}
		name := asString(direct(clusterMap, "name", "cluster_name"))
		frequency := frequencyMHzFromMap(
			clusterMap,
			[]string{"frequency_mhz"},
			[]string{"frequency_khz"},
			[]string{"freq_hz", "frequency_hz"},
		)
		active := asFloat(direct(clusterMap, "active_percent", "active_residency"))
		if active == 0 {
			idleNS := asFloat(direct(clusterMap, "idle_ns"))
			if idleNS > 0 && elapsedNS > 0 {
				active = math.Max(0, math.Min(100, (1-idleNS/elapsedNS)*100))
			}
		}
		power := normalizePower(direct(clusterMap, "power"))
		snapshot.Components.Clusters = append(
			snapshot.Components.Clusters,
			model.ClusterSample{
				Name:          name,
				FrequencyMHz:  frequency,
				ActivePercent: active,
				PowerWatts:    power,
			},
		)
		if frequency > 0 && active > 0 {
			weightedFrequency += frequency * active
			activeWeight += active
		}
	}
	if activeWeight > 0 {
		snapshot.Components.CPUEstimateMHz = weightedFrequency / activeWeight
		snapshot.Components.CPUEstimateSource = "residency-weighted cluster frequency"
	}
	if snapshot.Components.PackageWatts == 0 {
		snapshot.Components.PackageWatts = snapshot.Components.CPUWatts +
			snapshot.Components.GPUWatts +
			snapshot.Components.ANEWatts +
			snapshot.Components.DRAMWatts
	}

	snapshot.Thermal.MacOSPressure = asString(
		first(root, "thermal_pressure", "pressure_level", "current_pressure_level"),
	)
	if snapshot.Thermal.MacOSPressure != "" {
		snapshot.Thermal.Summary = "macOS " + snapshot.Thermal.MacOSPressure
		snapshot.Thermal.Source = "powermetrics thermal sampler"
	}

	// Parse tasks when ParsePowermetrics is used for an offline/full plist. The
	// continuous collector intentionally requests no tasks.
	snapshot.Processes = parseActivities(root, elapsedS)
	return snapshot
}

var powermetricsHelpCache struct {
	sync.Mutex
	text string
}

func powermetricsHelp(ctx context.Context) (string, error) {
	powermetricsHelpCache.Lock()
	if powermetricsHelpCache.text != "" {
		value := powermetricsHelpCache.text
		powermetricsHelpCache.Unlock()
		return value, nil
	}
	powermetricsHelpCache.Unlock()

	helpCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result, err := execx.Run(helpCtx, 4<<20, "/usr/bin/powermetrics", "--help")
	if err != nil {
		return "", fmt.Errorf("read powermetrics help: %w", err)
	}
	value := string(result.Stdout) + string(result.Stderr)
	powermetricsHelpCache.Lock()
	if powermetricsHelpCache.text == "" {
		powermetricsHelpCache.text = value
	}
	value = powermetricsHelpCache.text
	powermetricsHelpCache.Unlock()
	return value, nil
}

// CollectTasksOnce collects one bounded process/coalition sample.
func CollectTasksOnce(ctx context.Context) ([]model.ProcessActivity, error) {
	if runtime.GOOS != "darwin" {
		return nil, errors.New("powermetrics tasks collector requires macOS")
	}

	help, err := powermetricsHelp(ctx)
	if err != nil {
		return nil, err
	}
	args := []string{
		"-n", "/usr/bin/powermetrics",
		"-f", "plist",
		"-n", "1",
		"-i", "1000",
		"--samplers", "tasks",
	}
	for _, flag := range []string{
		"--show-process-coalition",
		"--show-responsible-pid",
		"--show-process-energy",
		"--show-process-gpu",
		"--show-process-io",
		"--show-process-netstats",
		"--show-process-samp-norm",
	} {
		if strings.Contains(help, flag) {
			args = append(args, flag)
		}
	}

	result, err := execx.Run(ctx, maxTasksOutput, "/usr/bin/sudo", args...)
	if err != nil {
		return nil, fmt.Errorf("collect task energy: %w", err)
	}
	if len(bytes.TrimSpace(result.Stdout)) == 0 {
		return nil, errors.New("powermetrics tasks sampler returned no data")
	}

	parts := plistx.SplitNUL(result.Stdout)
	for index := len(parts) - 1; index >= 0; index-- {
		value, parseErr := plistx.Parse(parts[index])
		if parseErr != nil {
			continue
		}
		root := asMap(value)
		if root == nil {
			continue
		}
		elapsedS := asFloat(direct(root, "elapsed_ns")) / 1e9
		if elapsedS <= 0 {
			elapsedS = 1
		}
		activities := parseActivities(root, elapsedS)
		if len(activities) == 0 {
			return nil, errors.New("powermetrics tasks sample contained no task or coalition rows")
		}
		return activities, nil
	}
	return nil, errors.New("powermetrics tasks sample contained no valid plist dictionary")
}

func parseActivities(root map[string]any, elapsedS float64) []model.ProcessActivity {
	if elapsedS <= 0 {
		elapsedS = 1
	}
	var raw []any
	source := "tasks"
	if coalitions := asSlice(direct(root, "coalitions")); len(coalitions) > 0 {
		raw = coalitions
		source = "coalition"
	} else {
		raw = asSlice(direct(root, "tasks"))
	}

	out := make([]model.ProcessActivity, 0, len(raw))
	for _, item := range raw {
		row := asMap(item)
		if row == nil {
			continue
		}
		name := asString(direct(row, "name", "command", "process_name"))
		if name == "" {
			continue
		}
		pid := asInt(direct(row, "pid"))
		responsiblePID := asInt(direct(row, "responsible_pid", "responsiblePid"))
		coalitionID := int64(asFloat(direct(row, "coalition_id", "id")))
		key := fmt.Sprintf("name:%s", name)
		switch {
		case coalitionID != 0:
			key = fmt.Sprintf("coalition:%d", coalitionID)
		case responsiblePID != 0:
			key = fmt.Sprintf("responsible:%d", responsiblePID)
		case pid != 0:
			key = fmt.Sprintf("pid:%d", pid)
		}

		activity := model.ProcessActivity{
			Key:            key,
			Name:           name,
			DisplayName:    appDisplayName(name),
			Category:       categorize(name),
			PID:            pid,
			ResponsiblePID: responsiblePID,
			CoalitionID:    coalitionID,
			Source:         "powermetrics-" + source,
		}
		activity.EnergyImpact = asFloat(direct(row, "energy_impact"))
		if value, present := rateMetricValue(
			row,
			[]string{"energy_impact_per_s"},
			nil,
			elapsedS,
		); present {
			activity.EnergyImpactPerS = value
		} else {
			// Raw Energy Impact is a rate-like score. Fall back only when the
			// explicit per-second field is absent; a present zero is meaningful.
			activity.EnergyImpactPerS = math.Max(0, activity.EnergyImpact)
		}
		activity.CPUTimeMSPerS = rateMetric(
			row,
			[]string{"cputime_ms_per_s", "cputime_sample_ms_per_s"},
			[]string{"cputime_sample_ms"},
			elapsedS,
		)
		activity.GPUTimeMSPerS = rateMetric(
			row,
			[]string{"gputime_ms_per_s"},
			[]string{"gputime_ms"},
			elapsedS,
		)
		activity.InterruptWakeupsPerS = rateMetric(
			row,
			[]string{"intr_wakeups_per_s"},
			[]string{"intr_wakeups"},
			elapsedS,
		)
		activity.IdleWakeupsPerS = rateMetric(
			row,
			[]string{"idle_wakeups_per_s"},
			[]string{"idle_wakeups"},
			elapsedS,
		)
		activity.DiskReadBytesPerS = rateMetric(
			row,
			[]string{"diskio_bytesread_per_s"},
			[]string{"diskio_bytesread"},
			elapsedS,
		)
		activity.DiskWriteBytesPerS = rateMetric(
			row,
			[]string{"diskio_byteswritten_per_s"},
			[]string{"diskio_byteswritten"},
			elapsedS,
		)
		activity.NetworkReadBytesPerS = rateMetric(
			row,
			[]string{"bytes_received_per_s"},
			[]string{"bytes_received"},
			elapsedS,
		)
		activity.NetworkWriteBytesPerS = rateMetric(
			row,
			[]string{"bytes_sent_per_s"},
			[]string{"bytes_sent"},
			elapsedS,
		)
		activity.RSSBytes = int64(asFloat(direct(row, "resident_size", "rss_bytes")))
		out = append(out, activity)
	}

	sort.SliceStable(out, func(i, j int) bool {
		left := activityWeight(out[i])
		right := activityWeight(out[j])
		if left == right {
			return out[i].Key < out[j].Key
		}
		return left > right
	})
	const maxActivities = 512
	if len(out) > maxActivities {
		out = out[:maxActivities]
	}
	return out
}

func rateMetric(
	row map[string]any,
	perSecondKeys []string,
	totalKeys []string,
	elapsedS float64,
) float64 {
	value, _ := rateMetricValue(row, perSecondKeys, totalKeys, elapsedS)
	return value
}

func rateMetricValue(
	row map[string]any,
	perSecondKeys []string,
	totalKeys []string,
	elapsedS float64,
) (float64, bool) {
	if raw := direct(row, perSecondKeys...); raw != nil {
		value, ok := number(raw)
		if !ok {
			return 0, false
		}
		return math.Max(0, value), true
	}
	if raw := direct(row, totalKeys...); raw != nil && elapsedS > 0 {
		value, ok := number(raw)
		if !ok {
			return 0, false
		}
		return math.Max(0, value/elapsedS), true
	}
	return 0, false
}

func publishLatest(ctx context.Context, out chan PowermetricsSnapshot, snapshot PowermetricsSnapshot) {
	select {
	case out <- snapshot:
		return
	default:
	}

	// The monitor consumes only the newest system snapshot. Replace one stale
	// buffered value rather than blocking the privileged child process.
	select {
	case <-out:
	default:
	}
	select {
	case out <- snapshot:
	case <-ctx.Done():
	}
}

func activityWeight(activity model.ProcessActivity) float64 {
	return activity.EnergyImpactPerS*4 +
		activity.CPUTimeMSPerS +
		activity.GPUTimeMSPerS*2 +
		activity.InterruptWakeupsPerS*.1 +
		activity.IdleWakeupsPerS*.1
}

func componentPower(root map[string]any, elapsedS float64, powerKey, energyKey string) float64 {
	if value := direct(root, powerKey); value != nil {
		return normalizePower(value)
	}
	energy := asFloat(direct(root, energyKey))
	if energy == 0 || elapsedS <= 0 {
		return 0
	}
	switch {
	case energy > 1e9:
		return energy / 1e9 / elapsedS
	case energy > 1e6:
		return energy / 1e6 / elapsedS
	case energy > 1e3:
		return energy / 1e3 / elapsedS
	default:
		return energy / elapsedS
	}
}

func frequencyMHzFromMap(
	root map[string]any,
	mhzKeys []string,
	khzKeys []string,
	hzKeys []string,
) float64 {
	if value := direct(root, mhzKeys...); value != nil {
		return math.Max(0, asFloat(value))
	}
	if value := direct(root, khzKeys...); value != nil {
		return math.Max(0, asFloat(value)/1e3)
	}
	if value := direct(root, hzKeys...); value != nil {
		return math.Max(0, asFloat(value)/1e6)
	}
	return 0
}

func nonblockErr(ch chan<- error, err error) {
	if err == nil {
		return
	}
	select {
	case ch <- err:
	default:
	}
}

func sleepContext(ctx context.Context, duration time.Duration) {
	if duration <= 0 {
		return
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

// Available returns whether powermetrics is installed.
func (p *PowermetricsCollector) Available() bool {
	_, err := exec.LookPath("/usr/bin/powermetrics")
	return err == nil
}
