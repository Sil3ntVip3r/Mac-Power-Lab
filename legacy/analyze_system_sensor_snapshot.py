#!/usr/bin/env python3
"""
MacPowerLab system sensor snapshot analyzer v0.7.1

This is intentionally broad and cautious:
- It records what macOS exposes.
- It does not assume every Mac exposes CPU/GPU temps or fan speeds.
- It keeps raw powermetrics output for later parser improvements.
"""

import json
import re
import sys
from datetime import datetime
from pathlib import Path

VERSION = "0.9.0"


def read(path):
    try:
        return Path(path).read_text(encoding="utf-8", errors="replace")
    except Exception:
        return ""


def load_json(path):
    try:
        return json.loads(read(path))
    except Exception:
        return {}


def find_interesting_sensor_lines(text):
    out = []
    keywords = ["temp", "temperature", "fan", "rpm", "thermal", "pressure", "CPU Power", "GPU Power", "ANE Power", "DRAM"]
    for line in text.splitlines():
        clean = line.strip()
        if not clean:
            continue
        low = clean.lower()
        if any(k.lower() in low for k in keywords):
            out.append(clean)
    # de-duplicate while preserving order
    seen = set()
    result = []
    for line in out:
        if line not in seen:
            result.append(line)
            seen.add(line)
    return result[:250]


def parse_power_values(text):
    out = {}
    patterns = {
        "cpu_power_mw": r"CPU Power:\s*([0-9.]+)\s*mW",
        "gpu_power_mw": r"GPU Power:\s*([0-9.]+)\s*mW",
        "ane_power_mw": r"ANE Power:\s*([0-9.]+)\s*mW",
        "combined_power_mw": r"Combined Power.*?:\s*([0-9.]+)\s*mW",
    }
    for key, pat in patterns.items():
        m = re.search(pat, text, re.I)
        if m:
            try:
                out[key] = float(m.group(1))
            except Exception:
                pass
    cluster_patterns = {
        "e_cluster_freq_mhz": r"^\s*E-Cluster HW active frequency:\s*([0-9.]+)\s*MHz",
        "e_cluster_active_percent": r"^\s*E-Cluster HW active residency:\s*([0-9.]+)%",
        "p0_cluster_freq_mhz": r"^\s*P0-Cluster HW active frequency:\s*([0-9.]+)\s*MHz",
        "p0_cluster_active_percent": r"^\s*P0-Cluster HW active residency:\s*([0-9.]+)%",
        "p1_cluster_freq_mhz": r"^\s*P1-Cluster HW active frequency:\s*([0-9.]+)\s*MHz",
        "p1_cluster_active_percent": r"^\s*P1-Cluster HW active residency:\s*([0-9.]+)%",
    }
    for key, pat in cluster_patterns.items():
        m = re.search(pat, text, re.I | re.M)
        if m:
            try:
                out[key] = float(m.group(1))
            except Exception:
                pass
    return out


def parse_top_processes(text):
    lines = text.splitlines()
    # Keep the most useful top/ps lines but avoid massive output.
    keep = []
    for line in lines:
        if not line.strip():
            continue
        if "COMMAND" in line or re.match(r"\s*\d+\s+", line):
            keep.append(line.rstrip())
        if len(keep) >= 30:
            break
    return keep


def get_sp_name(sp, section_name):
    sections = sp.get("SPHardwareDataType") or sp.get("SPPowerDataType") or sp.get("SPMemoryDataType") or []
    for s in sections:
        if s.get("_name") == section_name:
            return s
    return sections[0] if sections else {}


