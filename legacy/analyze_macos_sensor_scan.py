#!/usr/bin/env python3
"""
MacPowerLab macOS sensor scan analyzer v0.5.0
"""

import json
import plistlib
import re
import sys
from datetime import datetime
from pathlib import Path

VERSION = "0.9.0"


def load_text(path):
    try:
        return Path(path).read_text(encoding="utf-8", errors="replace")
    except Exception:
        return ""


def load_json(path):
    try:
        return json.loads(load_text(path))
    except Exception:
        return {}


def load_plist(path):
    try:
        return plistlib.loads(Path(path).read_bytes())
    except Exception:
        return None


def flatten(obj, prefix=""):
    out = {}
    if isinstance(obj, dict):
        for k, v in obj.items():
            key = f"{prefix}.{k}" if prefix else str(k)
            out[key] = type(v).__name__
            out.update(flatten(v, key))
    elif isinstance(obj, list):
        out[prefix + ".[]" if prefix else "[]"] = "list"
        for i, item in enumerate(obj[:4]):
            out.update(flatten(item, f"{prefix}[{i}]"))
    return out


def get_path(obj, path, default=None):
    cur = obj
    for part in path.split("."):
        if isinstance(cur, list):
            try:
                cur = cur[int(part)]
            except Exception:
                return default
        elif isinstance(cur, dict):
            cur = cur.get(part, default)
        else:
            return default
    return cur


def best_port_power_w(battery):
    vals = []
    for p in battery.get("PortControllerInfo", []) or []:
        try:
            vals.append(float(p.get("PortControllerMaxPower", 0)))
        except Exception:
            pass
    if not vals:
        return None
    v = max(vals)
    return v / 1000 if v > 1000 else v


def sp_sections(sp):
    return sp.get("SPPowerDataType", []) if isinstance(sp, dict) else []


def find_sp_section(sp, name):
    for s in sp_sections(sp):
        if s.get("_name") == name:
            return s
    return {}


def parse_powermetrics(text):
    out = {}
    m = re.search(r"Machine model:\s*(.+)", text)
    if m:
        out["machine_model"] = m.group(1).strip()
    m = re.search(r"OS version:\s*(.+)", text)
    if m:
        out["os_version"] = m.group(1).strip()
    for key, label in [
        ("cpu_power_mw", "CPU Power"),
        ("gpu_power_mw", "GPU Power"),
        ("ane_power_mw", "ANE Power"),
        ("combined_power_mw", "Combined Power \\(CPU \\+ GPU \\+ ANE\\)"),
    ]:
        m = re.search(label + r":\s*([0-9.]+)\s*mW", text)
        if m:
            out[key] = float(m.group(1))
    return out


def yes(v):
    return str(v).strip().lower() in ("yes", "true", "1", "on", "enabled")


def no(v):
    return str(v).strip().lower() in ("no", "false", "0", "off", "disabled")


def derive_energy_mode(low_power, high_power):
    # recent macOS raw System Information LowPowerMode can disagree with the user-facing UI.
    # Treat raw values as debug-only unless a future reliable active-mode source is found.
    if yes(high_power):
        return "High Power"
    if yes(low_power):
        return "Unknown / raw LowPowerMode=Yes"
    return "Normal / Automatic"


