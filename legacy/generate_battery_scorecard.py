#!/usr/bin/env python3
"""
MacPowerLab Battery Benchmark Scorecard v0.8.6

Creates a compact benchmark scorecard from the latest test_run_power_summary_*.json.

Outputs:
- logs/battery_benchmark_scorecard_YYYYMMDD_HHMMSS.md
- logs/battery_benchmark_scorecard_YYYYMMDD_HHMMSS.json
- logs/battery_benchmark_scores.jsonl persistent history

The score is intended for comparing the same Mac across:
- macOS builds
- battery ages
- power modes
- charger/cable setups
- app/background-load states
"""

import json
import math
from datetime import datetime
from pathlib import Path

VERSION = "0.9.0"

TEST_ALIASES = {
    "cpu": ["cpu stress"],
    "gpu": ["gpu stress"],
    "memory": ["memory bandwidth"],
    "extreme": ["extreme power", "cpu + extreme gpu"],
}

# Same-Mac comparative targets. These are intentionally practical rather than absolute.
TARGETS = {
    "cpu_runtime_h": 2.0,
    "gpu_runtime_h": 1.5,
    "memory_runtime_h": 2.0,
    "extreme_runtime_h": 1.0,
    "extreme_avg_load_w": 95.0,
    "extreme_peak_load_w": 110.0,
    "max_battery_temp_c": 40.0,
    "excellent_cell_delta_mv": 20.0,
    "acceptable_cell_delta_mv": 35.0,
}

def fnum(v):
    try:
        if v in ("", None, "n/a"):
            return None
        x = float(v)
        if math.isfinite(x):
            return x
    except Exception:
        pass
    return None

def clamp(v, lo=0.0, hi=100.0):
    return max(lo, min(hi, v))

def fmt(v, suffix="", digits=2):
    if v is None:
        return "n/a"
    try:
        return f"{float(v):.{digits}f}{suffix}"
    except Exception:
        return f"{v}{suffix}"