def analyze(scan_dir):
    scan_dir = Path(scan_dir)
    sw = read(scan_dir / "sw_vers.txt")
    uptime = read(scan_dir / "uptime.txt")
    vm = read(scan_dir / "vm_stat.txt")
    pmset_batt = read(scan_dir / "pmset_batt.txt")
    pmset_therm = read(scan_dir / "pmset_thermlog.txt")
    pow_raw = read(scan_dir / "powermetrics_raw.txt")
    pow_sensors = read(scan_dir / "powermetrics_sensors.txt")
    top = read(scan_dir / "top.txt")
    ps = read(scan_dir / "top_processes.txt")
    df = read(scan_dir / "df_root.txt")
    hw = load_json(scan_dir / "system_profiler_hardware.json")
    mem = load_json(scan_dir / "system_profiler_memory.json")
    power = load_json(scan_dir / "system_profiler_power.json")

    sensor_lines = find_interesting_sensor_lines(pow_sensors + "\n" + pow_raw + "\n" + pmset_therm)
    power_values = parse_power_values(pow_sensors + "\n" + pow_raw)

    hardware = (hw.get("SPHardwareDataType") or [{}])[0]
    power_sections = power.get("SPPowerDataType") or []
    battery_info = {}
    charger_info = {}
    for s in power_sections:
        if s.get("_name") == "spbattery_information":
            battery_info = s
        elif s.get("_name") == "sppower_ac_charger_information":
            charger_info = s

    report = {
        "created_at": datetime.now().isoformat(timespec="seconds"),
        "analyzer_version": VERSION,
        "scan_dir": str(scan_dir),
        "hardware": {
            "model_name": hardware.get("machine_name"),
            "model_identifier": hardware.get("machine_model"),
            "chip": hardware.get("chip_type"),
            "memory": hardware.get("physical_memory"),
        },
        "power_values": power_values,
        "sensor_lines_found": len(sensor_lines),
        "sensor_lines": sensor_lines,
        "battery": {
            "condition": (battery_info.get("sppower_battery_health_info") or {}).get("sppower_battery_health"),
            "cycle_count": (battery_info.get("sppower_battery_health_info") or {}).get("sppower_battery_cycle_count"),
            "state_of_charge": (battery_info.get("sppower_battery_charge_info") or {}).get("sppower_battery_state_of_charge"),
        },
        "charger": {
            "name": charger_info.get("sppower_ac_charger_name"),
            "watts": charger_info.get("sppower_ac_charger_watts"),
            "connected": charger_info.get("sppower_battery_charger_connected"),
        },
        "system": {
            "sw_vers": sw.strip(),
            "uptime": uptime.strip(),
            "pmset_batt": pmset_batt.strip(),
            "disk_root": df.strip(),
        },
        "top_processes": parse_top_processes(ps or top),
        "notes": [
            "Temperature/fan sensors vary by Mac model and macOS version.",
            "powermetrics SMC/thermal samplers may not expose all CPU/GPU/fan values on Apple Silicon.",
            "Raw powermetrics files are kept in this folder for parser improvements.",
        ],
    }

    return report


def write_reports(scan_dir, report):
    scan_dir = Path(scan_dir)
    json_path = scan_dir / "system_health_report.json"
    txt_path = scan_dir / "system_health_report.txt"
    html_path = scan_dir / "system_health_report.html"

    json_path.write_text(json.dumps(report, indent=2, ensure_ascii=False), encoding="utf-8")

    lines = []
    lines.append(f"MacPowerLab System Health Snapshot v{VERSION}")
    lines.append("=" * 45)
    lines.append("")
    lines.append("Hardware:")
    for k, v in report["hardware"].items():
        lines.append(f"- {k}: {v}")
    lines.append("")
    lines.append("Battery / charger:")
    for k, v in report["battery"].items():
        lines.append(f"- battery {k}: {v}")
    for k, v in report["charger"].items():
        lines.append(f"- charger {k}: {v}")
    lines.append("")
    lines.append("Power values from powermetrics:")
    if report["power_values"]:
        for k, v in report["power_values"].items():
            lines.append(f"- {k}: {v}")
    else:
        lines.append("- No standard CPU/GPU/ANE power values parsed from this snapshot.")
    lines.append("")
    lines.append(f"Interesting sensor lines found: {report['sensor_lines_found']}")
    for line in report["sensor_lines"][:80]:
        lines.append(f"- {line}")
    lines.append("")
    lines.append("Top processes:")
    if report["top_processes"]:
        for line in report["top_processes"][:20]:
            lines.append(f"- {line}")
    else:
        lines.append("- No process list parsed.")
    lines.append("")
    lines.append("Notes:")
    for note in report["notes"]:
        lines.append(f"- {note}")

    txt_path.write_text("\n".join(lines) + "\n", encoding="utf-8")

    html = "<!doctype html><meta charset='utf-8'><title>MacPowerLab System Health Snapshot</title>"
    html += "<body style='font-family:-apple-system,BlinkMacSystemFont,Segoe UI,sans-serif;background:#0b1016;color:#e6edf3;padding:28px'>"
    html += f"<h1>MacPowerLab System Health Snapshot v{VERSION}</h1>"
    html += "<pre style='white-space:pre-wrap;background:#111820;border:1px solid #253545;border-radius:12px;padding:16px'>"
    html += txt_path.read_text(encoding="utf-8")
    html += "</pre></body>"
    html_path.write_text(html, encoding="utf-8")
    return json_path, txt_path, html_path


def main():
    if len(sys.argv) < 2:
        print("Usage: analyze_system_sensor_snapshot.py <scan_dir>")
        raise SystemExit(1)
    scan_dir = Path(sys.argv[1])
    report = analyze(scan_dir)
    paths = write_reports(scan_dir, report)
    print("System health snapshot analyzed:")
    for p in paths:
        print(f"  {p}")


if __name__ == "__main__":
    main()