def analyze(scan_dir):
    scan_dir = Path(scan_dir)
    sw = load_text(scan_dir / "sw_vers.txt")
    pmset = load_text(scan_dir / "pmset_batt.txt")
    powermetrics = parse_powermetrics(load_text(scan_dir / "powermetrics_raw.txt"))
    sp = load_json(scan_dir / "system_profiler_power.json")
    plist = load_plist(scan_dir / "apple_smart_battery.plist")
    battery = plist[0] if isinstance(plist, list) and plist else (plist if isinstance(plist, dict) else {})

    sp_batt = find_sp_section(sp, "spbattery_information")
    sp_power = find_sp_section(sp, "sppower_information")
    sp_charger = find_sp_section(sp, "sppower_ac_charger_information")

    ac_power = sp_power.get("AC Power", {})
    batt_power = sp_power.get("Battery Power", {})

    adapter = battery.get("AdapterDetails", {}) or {}
    raw_adapter = (battery.get("AppleRawAdapterDetails") or [{}])[0] if isinstance(battery.get("AppleRawAdapterDetails"), list) else {}

    report = {
        "created_at": datetime.now().isoformat(timespec="seconds"),
        "analyzer_version": VERSION,
        "scan_dir": str(scan_dir),
        "macos": {},
        "system_profiler": {
            "battery_condition": get_path(sp_batt, "sppower_battery_health_info.sppower_battery_health"),
            "cycle_count": get_path(sp_batt, "sppower_battery_health_info.sppower_battery_cycle_count"),
            "state_of_charge": get_path(sp_batt, "sppower_battery_charge_info.sppower_battery_state_of_charge"),
            "maximum_capacity": get_path(sp_batt, "sppower_battery_health_info.sppower_battery_health_maximum_capacity"),
            "ac_low_power_mode": ac_power.get("LowPowerMode"),
            "battery_low_power_mode": batt_power.get("LowPowerMode"),
            "ac_high_power_mode": ac_power.get("HighPowerMode"),
            "battery_high_power_mode": batt_power.get("HighPowerMode"),
            "ac_energy_mode": derive_energy_mode(ac_power.get("LowPowerMode"), ac_power.get("HighPowerMode")),
            "battery_energy_mode": derive_energy_mode(batt_power.get("LowPowerMode"), batt_power.get("HighPowerMode")),
            "charger_name": sp_charger.get("sppower_ac_charger_name"),
            "charger_watts": sp_charger.get("sppower_ac_charger_watts"),
            "charger_connected": sp_charger.get("sppower_battery_charger_connected"),
        },
        "apple_smart_battery": {
            "key_count": len(flatten(battery)),
            "device_name": battery.get("DeviceName"),
            "cycle_count": battery.get("CycleCount"),
            "adapter_name": adapter.get("Name") or adapter.get("Description") or raw_adapter.get("Name") or raw_adapter.get("Description"),
            "adapter_watts": adapter.get("Watts") or raw_adapter.get("Watts"),
            "adapter_voltage_v": (adapter.get("AdapterVoltage") or raw_adapter.get("AdapterVoltage") or 0) / 1000,
            "adapter_current_a": (adapter.get("Current") or raw_adapter.get("Current") or 0) / 1000,
            "bms_system_power_available": "SystemPower" in (battery.get("BatteryData") or {}),
            "powertelemetry_available": isinstance(battery.get("PowerTelemetryData"), dict),
            "powertelemetry_system_effective_total_load_available": "SystemEffectiveTotalLoad" in (battery.get("PowerTelemetryData") or {}),
            "port_controller_max_power_w": best_port_power_w(battery),
            "battery_cell_disconnect_count": battery.get("BatteryCellDisconnectCount"),
            "low_power_energy_mode_data_available": "LPEMData" in battery,
            "power_distribution_available": "PowerDistribution" in battery,
        },
        "powermetrics": powermetrics,
        "pmset": {"raw": pmset.strip()},
        "warnings": [],
        "recommendations": [],
        "confidence": {},
    }

    # macOS version parsing.
    for line in sw.splitlines():
        if ":" in line:
            k, v = line.split(":", 1)
            report["macos"][k.strip()] = v.strip()

    # v0.7.1: macOS 27 raw LowPowerMode values are treated as debug-only
    # because they can disagree with the user-facing Battery settings UI.

    maxcap = report["system_profiler"]["maximum_capacity"]
    if str(maxcap).strip().upper() == "EM_DASH":
        report["warnings"].append("System Information reports maximum capacity as EM_DASH, so monitor should continue using raw battery capacity for health estimates.")

    if report["apple_smart_battery"]["bms_system_power_available"]:
        report["confidence"]["BMS SystemPower"] = "trusted internal system load source"
    else:
        report["confidence"]["BMS SystemPower"] = "missing; fallback to powermetrics SoC/component estimates"

    if report["apple_smart_battery"]["powertelemetry_system_effective_total_load_available"]:
        report["recommendations"].append("Add PowerTelemetryData.SystemEffectiveTotalLoad as a secondary macOS 27 telemetry field.")
    if report["apple_smart_battery"]["power_distribution_available"]:
        report["recommendations"].append("Add PowerDistribution debug extraction to future report sections.")
    if report["apple_smart_battery"]["low_power_energy_mode_data_available"]:
        report["recommendations"].append("Track LPEMData as a macOS 27 Low Power / energy mode debug section.")

    report["recommendations"].append("Energy Mode raw fields are debug-only on recent macOS builds; trust the Battery settings UI for High Power/Low Power.")

    return report, battery


