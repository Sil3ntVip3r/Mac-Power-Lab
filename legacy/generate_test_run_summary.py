#!/usr/bin/env python3
"""
MacPowerLab Test Run Power Summary v0.8.6

Creates per-test benchmark summaries by joining:
- logs/test_runs.jsonl
- nearest mac_power_*.csv
- timestamp windows for each test

v0.8.6 adds:
- Correct AC vs Battery benchmark classification.
- Wh used/charged per test.
- Runtime projection per workload.
- Battery percent drop per test.
- Better notes for 100% SOC where percent may lag behind real Wh drain.
"""

import csv
import json
import math
from datetime import datetime
from pathlib import Path

VERSION = "0.9.0"

def fnum(v):
    try:
        if v in ("", None, "n/a"):
            return None
        x = float(v)
        if math.isfinite(x):
            return x
    except Exception:
        return None
    return None

def dt(v):
    try:
        return datetime.fromisoformat(str(v))
    except Exception:
        return None

def clean(vals):
    return [v for v in vals if v is not None and math.isfinite(v)]

def vals(rows, key):
    return clean([fnum(r.get(key)) for r in rows])

def avg(vs):
    vs = clean(vs)
    return sum(vs) / len(vs) if vs else None

def med(vs):
    vs = sorted(clean(vs))
    if not vs:
        return None
    n = len(vs)
    return vs[n//2] if n % 2 else (vs[n//2-1] + vs[n//2]) / 2

def maxv(vs):
    vs = clean(vs)
    return max(vs) if vs else None

def minv(vs):
    vs = clean(vs)
    return min(vs) if vs else None

def diff_counter(rows, key):
    if not rows:
        return None
    first = fnum(rows[0].get(key))
    last = fnum(rows[-1].get(key))
    if first is None or last is None:
        return None
    d = last - first
    # These counters should be monotonic within a run. If not, do not report.
    return d if d >= -0.001 else None

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
    try:
        minutes = max(0, float(hours) * 60)
    except Exception:
        return "n/a"
    h = int(minutes // 60)
    m = int(round(minutes % 60))
    if h and m == 60:
        h += 1
        m = 0
    return f"{h}h {m:02d}m" if h else f"{m}m"

def read_csv(path):
    with open(path, newline="", encoding="utf-8", errors="replace") as f:
        return list(csv.DictReader(f))

def read_runs(path):
    rows = []
    if not path.exists():
        return rows
    for line in path.read_text(encoding="utf-8", errors="replace").splitlines():
        if not line.strip():
            continue
        try:
            rows.append(json.loads(line))
        except Exception:
            pass
    return rows

def latest_csv():
    files = sorted(Path("logs").glob("mac_power_*.csv"), key=lambda p: p.stat().st_mtime, reverse=True)
    files = [p for p in files if "_events" not in p.name]
    return files[0] if files else None

def interval_rows(csv_rows, start, end):
    out = []
    for row in csv_rows:
        t = dt(row.get("timestamp"))
        if t and start <= t <= end:
            out.append(row)
    return out

def interval_seconds(rows):
    if len(rows) < 2:
        return None
    a = dt(rows[0].get("timestamp"))
    b = dt(rows[-1].get("timestamp"))
    if not a or not b:
        return None
    return max(0.0, (b - a).total_seconds())

def summarize_interval(rows):
    if not rows:
        return {}

    net = vals(rows, "net_battery_watts")
    charge = [v for v in net if v > 0]
    draw = [abs(v) for v in net if v < 0]
    seconds = interval_seconds(rows)

    sources = {}
    for r in rows:
        src = r.get("primary_total_load_source") or r.get("benchmark_power_source_note") or ""
        if src:
            sources[src] = sources.get(src, 0) + 1

    percent_start = fnum(rows[0].get("battery_percent"))
    percent_end = fnum(rows[-1].get("battery_percent"))
    percent_delta = None
    if percent_start is not None and percent_end is not None:
        percent_delta = percent_end - percent_start

    wh_discharged = diff_counter(rows, "battery_wh_discharged")
    wh_charged = diff_counter(rows, "battery_wh_charged")
    wh_net = None
    first_net = fnum(rows[0].get("battery_wh_net"))
    last_net = fnum(rows[-1].get("battery_wh_net"))
    if first_net is not None and last_net is not None:
        wh_net = last_net - first_net

    avg_draw = avg(draw)
    full_wh = med(vals(rows, "estimated_wh_full"))
    runtime_hours = None
    if full_wh is not None and avg_draw is not None and avg_draw > 0:
        runtime_hours = full_wh / avg_draw

    remaining_wh = med(vals(rows, "estimated_wh_remaining"))
    time_to_empty_hours = None
    if remaining_wh is not None and avg_draw is not None and avg_draw > 0:
        time_to_empty_hours = remaining_wh / avg_draw

    # Detect true battery-assist only when AC Power is present but net battery watts go negative.
    ac_rows = [r for r in rows if r.get("power_source") == "AC Power"]
    ac_draw = [abs(fnum(r.get("net_battery_watts"))) for r in ac_rows if fnum(r.get("net_battery_watts")) is not None and fnum(r.get("net_battery_watts")) < 0]

    return {
        "samples": len(rows),
        "interval_seconds": seconds,
        "start_battery_percent": percent_start,
        "end_battery_percent": percent_end,
        "battery_percent_delta": percent_delta,
        "mode_start": rows[0].get("mode"),
        "mode_end": rows[-1].get("mode"),
        "power_source_start": rows[0].get("power_source"),
        "power_source_end": rows[-1].get("power_source"),
        "all_battery_power": all(r.get("power_source") == "Battery Power" for r in rows),
        "all_ac_power": all(r.get("power_source") == "AC Power" for r in rows),
        "ac_battery_assist_peak_w": maxv(ac_draw),
        "primary_source_counts": sources,

        "primary_load_avg_w": avg(vals(rows, "primary_total_load_w")),
        "primary_load_median_w": med(vals(rows, "primary_total_load_w")),
        "primary_load_peak_w": maxv(vals(rows, "primary_total_load_w")),
        "bms_system_power_avg_w": avg(vals(rows, "bms_system_power_w")),
        "bms_system_power_peak_w": maxv(vals(rows, "bms_system_power_w")),

        "net_battery_w_avg": avg(net),
        "battery_charge_avg_w": avg(charge),
        "battery_charge_peak_w": maxv(charge),
        "battery_draw_avg_w": avg_draw,
        "battery_draw_peak_w": maxv(draw),
        "battery_wh_discharged_interval": wh_discharged,
        "battery_wh_charged_interval": wh_charged,
        "battery_wh_net_interval": wh_net,
        "runtime_projection_hours_full": runtime_hours,
        "time_to_empty_projection_hours": time_to_empty_hours,
        "estimated_full_wh_median": full_wh,
        "estimated_remaining_wh_median": remaining_wh,

        "charger_load_avg_percent": avg(vals(rows, "charger_load_percent")),
        "charger_load_peak_percent": maxv(vals(rows, "charger_load_percent")),
        "charger_headroom_min_w": minv(vals(rows, "charger_headroom_estimate_w")),
        "visible_load_peak_w": maxv(vals(rows, "visible_load_estimate_w")),

        "battery_temp_avg_c": avg(vals(rows, "battery_temp_c")),
        "battery_temp_peak_c": maxv(vals(rows, "battery_temp_c")),
        "cell_delta_peak_mv": maxv(vals(rows, "cell_voltage_delta_mv")),

        "stress_cpu_avg_percent": avg(vals(rows, "stress_cpu_percent")),
        "stress_cpu_peak_percent": maxv(vals(rows, "stress_cpu_percent")),
        "stress_ram_peak_mb": maxv(vals(rows, "stress_rss_mb")),

        "p_cluster_peak_percent": maxv(vals(rows, "p_cluster_active_percent")),
        "e_cluster_peak_percent": maxv(vals(rows, "e_cluster_active_percent")),
        "cpu_power_avg_w": avg(vals(rows, "cpu_power_w")),
        "cpu_power_peak_w": maxv(vals(rows, "cpu_power_w")),
        "gpu_power_avg_w": avg(vals(rows, "gpu_power_w")),
        "gpu_power_peak_w": maxv(vals(rows, "gpu_power_w")),
        "soc_power_peak_w": maxv(vals(rows, "soc_power_w")),
    }

def score_test(summary):
    if not summary:
        return None
    peak = summary.get("primary_load_peak_w") or 0
    avg_load = summary.get("primary_load_avg_w") or 0
    temp = summary.get("battery_temp_peak_c") or 35
    thermal = max(0, min(100, 100 - max(0, temp - 38) * 5))
    load_score = min(100, peak / 130 * 100)
    sustain_score = min(100, avg_load / 100 * 100)
    return round(load_score * 0.45 + sustain_score * 0.35 + thermal * 0.20, 1)

def main():
    logs = Path("logs")
    runs = read_runs(logs / "test_runs.jsonl")
    if not runs:
        print("No logs/test_runs.jsonl found.")
        raise SystemExit(1)

    csv_cache = {}
    summaries = []
    for run in runs:
        start = dt(run.get("start_time"))
        end = dt(run.get("end_time"))
        if not start or not end:
            continue

        nearest = run.get("nearest_power_log")
        csv_path = Path(nearest) if nearest else latest_csv()
        if csv_path and not csv_path.exists():
            csv_path = Path(str(csv_path).lstrip("./"))
        if csv_path and not csv_path.exists():
            csv_path = latest_csv()

        csv_rows = []
        if csv_path:
            if str(csv_path) not in csv_cache:
                csv_cache[str(csv_path)] = read_csv(csv_path)
            csv_rows = csv_cache[str(csv_path)]

        rows = interval_rows(csv_rows, start, end) if csv_rows else []
        s = summarize_interval(rows)
        s["score"] = score_test(s)
        s["title"] = run.get("title")
        s["status"] = run.get("status")
        s["partial_or_stopped"] = run.get("status") in ("stopped", "failed") and (run.get("elapsed_seconds") or 0) < (run.get("requested_duration_seconds") or 0)
        s["start_time"] = run.get("start_time")
        s["end_time"] = run.get("end_time")
        s["requested_duration_seconds"] = run.get("requested_duration_seconds")
        s["elapsed_seconds"] = run.get("elapsed_seconds")
        s["nearest_power_log"] = str(csv_path) if csv_path else None
        s["command"] = " ".join(run.get("actual_command") or [])
        summaries.append(s)

    stamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    json_path = logs / f"test_run_power_summary_{stamp}.json"
    md_path = logs / f"test_run_power_summary_{stamp}.md"
    json_path.write_text(json.dumps({"tool_version": VERSION, "created_at": datetime.now().isoformat(timespec="seconds"), "runs": summaries}, indent=2), encoding="utf-8")

    lines = [f"# MacPowerLab Test Run Power Summary v{VERSION}", "", f"Created: {datetime.now().isoformat(timespec='seconds')}", ""]
    lines += ["## Overall notes", ""]

    valid = [s for s in summaries if s.get("samples")]
    if valid and all(s.get("all_battery_power") for s in valid):
        lines.append("- All captured benchmark tests ran on Battery Power. This is a real unplugged battery-discharge benchmark.")
    elif valid and all(s.get("all_ac_power") for s in valid):
        lines.append("- All captured benchmark tests ran on AC Power. This is a charger/load benchmark, not a battery runtime benchmark.")
    elif valid:
        lines.append("- Benchmark tests contain a mix of AC Power and Battery Power windows.")

    if any((s.get("ac_battery_assist_peak_w") or 0) > 0 for s in valid):
        lines.append("- Battery assist occurred during at least one AC test, meaning the workload briefly exceeded what the adapter supplied.")

    if any((s.get("start_battery_percent") == 100 and (s.get("battery_wh_discharged_interval") or 0) > 0 and (s.get("battery_percent_delta") or 0) == 0) for s in valid):
        lines.append("- Some 100% SOC windows used real Wh before the displayed battery percentage moved. For full-charge tests, Wh counters are more useful than percentage drop.")
    if any(s.get("partial_or_stopped") for s in summaries):
        lines.append("- At least one benchmark was stopped before the requested duration. Treat it as a partial soak result, not a failed hardware result.")
    lines.append("")

    lines += ["## Test summary table", ""]
    lines.append("| Test | Score | Samples | Avg load | Peak load | Wh used | Battery % Δ | Runtime full | Peak temp | Stress CPU peak | GPU peak |")
    lines.append("|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|")
    for s in summaries:
        lines.append(
            f"| {s.get('title','')} | {fmt(s.get('score'),'',1)} | {s.get('samples',0)} | "
            f"{fmt(s.get('primary_load_avg_w'),' W')} | {fmt(s.get('primary_load_peak_w'),' W')} | "
            f"{fmt(s.get('battery_wh_discharged_interval'),' Wh')} | {fmt(s.get('battery_percent_delta'),'%')} | "
            f"{fmt_runtime(s.get('runtime_projection_hours_full'))} | {fmt(s.get('battery_temp_peak_c'),' °C')} | "
            f"{fmt(s.get('stress_cpu_peak_percent'),'%')} | {fmt(s.get('gpu_power_peak_w'),' W')} |"
        )

    for s in summaries:
        lines += [
            "",
            f"## {s.get('title')}",
            "",
            f"- Status: {s.get('status')}" + (" — partial/stopped before requested duration" if s.get("partial_or_stopped") else ""),
            f"- Window: {s.get('start_time')} → {s.get('end_time')}",
            f"- Requested / actual: {fmt(s.get('requested_duration_seconds'),' s',1)} / {fmt(s.get('elapsed_seconds'),' s',1)}",
            f"- Power source: {s.get('power_source_start')} → {s.get('power_source_end')}",
            f"- Battery percent: {fmt(s.get('start_battery_percent'),'%')} → {fmt(s.get('end_battery_percent'),'%')} ({fmt(s.get('battery_percent_delta'),'%')})",
            f"- Wh used / charged / net: {fmt(s.get('battery_wh_discharged_interval'),' Wh')} / {fmt(s.get('battery_wh_charged_interval'),' Wh')} / {fmt(s.get('battery_wh_net_interval'),' Wh')}",
            f"- Runtime projection from full at this load: {fmt_runtime(s.get('runtime_projection_hours_full'))}",
            f"- Time-to-empty projection from this charge level: {fmt_runtime(s.get('time_to_empty_projection_hours'))}",
            f"- Estimated full / remaining Wh median: {fmt(s.get('estimated_full_wh_median'),' Wh')} / {fmt(s.get('estimated_remaining_wh_median'),' Wh')}",
            f"- Primary source counts: {s.get('primary_source_counts')}",
            f"- Avg / median / peak primary load: {fmt(s.get('primary_load_avg_w'),' W')} / {fmt(s.get('primary_load_median_w'),' W')} / {fmt(s.get('primary_load_peak_w'),' W')}",
            f"- Avg / peak battery draw: {fmt(s.get('battery_draw_avg_w'),' W')} / {fmt(s.get('battery_draw_peak_w'),' W')}",
            f"- AC battery assist peak: {fmt(s.get('ac_battery_assist_peak_w'),' W')}",
            f"- Charger load avg / peak: {fmt(s.get('charger_load_avg_percent'),'%')} / {fmt(s.get('charger_load_peak_percent'),'%')}",
            f"- Min charger headroom: {fmt(s.get('charger_headroom_min_w'),' W')}",
            f"- Battery temp avg / peak: {fmt(s.get('battery_temp_avg_c'),' °C')} / {fmt(s.get('battery_temp_peak_c'),' °C')}",
            f"- Cell delta peak: {fmt(s.get('cell_delta_peak_mv'),' mV')}",
            f"- Stress process CPU avg / peak: {fmt(s.get('stress_cpu_avg_percent'),'%')} / {fmt(s.get('stress_cpu_peak_percent'),'%')}",
            f"- Stress RAM peak: {fmt(s.get('stress_ram_peak_mb'),' MB')}",
            f"- P/E cluster peak: {fmt(s.get('p_cluster_peak_percent'),'%')} / {fmt(s.get('e_cluster_peak_percent'),'%')}",
            f"- CPU watts avg / peak: {fmt(s.get('cpu_power_avg_w'),' W')} / {fmt(s.get('cpu_power_peak_w'),' W')}",
            f"- GPU watts avg / peak: {fmt(s.get('gpu_power_avg_w'),' W')} / {fmt(s.get('gpu_power_peak_w'),' W')}",
        ]

    md_path.write_text("\n".join(lines) + "\n", encoding="utf-8")
    print(f"Test run power summary written: {md_path}")
    print(f"JSON summary written: {json_path}")

if __name__ == "__main__":
    main()