def fmt_runtime(hours):
    if hours is None:
        return "n/a"
    minutes = max(0, float(hours) * 60)
    h = int(minutes // 60)
    m = int(round(minutes % 60))
    if m == 60:
        h += 1
        m = 0
    return f"{h}h {m:02d}m" if h else f"{m}m"

def latest_summary():
    files = sorted(Path("logs").glob("test_run_power_summary_*.json"), key=lambda p: p.stat().st_mtime, reverse=True)
    return files[0] if files else None

def load_summary(path):
    return json.loads(Path(path).read_text(encoding="utf-8", errors="replace"))

def classify_run(runs):
    valid = [r for r in runs if r.get("samples")]
    if not valid:
        return "unknown"
    if all(r.get("all_battery_power") for r in valid):
        return "battery_discharge"
    if all(r.get("all_ac_power") for r in valid):
        return "ac_adapter"
    return "mixed"

def find_test(runs, key):
    aliases = TEST_ALIASES[key]
    for run in runs:
        title = (run.get("title") or "").lower()
        if any(alias in title for alias in aliases):
            return run
    return None

def score_runtime(actual, target):
    if actual is None:
        return None
    return clamp((actual / target) * 100.0)

def score_temperature(max_temp):
    if max_temp is None:
        return None
    if max_temp <= 38:
        return 100.0
    if max_temp <= TARGETS["max_battery_temp_c"]:
        return clamp(100.0 - (max_temp - 38.0) * 7.5)
    return clamp(85.0 - (max_temp - TARGETS["max_battery_temp_c"]) * 10.0)

def score_cell_delta(delta):
    if delta is None:
        return None
    excellent = TARGETS["excellent_cell_delta_mv"]
    acceptable = TARGETS["acceptable_cell_delta_mv"]
    if delta <= excellent:
        return 100.0
    if delta <= acceptable:
        return clamp(100.0 - ((delta - excellent) / (acceptable - excellent)) * 25.0)
    return clamp(75.0 - (delta - acceptable) * 2.0)

def avg_present(values):
    vals = [v for v in values if v is not None]
    return sum(vals) / len(vals) if vals else None

def compute_scorecard(summary):
    runs = summary.get("runs", [])
    cpu = find_test(runs, "cpu")
    gpu = find_test(runs, "gpu")
    memory = find_test(runs, "memory")
    extreme = find_test(runs, "extreme")
    valid = [r for r in runs if r.get("samples")]

    run_type = classify_run(runs)

    runtime_scores = {
        "cpu_runtime_score": score_runtime(fnum(cpu.get("runtime_projection_hours_full")) if cpu else None, TARGETS["cpu_runtime_h"]),
        "gpu_runtime_score": score_runtime(fnum(gpu.get("runtime_projection_hours_full")) if gpu else None, TARGETS["gpu_runtime_h"]),
        "memory_runtime_score": score_runtime(fnum(memory.get("runtime_projection_hours_full")) if memory else None, TARGETS["memory_runtime_h"]),
        "extreme_runtime_score": score_runtime(fnum(extreme.get("runtime_projection_hours_full")) if extreme else None, TARGETS["extreme_runtime_h"]),
    }
    endurance_score = avg_present(runtime_scores.values())

    extreme_avg = fnum(extreme.get("primary_load_avg_w")) if extreme else None
    extreme_peak = fnum(extreme.get("primary_load_peak_w")) if extreme else None
    sustained_load_score = avg_present([
        clamp((extreme_avg / TARGETS["extreme_avg_load_w"]) * 100.0) if extreme_avg is not None else None,
        clamp((extreme_peak / TARGETS["extreme_peak_load_w"]) * 100.0) if extreme_peak is not None else None,
    ])

    max_temp = max([fnum(r.get("battery_temp_peak_c")) for r in valid if fnum(r.get("battery_temp_peak_c")) is not None], default=None)
    thermal_score = score_temperature(max_temp)

    max_cell_delta = max([fnum(r.get("cell_delta_peak_mv")) for r in valid if fnum(r.get("cell_delta_peak_mv")) is not None], default=None)
    stability_score = score_cell_delta(max_cell_delta)
    if all((r.get("status") == "complete") for r in valid) and stability_score is not None:
        stability_score = min(100.0, stability_score + 3.0)

    # Correct weighted average:
    # v0.7.8-v0.8.6 incorrectly averaged the already-weighted components, which
    # made excellent runs show around 25/100. The correct formula is:
    #   sum(score * weight) / sum(active weights)
    weighted_parts = []
    if endurance_score is not None:
        weighted_parts.append((endurance_score, 0.35))
    if sustained_load_score is not None:
        weighted_parts.append((sustained_load_score, 0.25))
    if thermal_score is not None:
        weighted_parts.append((thermal_score, 0.25))
    if stability_score is not None:
        weighted_parts.append((stability_score, 0.15))

    if weighted_parts:
        overall = sum(component_score * weight for component_score, weight in weighted_parts) / sum(weight for _, weight in weighted_parts)
    else:
        overall = None

    total_wh_used = sum([fnum(r.get("battery_wh_discharged_interval")) or 0.0 for r in valid])
    total_percent_delta = sum([fnum(r.get("battery_percent_delta")) or 0.0 for r in valid])
    total_seconds = sum([fnum(r.get("interval_seconds")) or 0.0 for r in valid])

    card = {
        "tool_version": VERSION,
        "created_at": datetime.now().isoformat(timespec="seconds"),
        "summary_source_version": summary.get("tool_version"),
        "run_type": run_type,
        "overall_score": round(overall, 1) if overall is not None else None,
        "endurance_score": round(endurance_score, 1) if endurance_score is not None else None,
        "sustained_load_score": round(sustained_load_score, 1) if sustained_load_score is not None else None,
        "thermal_score": round(thermal_score, 1) if thermal_score is not None else None,
        "stability_score": round(stability_score, 1) if stability_score is not None else None,
        "max_battery_temp_c": max_temp,
        "max_cell_delta_mv": max_cell_delta,
        "total_wh_used": total_wh_used,
        "total_battery_percent_delta": total_percent_delta,
        "total_test_seconds": total_seconds,
        "runtime_scores": {k: (round(v, 1) if v is not None else None) for k, v in runtime_scores.items()},
        "score_formula": "overall = weighted_average(endurance 35%, sustained_load 25%, thermal 25%, stability 15%)",
        "score_formula_fix": "v0.8.6 fixed v0.7.8-v0.8.6 weighted-score averaging bug",
        "tests": [],
    }

    for key, run in [("cpu", cpu), ("gpu", gpu), ("memory", memory), ("extreme", extreme)]:
        if not run:
            continue
        card["tests"].append({
            "key": key,
            "title": run.get("title"),
            "score": run.get("score"),
            "avg_load_w": fnum(run.get("primary_load_avg_w")),
            "peak_load_w": fnum(run.get("primary_load_peak_w")),
            "wh_used": fnum(run.get("battery_wh_discharged_interval")),
            "battery_percent_delta": fnum(run.get("battery_percent_delta")),
            "runtime_projection_hours_full": fnum(run.get("runtime_projection_hours_full")),
            "time_to_empty_projection_hours": fnum(run.get("time_to_empty_projection_hours")),
            "peak_temp_c": fnum(run.get("battery_temp_peak_c")),
            "cell_delta_peak_mv": fnum(run.get("cell_delta_peak_mv")),
            "stress_cpu_peak_percent": fnum(run.get("stress_cpu_peak_percent")),
            "cpu_power_peak_w": fnum(run.get("cpu_power_peak_w")),
            "gpu_power_peak_w": fnum(run.get("gpu_power_peak_w")),
        })

    return card

def write_markdown(card, out_path):
    lines = [f"# MacPowerLab Battery Benchmark Scorecard v{VERSION}", "", f"Created: {card['created_at']}", ""]
    lines += [
        "## Scorecard",
        "",
        f"- Run type: `{card['run_type']}`",
        f"- Overall battery benchmark score: **{fmt(card['overall_score'], '/100', 1)}**",
        f"- Endurance score: {fmt(card['endurance_score'], '/100', 1)}",
        f"- Sustained load score: {fmt(card['sustained_load_score'], '/100', 1)}",
        f"- Thermal score: {fmt(card['thermal_score'], '/100', 1)}",
        f"- Stability score: {fmt(card['stability_score'], '/100', 1)}",
        "",
        "## Session totals",
        "",
        f"- Total Wh used during benchmark tests: {fmt(card['total_wh_used'], ' Wh')}",
        f"- Total displayed battery percent change: {fmt(card['total_battery_percent_delta'], '%')}",
        f"- Total captured test time: {fmt((card['total_test_seconds'] or 0)/60, ' min')}",
        f"- Max battery temperature: {fmt(card['max_battery_temp_c'], ' °C')}",
        f"- Max cell voltage delta: {fmt(card['max_cell_delta_mv'], ' mV')}",
        "",
        "## Per-test summary",
        "",
        "| Test | Runtime from full | Avg load | Peak load | Wh used | Battery % Δ | Peak temp | CPU peak | GPU peak |",
        "|---|---:|---:|---:|---:|---:|---:|---:|---:|",
    ]
    for t in card["tests"]:
        lines.append(
            f"| {t['title']} | {fmt_runtime(t.get('runtime_projection_hours_full'))} | "
            f"{fmt(t.get('avg_load_w'), ' W')} | {fmt(t.get('peak_load_w'), ' W')} | "
            f"{fmt(t.get('wh_used'), ' Wh')} | {fmt(t.get('battery_percent_delta'), '%')} | "
            f"{fmt(t.get('peak_temp_c'), ' °C')} | {fmt(t.get('cpu_power_peak_w'), ' W')} | {fmt(t.get('gpu_power_peak_w'), ' W')} |"
        )
    lines += [
        "",
        "## Notes",
        "",
        "- The score is meant for comparing the same Mac across macOS builds, battery health, power modes, app/background load, and charger setups.",
        "- Overall score formula: endurance 35%, sustained load 25%, thermals 25%, stability 15%.",
        "- v0.8.6 fixes the earlier weighted-score averaging bug that could make excellent runs show around 25/100.",
        "- For Battery Power runs, runtime projection is based on the Mac's estimated full Wh divided by measured average battery draw.",
        "- For runs starting near full charge, Wh counters are more useful than displayed battery percentage.",
        "",
    ]
    out_path.write_text("\n".join(lines), encoding="utf-8")

def append_history(card):
    hist = Path("logs") / "battery_benchmark_scores.jsonl"
    with hist.open("a", encoding="utf-8") as f:
        f.write(json.dumps(card) + "\n")

def write_comparison():
    hist = Path("logs") / "battery_benchmark_scores.jsonl"
    if not hist.exists():
        return None
    rows = []
    for line in hist.read_text(encoding="utf-8", errors="replace").splitlines():
        try:
            rows.append(json.loads(line))
        except Exception:
            pass
    if len(rows) < 2:
        return None
    prev, latest = rows[-2], rows[-1]
    out = Path("logs") / f"battery_benchmark_compare_{datetime.now().strftime('%Y%m%d_%H%M%S')}.md"

    def delta(key):
        a = fnum(prev.get(key))
        b = fnum(latest.get(key))
        if a is None or b is None:
            return "n/a"
        d = b - a
        return f"{d:+.2f}"

    lines = [
        f"# MacPowerLab Battery Benchmark Comparison v{VERSION}",
        "",
        f"Previous: {prev.get('created_at')}",
        f"Latest: {latest.get('created_at')}",
        "",
        "| Metric | Previous | Latest | Δ |",
        "|---|---:|---:|---:|",
        f"| Overall score | {fmt(prev.get('overall_score'))} | {fmt(latest.get('overall_score'))} | {delta('overall_score')} |",
        f"| Endurance score | {fmt(prev.get('endurance_score'))} | {fmt(latest.get('endurance_score'))} | {delta('endurance_score')} |",
        f"| Sustained load score | {fmt(prev.get('sustained_load_score'))} | {fmt(latest.get('sustained_load_score'))} | {delta('sustained_load_score')} |",
        f"| Thermal score | {fmt(prev.get('thermal_score'))} | {fmt(latest.get('thermal_score'))} | {delta('thermal_score')} |",
        f"| Max temp | {fmt(prev.get('max_battery_temp_c'), ' °C')} | {fmt(latest.get('max_battery_temp_c'), ' °C')} | {delta('max_battery_temp_c')} |",
        f"| Total Wh used | {fmt(prev.get('total_wh_used'), ' Wh')} | {fmt(latest.get('total_wh_used'), ' Wh')} | {delta('total_wh_used')} |",
    ]
    out.write_text("\n".join(lines) + "\n", encoding="utf-8")
    return out

def main():
    src = latest_summary()
    if not src:
        print("No logs/test_run_power_summary_*.json found. Run ./generate_test_run_summary.sh first.")
        raise SystemExit(1)

    summary = load_summary(src)
    card = compute_scorecard(summary)
    stamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    json_path = Path("logs") / f"battery_benchmark_scorecard_{stamp}.json"
    md_path = Path("logs") / f"battery_benchmark_scorecard_{stamp}.md"
    json_path.write_text(json.dumps(card, indent=2), encoding="utf-8")
    write_markdown(card, md_path)
    append_history(card)
    compare_path = write_comparison()

    print(f"Battery benchmark scorecard written: {md_path}")
    print(f"JSON scorecard written: {json_path}")
    if compare_path:
        print(f"Comparison written: {compare_path}")

if __name__ == "__main__":
    main()