def write_reports(scan_dir, report, battery):
    scan_dir = Path(scan_dir)
    json_path = scan_dir / "macos_sensor_report.json"
    txt_path = scan_dir / "macos_sensor_report.txt"
    html_path = scan_dir / "macos_sensor_report.html"
    keys_path = scan_dir / "apple_smart_battery_keys.json"

    json_path.write_text(json.dumps(report, indent=2, ensure_ascii=False), encoding="utf-8")
    keys_path.write_text(json.dumps(flatten(battery), indent=2, ensure_ascii=False), encoding="utf-8")

    lines = []
    lines.append(f"MacPowerLab macOS Sensor Report v{VERSION}")
    lines.append("=" * 42)
    lines.append("")
    lines.append(f"macOS: {report['macos'].get('ProductVersion', 'n/a')} build {report['macos'].get('BuildVersion', 'n/a')}")
    lines.append(f"Machine model: {report['powermetrics'].get('machine_model', 'n/a')}")
    lines.append("")
    lines.append("Power settings / raw Energy Mode flags:")
    lines.append(f"- Derived AC Energy Mode: {report['system_profiler'].get('ac_energy_mode')}")
    lines.append(f"- Derived Battery Energy Mode: {report['system_profiler'].get('battery_energy_mode')}")
    lines.append(f"- Raw AC Low Power Mode: {report['system_profiler'].get('ac_low_power_mode')}")
    lines.append(f"- Raw Battery Low Power Mode: {report['system_profiler'].get('battery_low_power_mode')}")
    lines.append(f"- Raw AC High Power Mode: {report['system_profiler'].get('ac_high_power_mode')}")
    lines.append(f"- Raw Battery High Power Mode: {report['system_profiler'].get('battery_high_power_mode')}")
    lines.append("")
    lines.append("Charger:")
    lines.append(f"- System Information charger: {report['system_profiler'].get('charger_name')} / {report['system_profiler'].get('charger_watts')} W")
    lines.append(f"- AppleSmartBattery charger: {report['apple_smart_battery'].get('adapter_name')} / {report['apple_smart_battery'].get('adapter_watts')} W")
    lines.append(f"- Adapter voltage/current: {report['apple_smart_battery'].get('adapter_voltage_v')} V / {report['apple_smart_battery'].get('adapter_current_a')} A")
    lines.append(f"- Port max power: {report['apple_smart_battery'].get('port_controller_max_power_w')} W")
    lines.append("")
    lines.append("Battery:")
    lines.append(f"- Condition: {report['system_profiler'].get('battery_condition')}")
    lines.append(f"- Cycle count: {report['system_profiler'].get('cycle_count')}")
    lines.append(f"- System Information maximum capacity: {report['system_profiler'].get('maximum_capacity')}")
    lines.append(f"- AppleSmartBattery key count: {report['apple_smart_battery'].get('key_count')}")
    lines.append(f"- BMS SystemPower available: {report['apple_smart_battery'].get('bms_system_power_available')}")
    lines.append(f"- PowerTelemetry available: {report['apple_smart_battery'].get('powertelemetry_available')}")
    lines.append("")
    lines.append("Warnings:")
    if report["warnings"]:
        lines.extend(f"- {w}" for w in report["warnings"])
    else:
        lines.append("- None")
    lines.append("")
    lines.append("Recommendations:")
    lines.extend(f"- {r}" for r in report["recommendations"])
    txt_path.write_text("\n".join(lines) + "\n", encoding="utf-8")

    html = "<!doctype html><meta charset='utf-8'><title>MacPowerLab macOS Sensor Report</title>"
    html += "<body style='font-family:-apple-system,BlinkMacSystemFont,Segoe UI,sans-serif;background:#0b1016;color:#e6edf3;padding:28px'>"
    html += f"<h1>MacPowerLab macOS Sensor Report v{VERSION}</h1>"
    html += "<pre style='white-space:pre-wrap;background:#111820;border:1px solid #253545;border-radius:12px;padding:16px'>"
    html += txt_path.read_text(encoding="utf-8")
    html += "</pre></body>"
    html_path.write_text(html, encoding="utf-8")

    return json_path, txt_path, html_path, keys_path


def main():
    if len(sys.argv) < 2:
        print("Usage: analyze_macos_sensor_scan.py <scan_dir>")
        raise SystemExit(1)
    scan_dir = Path(sys.argv[1])
    report, battery = analyze(scan_dir)
    paths = write_reports(scan_dir, report, battery)
    print("macOS sensor scan analyzed:")
    for p in paths:
        print(f"  {p}")


if __name__ == "__main__":
    main()
