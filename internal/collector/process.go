package collector

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/execx"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/model"
)

const maxFallbackProcesses = 512

// CollectPS is a bounded, low-confidence fallback when task-level
// powermetrics is unavailable. The result is sorted by observed CPU use and
// capped so a process-spawn storm cannot inflate the live monitor's memory.
func CollectPS(ctx context.Context) ([]model.ProcessActivity, error) {
	result, err := execx.Run(
		ctx,
		8<<20,
		"/bin/ps",
		"-axo",
		"pid=,ppid=,pcpu=,pmem=,rss=,comm=",
	)
	if err != nil {
		return nil, fmt.Errorf("collect process fallback: %w", err)
	}
	return parsePSOutput(result.Stdout), nil
}

func parsePSOutput(data []byte) []model.ProcessActivity {
	out := make([]model.ProcessActivity, 0, 128)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	// Process command lines can be longer than Scanner's default token limit.
	// ps output is already bounded by execx, so a 1 MiB line ceiling is safe.
	scanner.Buffer(make([]byte, 32<<10), 1<<20)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 6 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid <= 0 {
			continue
		}
		cpu, err := strconv.ParseFloat(fields[2], 64)
		if err != nil || cpu < 0 {
			continue
		}
		rssKiB, err := strconv.ParseInt(fields[4], 10, 64)
		if err != nil || rssKiB < 0 {
			continue
		}
		name := strings.Join(fields[5:], " ")
		if name == "" {
			continue
		}
		out = append(out, model.ProcessActivity{
			Key:           fmt.Sprintf("pid:%d", pid),
			Name:          name,
			DisplayName:   appDisplayName(name),
			Category:      categorize(name),
			PID:           pid,
			CPUTimeMSPerS: cpu * 10,
			RSSBytes:      rssKiB * 1024,
			Source:        "ps-fallback",
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CPUTimeMSPerS == out[j].CPUTimeMSPerS {
			return out[i].PID < out[j].PID
		}
		return out[i].CPUTimeMSPerS > out[j].CPUTimeMSPerS
	})
	if len(out) > maxFallbackProcesses {
		out = out[:maxFallbackProcesses]
	}
	return out
}
