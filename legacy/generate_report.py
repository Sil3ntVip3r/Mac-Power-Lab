#!/usr/bin/env python3
import csv, html, json, math, sys
from pathlib import Path
from datetime import datetime

VERSION = "0.9.0"

def fnum(v):
    try:
        if v in ("", "n/a", None):
            return None
        return float(v)
    except Exception:
        return None

def clean(vals):
    return [v for v in vals if v is not None and math.isfinite(v)]

def vals(rows, key):
    return [fnum(r.get(key)) for r in rows]

def avg(vs):
    vs = clean(vs)
    return sum(vs) / len(vs) if vs else None

def maxv(vs):
    vs = clean(vs)
    return max(vs) if vs else None

def minv(vs):
    vs = clean(vs)
    return min(vs) if vs else None

def fmt(v, suffix="", digits=2):
    if v is None:
        return "n/a"
    try:
        return f"{float(v):.{digits}f}{suffix}"
    except Exception:
        return f"{v}{suffix}"

def esc(s):
    return html.escape("" if s is None else str(s))

def latest_csv():
    logs = sorted(Path("logs").glob("mac_power_*.csv"), key=lambda p: p.stat().st_mtime, reverse=True)
    logs = [p for p in logs if "_events" not in p.name]
    return logs[0] if logs else None

def sibling_paths(csv_path):
    stem = csv_path.with_suffix("").name
    return {
        "events": csv_path.with_name(stem + "_events.jsonl"),
        "debug": csv_path.with_name(stem + "_debug.json"),
        "report": csv_path.with_name(stem + "_report.html"),
    }

def read_csv(path):
    with open(path, newline="", encoding="utf-8", errors="replace") as f:
        return list(csv.DictReader(f))

def read_json(path):
    if not path.exists():
        return {}
    try:
        return json.loads(path.read_text(encoding="utf-8", errors="replace"))
    except Exception:
        return {}

def net_discharge(rows):
    return [abs(v) for v in vals(rows, "net_battery_watts") if v is not None and v < 0]

def net_charge(rows):
    return [v for v in vals(rows, "net_battery_watts") if v is not None and v > 0]

def load_values(rows):
    primary = clean(vals(rows, "primary_total_load_w"))
    if primary:
        return primary
    benchmark = clean(vals(rows, "benchmark_load_w"))
    if benchmark:
        return benchmark
    discharge = net_discharge(rows)
    if discharge:
        return discharge
    bms = clean(vals(rows, "bms_system_power_w"))
    if bms:
        return bms
    eff = clean(vals(rows, "telemetry_system_effective_total_load_w"))
    if eff:
        return eff
    return clean(vals(rows, "soc_power_w"))

def by_phase(rows):
    phases = {}
    for r in rows:
        phases.setdefault(r.get("phase") or "unknown", []).append(r)
    return phases

def score_battery(rows):
    load = load_values(rows)
    peak_load = maxv(load)
    avg_load = avg(load)
    peak_discharge = maxv(net_discharge(rows))
    max_temp = maxv(vals(rows, "battery_temp_c"))
    temp_rise = None
    temps = clean(vals(rows, "battery_temp_c"))
    if len(temps) >= 2:
        temp_rise = max(temps) - min(temps)

    # Practical 0-100 score, tuned for comparison between runs on the same Mac.
    load_score = min(100.0, (peak_load or 0) / 130.0 * 100.0)
    sustained_score = min(100.0, (avg_load or 0) / 105.0 * 100.0)
    thermal_score = 100.0
    if max_temp is not None:
        thermal_score -= max(0.0, max_temp - 38.0) * 5.0
    if temp_rise is not None:
        thermal_score -= max(0.0, temp_rise - 8.0) * 3.0
    thermal_score = max(0.0, min(100.0, thermal_score))
    score = round((load_score * 0.40) + (sustained_score * 0.35) + (thermal_score * 0.25), 1)

    return {
        "score": score,
        "peak_load_w": peak_load,
        "avg_load_w": avg_load,
        "peak_discharge_w": peak_discharge,
        "max_temp_c": max_temp,
        "temp_rise_c": temp_rise,
        "load_score": round(load_score, 1),
        "sustained_score": round(sustained_score, 1),
        "thermal_score": round(thermal_score, 1),
    }

