#!/usr/bin/env python3
import csv
import json
import sys
from pathlib import Path

def fnum(v):
    try:
        if v in ("", "n/a", None):
            return None
        return float(v)
    except Exception:
        return None

def avg(vals):
    vals = [v for v in vals if v is not None]
    return sum(vals) / len(vals) if vals else None

def maxv(vals):
    vals = [v for v in vals if v is not None]
    return max(vals) if vals else None

def minv(vals):
    vals = [v for v in vals if v is not None]
    return min(vals) if vals else None

def fmt(v, suffix="", digits=2):
    if v is None:
        return "n/a"
    return f"{v:.{digits}f}{suffix}"

def read_events_for_csv(csv_path):
    stem = csv_path.with_suffix("").name
    events_path = csv_path.parent / f"{stem}_events.jsonl"
    events = []
    if events_path.exists():
        for line in events_path.read_text(encoding="utf-8").splitlines():
            try:
                events.append(json.loads(line))
            except Exception:
                pass
    return events, events_path

def main():
    path = Path(sys.argv[1]) if len(sys.argv) > 1 else None
    if path is None:
        logs = sorted(Path("logs").glob("mac_power_*.csv"), key=lambda p: p.stat().st_mtime, reverse=True)
        logs = [p for p in logs if not p.name.endswith("_events.csv")]
        if not logs:
            print("No logs found.")
            sys.exit(1)
        path = logs[0]

    if not path.exists():
        print(f"CSV not found: {path}")
        sys.exit(1)

    rows = list(csv.DictReader(path.open()))
    if not rows:
        print("No rows found.")
        return

    phase_key = "auto_phase" if "auto_phase" in rows[0] else "phase"

    phases = {}
    for r in rows:
        phase = r.get(phase_key) or r.get("phase") or "unmarked"
        phases.setdefault(phase, []).append(r)

    events, events_path = read_events_for_csv(path)

    print("MacPowerLab CSV Summary v0.4.2")
    print("=======================")
    print(f"File: {path}")
    print(f"Rows: {len(rows)}")
    if events:
        print(f"Events: {len(events)} ({events_path})")
    print()

    start = rows[0].get("timestamp")
    end = rows[-1].get("timestamp")
    print(f"Start: {start}")
    print(f"End:   {end}")
    print()

    totals = rows[-1]
    print("Session totals from last row")
    print("----------------------------")
    print(f"Battery charged:    {fmt(fnum(totals.get('battery_wh_charged')), ' Wh')}")
    print(f"Battery discharged: {fmt(fnum(totals.get('battery_wh_discharged')), ' Wh')}")
    print(f"Battery net:        {fmt(fnum(totals.get('battery_wh_net')), ' Wh')}")
    print(f"Peak discharge:     {fmt(fnum(totals.get('max_discharge_watts')), ' W')}")
    print(f"Peak charge:        {fmt(fnum(totals.get('max_charge_watts')), ' W')}")
    print(f"Max battery temp:   {fmt(fnum(totals.get('max_battery_temp_c')), ' °C')}")
    print()

    for phase, items in phases.items():
        net = [fnum(r.get("net_battery_watts")) for r in items]
        mac = [fnum(r.get("whole_mac_watts_estimate")) for r in items]
        temp = [fnum(r.get("battery_temp_c")) for r in items]
        soc = [fnum(r.get("soc_power_w")) for r in items]
        cpu = [fnum(r.get("cpu_power_w")) for r in items]
        gpu = [fnum(r.get("gpu_power_w")) for r in items]
        bms = [fnum(r.get("bms_system_power_w")) for r in items]
        telem = [fnum(r.get("telemetry_system_load_w")) for r in items]
        cell_delta = [fnum(r.get("cell_voltage_delta_mv")) for r in items]
        charge = [v for v in net if v is not None and v > 0]
        discharge = [abs(v) for v in net if v is not None and v < 0]

        print(f"Phase: {phase}")
        print(f"  Samples:           {len(items)}")
        print(f"  Avg net battery:   {fmt(avg(net), ' W')}")
        print(f"  Max discharge:     {fmt(maxv(discharge), ' W')}")
        print(f"  Max charge:        {fmt(maxv(charge), ' W')}")
        print(f"  Avg Mac use est:   {fmt(avg(mac), ' W')}")
        print(f"  Max battery temp:  {fmt(maxv(temp), ' °C')}")
        print(f"  Avg SoC power:     {fmt(avg(soc), ' W')}")
        print(f"  Avg CPU power:     {fmt(avg(cpu), ' W')}")
        print(f"  Avg GPU power:     {fmt(avg(gpu), ' W')}")
        print(f"  Avg BMS SysPower:  {fmt(avg(bms), ' W')}")
        print(f"  Avg PT Load est: {fmt(avg(telem), ' W')}")
        print(f"  Max cell delta:    {fmt(maxv(cell_delta), ' mV')}")
        print()

    if events:
        print("Detected events")
        print("---------------")
        for e in events[-30:]:
            print(f"{e.get('timestamp')}  {e.get('type')}  mode={e.get('mode')} source={e.get('power_source')} net={fmt(fnum(e.get('net_battery_watts')), ' W')}")
        print()

if __name__ == "__main__":
    main()
