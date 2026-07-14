package collector

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

func asString(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case []byte:
		return string(value)
	case fmt.Stringer:
		return value.String()
	case int64:
		return strconv.FormatInt(value, 10)
	case int:
		return strconv.Itoa(value)
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(value)
	default:
		return ""
	}
}

func number(v any) (float64, bool) {
	switch value := v.(type) {
	case float64:
		return value, isFinite(value)
	case float32:
		out := float64(value)
		return out, isFinite(out)
	case int64:
		return float64(value), true
	case int:
		return float64(value), true
	case uint64:
		return float64(value), true
	case uint:
		return float64(value), true
	case string:
		value = strings.TrimSpace(strings.TrimSuffix(value, "%"))
		if value == "" {
			return 0, false
		}
		out, err := strconv.ParseFloat(value, 64)
		return out, err == nil && isFinite(out)
	default:
		return 0, false
	}
}

func asFloat(v any) float64 {
	out, _ := number(v)
	return out
}

func asInt(v any) int {
	out, ok := number(v)
	if !ok {
		return 0
	}
	return int(math.Round(out))
}

func asBool(v any) bool {
	switch value := v.(type) {
	case bool:
		return value
	case string:
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "true", "yes", "1", "on":
			return true
		default:
			return false
		}
	default:
		out, ok := number(v)
		return ok && out != 0
	}
}

// first finds the first requested alias in alias priority order. Search order is
// deterministic: each map is checked directly first, then children are visited
// by sorted key. The prior map-range implementation could return different
// nested values across runs when a plist repeated common keys such as Current,
// Voltage, or ID.
func first(root any, keys ...string) any {
	for _, key := range keys {
		if value, ok := findKey(root, key); ok {
			return value
		}
	}
	return nil
}

func findKey(root any, wanted string) (any, bool) {
	switch value := root.(type) {
	case map[string]any:
		if found, ok := value[wanted]; ok {
			return found, true
		}
		keys := sortedMapKeys(value)
		for _, key := range keys {
			if strings.EqualFold(key, wanted) {
				return value[key], true
			}
		}
		for _, key := range keys {
			if found, ok := findKey(value[key], wanted); ok {
				return found, true
			}
		}
	case []any:
		for _, item := range value {
			if found, ok := findKey(item, wanted); ok {
				return found, true
			}
		}
	}
	return nil, false
}

func direct(root map[string]any, keys ...string) any {
	if root == nil {
		return nil
	}
	// The plist keys used by macOS are stable and case-sensitive. The exact
	// lookup avoids allocating and sorting the complete key set on every sensor
	// read. A deterministic case-insensitive fallback preserves compatibility.
	for _, wanted := range keys {
		if value, ok := root[wanted]; ok {
			return value
		}
	}
	mapKeys := sortedMapKeys(root)
	for _, wanted := range keys {
		for _, key := range mapKeys {
			if strings.EqualFold(key, wanted) {
				return root[key]
			}
		}
	}
	return nil
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func collectNumbers(root any, key string) []float64 {
	out := make([]float64, 0, 8)
	var walk func(any)
	walk = func(v any) {
		switch value := v.(type) {
		case map[string]any:
			keys := sortedMapKeys(value)
			for _, currentKey := range keys {
				item := value[currentKey]
				if strings.EqualFold(currentKey, key) {
					switch numbers := item.(type) {
					case []any:
						for _, candidate := range numbers {
							if n, ok := number(candidate); ok {
								out = append(out, n)
							}
						}
					default:
						if n, ok := number(numbers); ok {
							out = append(out, n)
						}
					}
				}
				walk(item)
			}
		case []any:
			for _, item := range value {
				walk(item)
			}
		}
	}
	walk(root)
	return out
}

func positiveFinite(values []float64) []float64 {
	out := values[:0]
	for _, value := range values {
		if value > 0 && isFinite(value) {
			out = append(out, value)
		}
	}
	return out
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func appDisplayName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Unknown"
	}
	base := filepath.Base(name)
	base = strings.TrimSuffix(base, ".app")
	if strings.Contains(base, ".") {
		parts := strings.Split(base, ".")
		if len(parts) > 2 && parts[0] == "com" {
			base = parts[len(parts)-1]
		}
	}
	return base
}

func categorize(name string) string {
	normalized := strings.ToLower(name)
	switch {
	case strings.Contains(normalized, "windowserver"):
		return "display"
	case strings.Contains(normalized, "safari"),
		strings.Contains(normalized, "webkit"),
		strings.Contains(normalized, "chrome"),
		strings.Contains(normalized, "firefox"):
		return "browser"
	case strings.Contains(normalized, "mds"),
		strings.Contains(normalized, "spotlight"),
		strings.Contains(normalized, "mdworker"):
		return "indexing"
	case strings.Contains(normalized, "backupd"),
		strings.Contains(normalized, "time machine"):
		return "backup"
	case strings.Contains(normalized, "photo"),
		strings.Contains(normalized, "mediaanalysis"):
		return "media-analysis"
	case strings.Contains(normalized, "cloudd"), strings.Contains(normalized, "bird"):
		return "cloud"
	case strings.Contains(normalized, "kernel"):
		return "system"
	case strings.Contains(normalized, "stress"):
		return "benchmark"
	default:
		return "application"
	}
}