def score_charging(rows):
    charge = net_charge(rows)
    max_charge = maxv(charge)
    avg_charge = avg(charge)
    load_percent = clean(vals(rows, "charger_load_percent"))
    max_load = maxv(load_percent)
    headroom = clean(vals(rows, "charger_headroom_estimate_w"))
    min_head = minv(headroom)

    charge_score = min(100.0, (max_charge or 0) / 100.0 * 100.0)
    headroom_score = 100.0
    if min_head is not None:
        if min_head < 0:
            headroom_score = 20.0
        elif min_head < 5:
            headroom_score = 65.0
        elif min_head < 15:
            headroom_score = 85.0
    load_score = 100.0
    if max_load is not None and max_load > 100:
        load_score = 50.0
    elif max_load is not None and max_load > 97:
        load_score = 80.0
    score = round((charge_score * 0.45) + (headroom_score * 0.35) + (load_score * 0.20), 1)
    return {
        "score": score,
        "max_charge_w": max_charge,
        "avg_charge_w": avg_charge,
        "max_charger_load_percent": max_load,
        "min_headroom_w": min_head,
    }

def test_runs_section(logs_dir):
    path = logs_dir / "test_runs.jsonl"
    if not path.exists():
        return "<p>No structured test runs recorded.</p>"
    rows = []
    for line in path.read_text(encoding="utf-8", errors="replace").splitlines():
        try:
            rows.append(json.loads(line))
        except Exception:
            pass
    if not rows:
        return "<p>No structured test runs recorded.</p>"
    trs = []
    for r in rows[-30:]:
        meta = r.get("metadata") or {}
        trs.append(
            "<tr>"
            f"<td>{esc(r.get('start_time'))}</td>"
            f"<td>{esc(r.get('title'))}</td>"
            f"<td>{esc(r.get('requested_duration_seconds'))}</td>"
            f"<td>{esc(r.get('elapsed_seconds'))}</td>"
            f"<td>{esc(r.get('status'))}</td>"
            f"<td>{esc(meta.get('gpu_profile',''))}</td>"
            f"<td>{esc(r.get('nearest_power_log'))}</td>"
            "</tr>"
        )
    return "<table><tr><th>Start</th><th>Test</th><th>Requested s</th><th>Elapsed s</th><th>Status</th><th>GPU</th><th>Power log</th></tr>" + "".join(trs) + "</table>"

def power_suspects(rows):
    # Simple process-attribution layer from captured process CPU/RAM and top snapshots.
    peak_cpu = maxv(vals(rows, "stress_cpu_percent"))
    peak_ram = maxv(vals(rows, "stress_rss_mb"))
    names = []
    for r in rows:
        n = r.get("stress_process_names")
        if n:
            for part in str(n).split(","):
                if part and part not in names:
                    names.append(part)
    trs = []
    if names:
        trs.append(f"<tr><td>{esc(', '.join(names))}</td><td>{fmt(peak_cpu,'%')}</td><td>{fmt(peak_ram,' MB')}</td><td>Known test workload</td></tr>")
    else:
        trs.append("<tr><td colspan=4>No stress process data captured.</td></tr>")
    return "<table><tr><th>Process/app</th><th>Peak process CPU</th><th>Peak RAM</th><th>Reason</th></tr>" + "".join(trs) + "</table>"

def phase_breakdown(rows):
    trs = []
    for phase, items in by_phase(rows).items():
        trs.append(
            f"<tr><td>{esc(phase)}</td><td>{len(items)}</td>"
            f"<td>{fmt(avg(load_values(items)), ' W')}</td>"
            f"<td>{fmt(maxv(load_values(items)), ' W')}</td>"
            f"<td>{fmt(avg(net_discharge(items)), ' W')}</td>"
            f"<td>{fmt(avg(net_charge(items)), ' W')}</td>"
            f"<td>{fmt(maxv(vals(items,'stress_cpu_percent')), '%')}</td>"
            f"<td>{fmt(maxv(vals(items,'p_cluster_active_percent')), '%')}</td>"
            f"<td>{fmt(maxv(vals(items,'battery_temp_c')), ' °C')}</td></tr>"
        )
    return "<table><tr><th>Phase</th><th>Samples</th><th>Avg primary load</th><th>Peak primary system/load estimate</th><th>Avg battery draw</th><th>Avg charge</th><th>Peak process CPU</th><th>Peak P-cluster active</th><th>Max temp</th></tr>" + "".join(trs) + "</table>"

def event_timeline(events):
    if not events:
        return "<p>No events file found.</p>"
    rows = []
    for e in events:
        rows.append(f"<tr><td>{esc(e.get('timestamp'))}</td><td>{esc(e.get('type'))}</td><td>{esc(e.get('details') or e.get('message') or '')}</td></tr>")
    return "<table><tr><th>Time</th><th>Event</th><th>Details</th></tr>" + "".join(rows) + "</table>"

def mini_svg(rows, series, title):
    points = []
    for key, label in series:
        values = clean(vals(rows, key))
        if not values:
            continue
        points.append((key, label, values))
    if not points:
        return f"<div class='card'><b>{esc(title)}</b><p>No data.</p></div>"
    return f"<div class='card'><b>{esc(title)}</b><p class='small'>Graph omitted in v0.8.6 compact report; use CSV for plotting. Series: {esc(', '.join(label for _,label,_ in points))}</p></div>"

def generate(csv_path, out_path=None):
    csv_path = Path(csv_path)
    rows = read_csv(csv_path)
    if not rows:
        raise SystemExit("CSV has no rows.")

    paths = sibling_paths(csv_path)
    out_path = Path(out_path) if out_path else paths["report"]

    events = []
    if paths["events"].exists():
        for line in paths["events"].read_text(encoding="utf-8", errors="replace").splitlines():
            try:
                events.append(json.loads(line))
            except Exception:
                pass

    last = rows[-1]
    start = rows[0].get("timestamp")
    end = last.get("timestamp")
    first_pct = fnum(rows[0].get("battery_percent"))
    last_pct = fnum(last.get("battery_percent"))

    batt = score_battery(rows)
    charge = score_charging(rows)

    max_load = maxv(load_values(rows))
    avg_load = avg(load_values(rows))
    max_draw = maxv(net_discharge(rows))
    max_charge = maxv(net_charge(rows))
    max_temp = maxv(vals(rows, "battery_temp_c"))
    max_stress_cpu = maxv(vals(rows, "stress_cpu_percent"))
    max_stress_rss = maxv(vals(rows, "stress_rss_mb"))
    max_p_cluster = maxv(vals(rows, "p_cluster_active_percent"))
    max_e_cluster = maxv(vals(rows, "e_cluster_active_percent"))
    max_batt_accept = maxv(vals(rows, "battery_charge_acceptance_w"))
    max_charger_load = maxv(vals(rows, "charger_load_percent"))
    min_headroom = minv(vals(rows, "charger_headroom_estimate_w"))
    health = fnum(last.get("stable_health_percent")) or fnum(last.get("battery_health_percent"))
    cycle = fnum(last.get("cycle_count"))
    cell_delta = maxv(vals(rows, "cell_voltage_delta_mv"))

    overall = round((batt["score"] * 0.60) + (charge["score"] * 0.25) + (min(100, (max_p_cluster or 0)) * 0.15), 1)

    css = """
body{margin:0;font-family:-apple-system,BlinkMacSystemFont,Segoe UI,sans-serif;background:#0b1016;color:#e6edf3}
header{padding:28px 36px;background:#111b26;border-bottom:1px solid #263443}
main{padding:28px 36px 60px;max-width:1200px}
h1{margin:0 0 6px;font-size:30px}h2{margin-top:32px;color:#65e5ff;border-bottom:1px solid #22313f;padding-bottom:8px}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:14px}
.card{background:#111820;border:1px solid #253545;border-radius:14px;padding:16px}
.label{color:#9fb3c8;font-size:13px}.value{font-size:24px;font-weight:700;margin-top:5px}
.good{color:#6ee7a8}.warn{color:#ffd166}.bad{color:#ff7b72}.accent{color:#f78cff}
table{border-collapse:collapse;width:100%;margin:12px 0 22px;background:#111820;border-radius:12px;overflow:hidden}
th,td{text-align:left;padding:9px 11px;border-bottom:1px solid #263443;vertical-align:top}th{background:#182433;color:#c9d7e3}
.small{color:#9fb3c8;font-size:13px}
"""
    doc = f"""<!doctype html><html><head><meta charset='utf-8'><title>MacPowerLab Benchmark Report</title><style>{css}</style></head><body>
<header><h1>MacPowerLab v{VERSION} Battery Benchmark Report</h1>
<div class='small'>Source: {esc(csv_path)}<br>Start: {esc(start)} &nbsp; End: {esc(end)}</div></header><main>

<h2>Benchmark Summary</h2>
<div class='grid'>
<div class='card'><div class='label'>Overall battery system score</div><div class='value accent'>{fmt(overall,'/100',1)}</div></div>
<div class='card'><div class='label'>Battery load score</div><div class='value'>{fmt(batt['score'],'/100',1)}</div></div>
<div class='card'><div class='label'>Charging score</div><div class='value'>{fmt(charge['score'],'/100',1)}</div></div>
<div class='card'><div class='label'>Peak primary system/load estimate</div><div class='value bad'>{fmt(max_load,' W')}</div></div>
<div class='card'><div class='label'>Average primary system/load estimate</div><div class='value'>{fmt(avg_load,' W')}</div></div>
<div class='card'><div class='label'>Peak battery draw</div><div class='value bad'>{fmt(max_draw,' W')}</div></div>
<div class='card'><div class='label'>Peak charge acceptance</div><div class='value good'>{fmt(max_charge,' W')}</div></div>
<div class='card'><div class='label'>Max battery temp</div><div class='value warn'>{fmt(max_temp,' °C')}</div></div>
<div class='card'><div class='label'>Battery % start → end</div><div class='value'>{fmt(first_pct,'%',0)} → {fmt(last_pct,'%',0)}</div></div>
<div class='card'><div class='label'>Peak process CPU</div><div class='value'>{fmt(max_stress_cpu,'%')}</div></div>
<div class='card'><div class='label'>Peak process RAM</div><div class='value'>{fmt(max_stress_rss,' MB')}</div></div>
<div class='card'><div class='label'>Peak P-cluster active</div><div class='value'>{fmt(max_p_cluster,'%')}</div></div>
</div>

<h2>Battery score details</h2>
<table><tr><th>Metric</th><th>Value</th></tr>
<tr><td>Load score</td><td>{fmt(batt['load_score'],'/100',1)}</td></tr>
<tr><td>Sustained score</td><td>{fmt(batt['sustained_score'],'/100',1)}</td></tr>
<tr><td>Thermal score</td><td>{fmt(batt['thermal_score'],'/100',1)}</td></tr>
<tr><td>Temperature rise</td><td>{fmt(batt['temp_rise_c'],' °C')}</td></tr>
</table>

<h2>Charging / adapter score details</h2>
<table><tr><th>Metric</th><th>Value</th></tr>
<tr><td>Charging score</td><td>{fmt(charge['score'],'/100',1)}</td></tr>
<tr><td>Peak battery acceptance</td><td>{fmt(max_batt_accept,' W')}</td></tr>
<tr><td>Max charger load</td><td>{fmt(max_charger_load,'%')}</td></tr>
<tr><td>Minimum headroom</td><td>{fmt(min_headroom,' W')}</td></tr>
</table>

<h2>Power attribution / top suspects</h2>
{power_suspects(rows)}

<h2>Phase breakdown</h2>
{phase_breakdown(rows)}

<h2>Health snapshot</h2>
<table><tr><th>Metric</th><th>Value</th></tr>
<tr><td>Battery health estimate</td><td>{fmt(health,'%')}</td></tr>
<tr><td>Cycle count</td><td>{fmt(cycle,'',0)}</td></tr>
<tr><td>Max cell voltage delta</td><td>{fmt(cell_delta,' mV')}</td></tr>
<tr><td>Energy charged / discharged</td><td>{fmt(fnum(last.get('battery_wh_charged')),' Wh')} / {fmt(fnum(last.get('battery_wh_discharged')),' Wh')}</td></tr>
</table>

<h2>Charts / series inventory</h2>
{mini_svg(rows,[('battery_percent','Battery %')],'Battery percent')}
{mini_svg(rows,[('primary_total_load_w','Primary load'),('net_battery_watts','Net battery W'),('benchmark_load_w','Benchmark load')],'Power')}
{mini_svg(rows,[('stress_cpu_percent','Process CPU'),('p_cluster_active_percent','P-cluster active'),('e_cluster_active_percent','E-cluster active')],'Workload proof')}
{mini_svg(rows,[('battery_temp_c','Battery temp °C')],'Battery temperature')}

<h2>Structured test runs</h2>
{test_runs_section(csv_path.parent)}

<h2>Event timeline</h2>
{event_timeline(events)}

<h2>Notes</h2>
<ul>
<li>On Battery Power, v0.8.6 treats battery discharge watts as the best real total-load signal.</li>
<li>On macOS 27 Beta 2, BatteryData.SystemPower may be missing; PowerTelemetry and cluster residency are used as secondary proof layers.</li>
<li>AC charger load is estimated from system load plus/minus battery charge/discharge. Exact wall power still requires a USB-C/wall watt meter.</li>
</ul>
</main></body></html>"""

    out_path.write_text(doc, encoding="utf-8")
    hist = {
        "created_at": datetime.now().isoformat(timespec="seconds"),
        "csv": str(csv_path),
        "report": str(out_path),
        "overall_score": overall,
        "battery_score": batt["score"],
        "charging_score": charge["score"],
        "peak_primary_load_w": max_load,
        "peak_battery_draw_w": max_draw,
        "max_battery_temp_c": max_temp,
    }
    with (csv_path.parent / "history.jsonl").open("a", encoding="utf-8") as f:
        f.write(json.dumps(hist) + "\n")
    return out_path

def main():
    csv_path = Path(sys.argv[1]) if len(sys.argv) > 1 else latest_csv()
    if csv_path is None:
        print("No logs/mac_power_*.csv files found.")
        raise SystemExit(1)
    report = generate(csv_path)
    print(f"Report written: {report}")

if __name__ == "__main__":
    main()
