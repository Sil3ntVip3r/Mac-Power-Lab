#!/usr/bin/env python3
"""
mac_power_watch.py

MacBook Power Monitor
Version: 0.12.0

MacPowerLab monitor for macOS. app power attribution pack v0.9.0 compatible.

Major v0.9.0 upgrades:
- Default timestamped run logging
- CSV + events JSONL + debug JSON per run
- Automatic event detection
- Energy integration: Wh charged/discharged/net
- Charger/adapter wattage-change event detection
- AC connected but battery discharging warning
- Rolling averages, peaks, trends
- Thermal plateau and possible throttling detection
- Baseline capture and load increase estimates
- Optional powermetrics CPU/GPU/ANE/DRAM parsing
- Optional slow system_profiler SPPowerDataType cross-check
- Recursive debug dump of AppleSmartBattery telemetry
- Phase marker support
- Per-application Energy Impact and estimated watts/Wh attribution
"""

import argparse
import os
import csv
import curses
import json
import os
import plistlib
import re
import subprocess
import threading
import time
from collections import deque
from datetime import datetime

from app_power_attribution import (
    DEFAULT_TOP_SLOTS as APP_POWER_TOP_SLOTS,
    AppPowerAttributionEngine,
    AppPowerSessionLogger,
    AppPowerWorker,
    empty_app_power_row_fields,
)


APP_NAME = "MacBook Power Monitor"
VERSION = "0.12.0"


# -------------------------
# Command helpers
# -------------------------

def run_cmd(cmd, text=False, timeout=None):
    return subprocess.check_output(cmd, stderr=subprocess.STDOUT, text=text, timeout=timeout)


def safe_json(value):
    try:
        json.dumps(value)
        return value
    except Exception:
        return str(value)


def recursive_sanitize(obj, max_depth=12):
    if max_depth <= 0:
        return str(type(obj))
    if isinstance(obj, dict):
        return {str(k): recursive_sanitize(v, max_depth - 1) for k, v in obj.items()}
    if isinstance(obj, (list, tuple)):
        return [recursive_sanitize(v, max_depth - 1) for v in obj]
    if isinstance(obj, (str, int, float, bool)) or obj is None:
        return obj
    return str(obj)


# -------------------------
# macOS power telemetry
# -------------------------

def read_battery():
    raw = run_cmd(["ioreg", "-r", "-c", "AppleSmartBattery", "-a"], text=False, timeout=3)
    data = plistlib.loads(raw)
    if not data:
        raise RuntimeError("No AppleSmartBattery data returned")
    return data[0]


def read_battery_banks():
    """
    macOS 27 Beta 2 exposes useful cell-bank fields under AppleSmartBatteryBank
    that are not present on the root AppleSmartBattery object.
    """
    try:
        raw = run_cmd(["ioreg", "-r", "-c", "AppleSmartBatteryBank", "-a"], text=False, timeout=3)
        data = plistlib.loads(raw)
        return data if isinstance(data, list) else []
    except Exception:
        return []


def read_pmset_status():
    try:
        return run_cmd(["pmset", "-g", "batt"], text=True, timeout=2).strip()
    except Exception:
        return ""



def is_yes(v):
    return str(v).strip().lower() in ("yes", "true", "1", "on", "enabled")


def is_no(v):
    return str(v).strip().lower() in ("no", "false", "0", "off", "disabled")


def derive_energy_mode(low_power, high_power):
    """
    macOS 27 can expose raw LowPowerMode=Yes even when the user-facing
    Battery settings screen is set to High Power. Therefore:

    - HighPowerMode=Yes wins.
    - LowPowerMode=Yes only becomes an active Low Power warning if HighPowerMode
      is explicitly No/False/Off.
    - LowPowerMode=Yes with missing/unknown HighPowerMode is treated as
      ambiguous/debug-only, not a main-screen warning.
    """
    if is_yes(high_power):
        return "High Power"
    if is_yes(low_power) and is_no(high_power):
        return "Low Power"
    if is_yes(low_power):
        return "Unknown / raw LowPowerMode=Yes"
    return "Normal / Automatic"


def power_mode_warning_for(ac_low, batt_low, ac_high, batt_high):
    """
    v0.6.3: Do not show a main-screen Low Power warning from raw
    system_profiler values on macOS 27. The user-facing Battery settings UI can
    show High Power while raw System Information still reports LowPowerMode=Yes.

    Keep raw fields in --full/debug; do not warn unless we later find a reliable
    public source for the active user-facing Energy Mode.
    """
    return None


def read_system_profiler_power():
    """
    Slow cross-check. Use sparingly.

    v0.9.0 uses JSON-first parsing because macOS 27 exposes useful power
    fields in system_profiler SPPowerDataType -json, including LowPowerMode
    and charger identity.
    """
    info = {
        "sp_status": "off",
        "sp_raw_length": None,
        "sp_ac_charger_watts": None,
        "sp_cycle_count": None,
        "sp_condition": None,
        "sp_full_charge_capacity": None,
        "sp_state_of_charge": None,
        "sp_ac_low_power_mode": None,
        "sp_battery_low_power_mode": None,
        "sp_ac_high_power_mode": None,
        "sp_battery_high_power_mode": None,
        "sp_charger_name": None,
        "sp_charger_connected": None,
        "sp_power_mode_warning": None,
    }

    try:
        raw_json = run_cmd(["system_profiler", "SPPowerDataType", "-json"], text=True, timeout=30)
        info["sp_raw_length"] = len(raw_json)
        data = json.loads(raw_json)
        sections = data.get("SPPowerDataType", [])

        for section in sections:
            name = section.get("_name")

            if name == "spbattery_information":
                charge = section.get("sppower_battery_charge_info", {})
                health = section.get("sppower_battery_health_info", {})
                info["sp_cycle_count"] = health.get("sppower_battery_cycle_count")
                info["sp_condition"] = health.get("sppower_battery_health")
                maxcap = health.get("sppower_battery_health_maximum_capacity")
                if isinstance(maxcap, (int, float)):
                    info["sp_full_charge_capacity"] = maxcap
                info["sp_state_of_charge"] = charge.get("sppower_battery_state_of_charge")

            elif name == "sppower_information":
                ac = section.get("AC Power", {})
                batt = section.get("Battery Power", {})
                info["sp_ac_low_power_mode"] = ac.get("LowPowerMode")
                info["sp_battery_low_power_mode"] = batt.get("LowPowerMode")
                info["sp_ac_high_power_mode"] = ac.get("HighPowerMode")
                info["sp_battery_high_power_mode"] = batt.get("HighPowerMode")

            elif name == "sppower_ac_charger_information":
                watts = section.get("sppower_ac_charger_watts")
                try:
                    info["sp_ac_charger_watts"] = float(watts)
                except Exception:
                    info["sp_ac_charger_watts"] = watts
                info["sp_charger_name"] = section.get("sppower_ac_charger_name")
                info["sp_charger_connected"] = section.get("sppower_battery_charger_connected")

        info["sp_ac_energy_mode"] = derive_energy_mode(info.get("sp_ac_low_power_mode"), info.get("sp_ac_high_power_mode"))
        info["sp_battery_energy_mode"] = derive_energy_mode(info.get("sp_battery_low_power_mode"), info.get("sp_battery_high_power_mode"))
        info["sp_power_mode_warning"] = power_mode_warning_for(
            info.get("sp_ac_low_power_mode"),
            info.get("sp_battery_low_power_mode"),
            info.get("sp_ac_high_power_mode"),
            info.get("sp_battery_high_power_mode"),
        )

        info["sp_status"] = "ok-json"
        return info

    except Exception:
        # Fallback to old text parser.
        pass

    try:
        raw = run_cmd(["system_profiler", "SPPowerDataType"], text=True, timeout=20)
    except Exception as e:
        info["sp_status"] = f"error: {e}"
        return info

    info["sp_status"] = "ok-text"
    info["sp_raw_length"] = len(raw)

    patterns = [
        ("sp_ac_charger_watts", r"(?im)^\s*Wattage \(W\):\s*([0-9.]+)"),
        ("sp_cycle_count", r"(?im)^\s*Cycle Count:\s*([0-9.]+)"),
        ("sp_condition", r"(?im)^\s*Condition:\s*(.+)$"),
        ("sp_full_charge_capacity", r"(?im)^\s*Full Charge Capacity.*?:\s*([0-9.]+)"),
        ("sp_state_of_charge", r"(?im)^\s*State of Charge.*?:\s*([0-9.]+)%"),
        ("sp_ac_low_power_mode", r"(?im)AC Power:.*?Low Power Mode:\s*(Yes|No)"),
        ("sp_battery_low_power_mode", r"(?im)Battery Power:.*?Low Power Mode:\s*(Yes|No)"),
    ]

    for key, pattern in patterns:
        m = re.search(pattern, raw, flags=re.S)
        if m:
            val = m.group(1).strip()
            try:
                info[key] = float(val)
            except Exception:
                info[key] = val

    info["sp_ac_energy_mode"] = derive_energy_mode(info.get("sp_ac_low_power_mode"), info.get("sp_ac_high_power_mode"))
    info["sp_battery_energy_mode"] = derive_energy_mode(info.get("sp_battery_low_power_mode"), info.get("sp_battery_high_power_mode"))
    info["sp_power_mode_warning"] = power_mode_warning_for(
        info.get("sp_ac_low_power_mode"),
        info.get("sp_battery_low_power_mode"),
        info.get("sp_ac_high_power_mode"),
        info.get("sp_battery_high_power_mode"),
    )

    return info

def parse_pmset(pmset_text):
    info = {"raw": pmset_text, "source": None, "percent": None, "state": None, "time_remaining": None}
    if not pmset_text:
        return info

    lines = pmset_text.splitlines()
    first_line = lines[0] if lines else ""

    if "AC Power" in first_line:
        info["source"] = "AC Power"
    elif "Battery Power" in first_line:
        info["source"] = "Battery Power"

    percent_match = re.search(r"(\d+)%", pmset_text)
    if percent_match:
        info["percent"] = float(percent_match.group(1))

    lower = pmset_text.lower()
    if "discharging" in lower:
        info["state"] = "discharging"
    elif "charging" in lower:
        info["state"] = "charging"
    elif "charged" in lower:
        info["state"] = "charged"

    time_match = re.search(r"(\d+:\d+)\s+remaining", pmset_text)
    if time_match:
        info["time_remaining"] = time_match.group(1)
    elif "no estimate" in lower:
        info["time_remaining"] = "no estimate"

    return info


def get_num(d, key, default=None):
    value = d.get(key, default)
    try:
        return float(value)
    except (TypeError, ValueError):
        return default


def get_bool(d, key, default=None):
    value = d.get(key, default)
    if isinstance(value, bool):
        return value
    return default


def calc_percent(b, pmset_info):
    if pmset_info.get("percent") is not None:
        return pmset_info["percent"]

    current = get_num(b, "CurrentCapacity")
    maxcap = get_num(b, "MaxCapacity")

    if current is None:
        return None

    if 0 <= current <= 100 and (maxcap is None or maxcap <= 100):
        return current

    if maxcap and maxcap > 0:
        return current / maxcap * 100

    return current


def battery_electrical(b):
    voltage_mv = get_num(b, "Voltage")
    amperage_ma = get_num(b, "InstantAmperage")

    if amperage_ma is None:
        amperage_ma = get_num(b, "Amperage")

    if voltage_mv is None or amperage_ma is None:
        return {
            "battery_voltage_mv": voltage_mv,
            "battery_amperage_ma": amperage_ma,
            "battery_voltage_v": None,
            "battery_amperage_a": None,
            "net_battery_watts": None,
        }

    voltage_v = voltage_mv / 1000.0
    amperage_a = amperage_ma / 1000.0

    return {
        "battery_voltage_mv": voltage_mv,
        "battery_amperage_ma": amperage_ma,
        "battery_voltage_v": voltage_v,
        "battery_amperage_a": amperage_a,
        "net_battery_watts": voltage_v * amperage_a,
    }


def normalize_adapter_voltage(value):
    if value is None:
        return None
    try:
        value = float(value)
    except Exception:
        return None
    if value > 1000:
        return value / 1000.0
    return value


def normalize_adapter_current(value):
    if value is None:
        return None
    try:
        value = float(value)
    except Exception:
        return None
    if abs(value) > 100:
        return value / 1000.0
    return value


def adapter_info(b):
    adapter = b.get("AdapterDetails") or {}
    raw_adapter = b.get("AppleRawAdapterDetails")
    charger_data = b.get("ChargerData")
    port_info = b.get("PortControllerInfo")
    power_telemetry = b.get("PowerTelemetryData")
    fed_details = b.get("FedDetails")

    best_adapter_index = b.get("BestAdapterIndex")
    selected_raw_adapter = None

    if isinstance(raw_adapter, list) and raw_adapter:
        try:
            idx = int(best_adapter_index or 0)
            selected_raw_adapter = raw_adapter[idx] if 0 <= idx < len(raw_adapter) else raw_adapter[0]
        except Exception:
            selected_raw_adapter = raw_adapter[0]
    elif isinstance(raw_adapter, dict):
        selected_raw_adapter = raw_adapter

    selected_raw_adapter = selected_raw_adapter or {}

    # Some Apple Silicon Macs expose AdapterVoltage, not Voltage.
    raw_voltage = (
        adapter.get("Voltage")
        or adapter.get("AdapterVoltage")
        or selected_raw_adapter.get("Voltage")
        or selected_raw_adapter.get("AdapterVoltage")
    )
    raw_current = adapter.get("Current") if adapter.get("Current") is not None else selected_raw_adapter.get("Current")

    voltage_v = normalize_adapter_voltage(raw_voltage)
    current_a = normalize_adapter_current(raw_current)

    calculated_watts = None
    if voltage_v is not None and current_a is not None:
        calculated_watts = voltage_v * current_a

    charger_configuration = b.get("ChargerConfiguration")
    raw_external_connected = b.get("AppleRawExternalConnected")

    adapter_name = (
        adapter.get("Name")
        or adapter.get("Description")
        or adapter.get("Manufacturer")
        or selected_raw_adapter.get("Name")
        or selected_raw_adapter.get("Description")
        or selected_raw_adapter.get("Manufacturer")
        or "Unknown"
    )

    reported_watts = adapter.get("Watts")
    if reported_watts is None:
        reported_watts = selected_raw_adapter.get("Watts")

    return {
        "name": adapter_name,
        "reported_watts": reported_watts,
        "raw_voltage": raw_voltage,
        "raw_current": raw_current,
        "voltage_v": voltage_v,
        "current_a": current_a,
        "calculated_watts": calculated_watts,
        "best_adapter_index": best_adapter_index,
        "charger_configuration": charger_configuration,
        "apple_raw_external_connected": raw_external_connected,
        "raw_adapter_details": recursive_sanitize(raw_adapter),
        "charger_data": recursive_sanitize(charger_data),
        "port_controller_info": recursive_sanitize(port_info),
        "power_telemetry_data": recursive_sanitize(power_telemetry),
        "fed_details": recursive_sanitize(fed_details),
    }


def temperature_from_battery(b, banks=None):
    banks = banks or []

    def plausible_temp(raw_value):
        if raw_value is None:
            return None
        try:
            raw_value = float(raw_value)
        except Exception:
            return None

        candidates = []
        # Common Apple battery encodings seen across betas:
        # - deci-Kelvin: 3061 -> 33.0 C
        # - centi-Celsius: 3589 -> 35.89 C
        # - plain Celsius: 34 -> 34 C
        if raw_value > 2000:
            candidates.append((raw_value / 10.0) - 273.15)
            candidates.append(raw_value / 100.0)
        elif raw_value > 200:
            candidates.append(raw_value - 273.15)
            candidates.append(raw_value / 10.0)
        else:
            candidates.append(raw_value)

        valid = [c for c in candidates if -20.0 <= c <= 85.0]
        if not valid:
            return None
        return min(valid, key=lambda c: abs(c - 35.0))

    def first_raw(keys):
        sources = [b, b.get("BatteryData") if isinstance(b.get("BatteryData"), dict) else None]
        for bank in banks:
            if isinstance(bank, dict):
                sources.append(bank)
                bd = bank.get("BatteryData")
                if isinstance(bd, dict):
                    sources.append(bd)
        for source in sources:
            if not isinstance(source, dict):
                continue
            for key in keys:
                value = get_num(source, key)
                if value is not None:
                    return value
        return None

    raw = first_raw(["Temperature", "BatteryTemperature"])
    virtual_raw = first_raw(["VirtualTemperature"])

    c = plausible_temp(raw)
    vc = plausible_temp(virtual_raw)
    f = ((c * 9 / 5) + 32) if c is not None else None
    vf = ((vc * 9 / 5) + 32) if vc is not None else None

    return raw, c, f, virtual_raw, vc, vf


def time_field_minutes(value):
    try:
        value = int(value)
    except Exception:
        return None
    if value < 0 or value >= 65000:
        return None
    return value


def format_minutes(minutes):
    if minutes is None:
        return "n/a"
    minutes = int(max(0, minutes))
    h = minutes // 60
    m = minutes % 60
    if h:
        return f"{h}h {m}m"
    return f"{m}m"


def format_duration(seconds):
    seconds = int(max(0, seconds))
    h = seconds // 3600
    m = (seconds % 3600) // 60
    s = seconds % 60
    if h:
        return f"{h:02d}:{m:02d}:{s:02d}"
    return f"{m:02d}:{s:02d}"


def estimate_battery_health(b):
    raw_max = get_num(b, "AppleRawMaxCapacity")
    nominal = get_num(b, "NominalChargeCapacity")
    design = get_num(b, "DesignCapacity")
    numerator = raw_max or nominal

    if numerator and design and design > 1000:
        return (numerator / design) * 100

    max_capacity = get_num(b, "MaxCapacity")
    if max_capacity and design and design <= 100:
        return (max_capacity / design) * 100

    return None


def estimated_wh(raw_capacity_mah, voltage_v):
    if raw_capacity_mah is None or voltage_v is None:
        return None
    try:
        return (float(raw_capacity_mah) / 1000.0) * float(voltage_v)
    except Exception:
        return None


def telemetry_power_to_watts(value):
    """
    AppleSmartBattery PowerTelemetryData values are not formally documented.

    v0.3.1 used /100, but the user's debug data showed:
      BatteryData.SystemPower = 24.175 W
      PowerTelemetryData.SystemLoad = 24175

    That lines up as 24175 / 1000 = 24.175 W.

    Treat this as a secondary estimate/debug value. BatteryData.SystemPower
    is preferred when available.
    """
    if value is None:
        return None
    try:
        value = float(value)
    except Exception:
        return None

    if abs(value) > 1000:
        return value / 1000.0
    return value


def battery_data_stats(b, banks=None):
    banks = banks or []
    root_bd = b.get("BatteryData") or {}
    if not isinstance(root_bd, dict):
        root_bd = {}

    data_sources = [root_bd]
    lifetime_sources = []

    if isinstance(b.get("LifetimeData"), dict):
        lifetime_sources.append(b.get("LifetimeData"))

    for bank in banks:
        if not isinstance(bank, dict):
            continue
        if isinstance(bank.get("BatteryData"), dict):
            data_sources.append(bank.get("BatteryData"))
        if isinstance(bank.get("LifetimeData"), dict):
            lifetime_sources.append(bank.get("LifetimeData"))

    if isinstance(root_bd.get("LifetimeData"), dict):
        lifetime_sources.append(root_bd.get("LifetimeData"))

    def nums_from_sources(key):
        nums = []
        for source in data_sources:
            value = source.get(key)
            if isinstance(value, list):
                for item in value:
                    try:
                        nums.append(float(item))
                    except Exception:
                        pass
            else:
                try:
                    if value is not None:
                        nums.append(float(value))
                except Exception:
                    pass
        return nums

    def first_num(key):
        for source in data_sources:
            value = get_num(source, key)
            if value is not None:
                return value
        return None

    cell_v = nums_from_sources("CellVoltage")
    qmax = nums_from_sources("Qmax")
    weighted_ra = nums_from_sources("WeightedRa")

    lifetime = lifetime_sources[0] if lifetime_sources else {}
    if not isinstance(lifetime, dict):
        lifetime = {}

    stats = {
        "bms_system_power_w": first_num("SystemPower"),
        "bms_adapter_power_w": first_num("AdapterPower"),
        "bms_state_of_charge": first_num("StateOfCharge"),
        "bms_filtered_current": first_num("FilteredCurrent"),
        "bms_iss_current": first_num("ISS"),
        "bms_gauge_flags_raw": first_num("GaugeFlagRaw"),
        "bms_daily_min_soc": first_num("DailyMinSoc"),
        "bms_daily_max_soc": first_num("DailyMaxSoc"),
        "bms_data_flash_write_count": first_num("DataFlashWriteCount"),
        "cell_voltage_min_mv": min(cell_v) if cell_v else None,
        "cell_voltage_max_mv": max(cell_v) if cell_v else None,
        "cell_voltage_delta_mv": (max(cell_v) - min(cell_v)) if cell_v else None,
        "cell_voltage_values": cell_v,
        "qmax_min": min(qmax) if qmax else None,
        "qmax_max": max(qmax) if qmax else None,
        "qmax_delta": (max(qmax) - min(qmax)) if qmax else None,
        "qmax_values": qmax,
        "weighted_ra_min": min(weighted_ra) if weighted_ra else None,
        "weighted_ra_max": max(weighted_ra) if weighted_ra else None,
        "weighted_ra_delta": (max(weighted_ra) - min(weighted_ra)) if weighted_ra else None,
        "weighted_ra_values": weighted_ra,
        "lifetime_avg_temp": get_num(lifetime, "AverageTemperature"),
        "lifetime_min_temp": get_num(lifetime, "MinimumTemperature"),
        "lifetime_max_temp": get_num(lifetime, "MaximumTemperature"),
        "lifetime_max_charge_current_ma": get_num(lifetime, "MaximumChargeCurrent"),
        "lifetime_max_discharge_current_ma": get_num(lifetime, "MaximumDischargeCurrent"),
        "lifetime_max_pack_voltage_mv": get_num(lifetime, "MaximumPackVoltage"),
        "lifetime_min_pack_voltage_mv": get_num(lifetime, "MinimumPackVoltage"),
        "lifetime_cycle_count_last_qmax": get_num(lifetime, "CycleCountLastQmax"),
        "lifetime_total_operating_time": get_num(lifetime, "TotalOperatingTime"),
        "lifetime_temperature_samples": get_num(lifetime, "TemperatureSamples"),
    }

    return stats


def power_telemetry_stats(b):
    pt = b.get("PowerTelemetryData") or {}
    if not isinstance(pt, dict):
        pt = {}

    stats = {
        "telemetry_battery_power_raw": pt.get("BatteryPower"),
        "telemetry_battery_power_w": telemetry_power_to_watts(pt.get("BatteryPower")),
        "telemetry_system_load_raw": pt.get("SystemLoad"),
        "telemetry_system_load_w": telemetry_power_to_watts(pt.get("SystemLoad")),
        "telemetry_system_power_in_raw": pt.get("SystemPowerIn"),
        "telemetry_system_power_in_w": telemetry_power_to_watts(pt.get("SystemPowerIn")),
        "telemetry_system_effective_total_load_raw": pt.get("SystemEffectiveTotalLoad"),
        "telemetry_system_effective_total_load_w": telemetry_power_to_watts(pt.get("SystemEffectiveTotalLoad")),
        "telemetry_accum_system_effective_total_load_raw": pt.get("AccumSystemEffectiveTotalLoad"),
        "telemetry_accum_system_effective_total_load_count": pt.get("AccumSystemEffectiveTotalLoadCount"),
        "telemetry_wall_energy_estimate_raw": pt.get("WallEnergyEstimate"),
        "telemetry_adapter_efficiency_loss_raw": pt.get("AdapterEfficiencyLoss"),
        "telemetry_accumulated_system_energy_raw": pt.get("AccumulatedSystemEnergyConsumed"),
        "telemetry_accumulated_wall_energy_raw": pt.get("AccumulatedWallEnergyEstimate"),
        "telemetry_error_count": pt.get("PowerTelemetryErrorCount"),
    }

    return stats



def power_distribution_stats(b):
    pd = b.get("PowerDistribution") or {}
    if not isinstance(pd, dict):
        pd = {}

    raw_power = pd.get("IPDInputPower")
    raw_voltage = pd.get("IPDInputVoltage")
    raw_current = pd.get("IPDInputCurrent")

    def volts(v):
        try:
            v = float(v)
        except Exception:
            return None
        return v / 1000.0 if abs(v) > 100 else v

    def amps(v):
        try:
            v = float(v)
        except Exception:
            return None
        return v / 1000.0 if abs(v) > 100 else v

    return {
        "pd_ipd_input_power_raw": raw_power,
        "pd_ipd_input_power_w": telemetry_power_to_watts(raw_power),
        "pd_ipd_input_voltage_raw": raw_voltage,
        "pd_ipd_input_voltage_v": volts(raw_voltage),
        "pd_ipd_input_current_raw": raw_current,
        "pd_ipd_input_current_a": amps(raw_current),
    }


def primary_total_load(row):
    """
    v0.8.6 power hierarchy.

    On Battery Power, battery discharge watts are the best real total-load signal.
    On AC Power, do not treat IPDInputPower as system load; it is adapter/input-side
    power and can double-count if added to battery charge acceptance.
    """
    net = row.get("net_battery_watts")
    mode = row.get("mode")
    power_source = row.get("power_source")

    if mode == "DISCHARGING" and net is not None and net < -2:
        return abs(net), "battery discharge watts"

    bms = row.get("bms_system_power_w")
    if bms is not None and bms > 0:
        return bms, "BMS SystemPower"

    eff = row.get("telemetry_system_effective_total_load_w")
    if eff is not None and 0 < eff < 300:
        return eff, "PowerTelemetry SystemEffectiveTotalLoad"

    # AC split estimate: adapter draw minus battery charge. This is a low-confidence
    # estimate if battery charge exceeds reported adapter draw due sensor mismatch.
    adapter_draw = row.get("adapter_calculated_watts") or row.get("pd_ipd_input_power_w") or row.get("adapter_reported_watts")
    if power_source == "AC Power" and adapter_draw is not None:
        if net is not None and net < -2:
            return adapter_draw + abs(net), "adapter draw plus battery assist"
        if net is not None and net > 0:
            system_est = adapter_draw - net
            if 0 < system_est < 300:
                return system_est, "adapter draw minus battery charge estimate"
        # Fall through to component estimate rather than mislabeling adapter input as system load.

    soc = row.get("soc_power_w")
    if soc is not None and 0 < soc < 300:
        return soc, "powermetrics SoC/component estimate"

    if power_source == "AC Power" and adapter_draw is not None and adapter_draw > 0:
        return None, "AC adapter draw available; system split unreliable"

    return None, "no reliable total-load source"


def power_mode(pmset_info, ioreg_external_connected, ioreg_is_charging, net_watts):
    source = pmset_info.get("source")
    state = pmset_info.get("state")

    if source == "Battery Power":
        return "DISCHARGING"
    if source == "AC Power" and state == "charging":
        return "CHARGING"
    if source == "AC Power" and state == "charged":
        return "PLUGGED IN / FULL"
    if source == "AC Power":
        return "PLUGGED IN"

    if ioreg_external_connected and ioreg_is_charging:
        return "CHARGING"
    if ioreg_external_connected:
        return "PLUGGED IN"
    if net_watts is not None and net_watts < 0:
        return "DISCHARGING"

    return "UNKNOWN"




def stress_process_activity():
    """
    Process-based workload proof.

    macOS 27 powermetrics can report incomplete component watts. This scanner
    looks for our known stress processes and sums their CPU/RAM usage so the
    dashboard can prove the workload is active even when CPU/GPU watts read 0.
    """
    try:
        out = run_cmd(["ps", "-axo", "pid,ppid,pcpu,pmem,rss,comm"], text=True, timeout=3)
    except Exception:
        return {
            "stress_process_count": None,
            "stress_cpu_percent": None,
            "stress_mem_percent": None,
            "stress_rss_mb": None,
            "stress_process_names": None,
            "stress_workload_active": None,
        }

    names_of_interest = ("cpu_stress", "gpu_stress", "memory_stress")
    rows = []
    for line in out.splitlines()[1:]:
        parts = line.strip().split(None, 5)
        if len(parts) < 6:
            continue
        try:
            pid = int(parts[0])
            ppid = int(parts[1])
            pcpu = float(parts[2])
            pmem = float(parts[3])
            rss_kb = float(parts[4])
            comm = parts[5]
        except Exception:
            continue
        base = os.path.basename(comm)
        if any(name in base for name in names_of_interest):
            rows.append((pid, ppid, pcpu, pmem, rss_kb, base))

    if not rows:
        return {
            "stress_process_count": 0,
            "stress_cpu_percent": 0.0,
            "stress_mem_percent": 0.0,
            "stress_rss_mb": 0.0,
            "stress_process_names": "",
            "stress_workload_active": False,
        }

    cpu = sum(r[2] for r in rows)
    mem = sum(r[3] for r in rows)
    rss_mb = sum(r[4] for r in rows) / 1024.0
    names = []
    for r in rows:
        if r[5] not in names:
            names.append(r[5])

    return {
        "stress_process_count": len(rows),
        "stress_cpu_percent": cpu,
        "stress_mem_percent": mem,
        "stress_rss_mb": rss_mb,
        "stress_process_names": ",".join(names),
        "stress_workload_active": cpu >= 50 or rss_mb >= 256 or len(rows) > 0,
    }



def powermetrics_component_note(row):
    primary = row.get("primary_total_load_w")
    source = row.get("primary_total_load_source")
    bms = row.get("bms_system_power_w")
    soc = row.get("soc_power_w")
    cpu = row.get("cpu_power_w")
    stress_cpu = row.get("stress_cpu_percent")
    p_active = row.get("p_cluster_active_percent")
    e_active = row.get("e_cluster_active_percent")

    if source == "battery discharge watts":
        return "battery discharge is primary total-load source on Battery Power"
    if stress_cpu is not None and stress_cpu >= 200 and primary is not None and primary >= 30:
        if soc is None or soc < max(3.0, primary * 0.35) or (cpu is not None and cpu <= 0.05):
            return f"component watts incomplete; process CPU {stress_cpu:.0f}% active; trust primary load"
    if (p_active is not None and p_active > 50) or (e_active is not None and e_active > 80):
        if cpu is not None and cpu <= 0.05:
            return "CPU watts missing, but cluster residency proves CPU activity"
    if bms is None and primary is not None:
        return f"BMS missing; using {source}"
    if bms is None:
        return "BMS total unavailable; using best available estimate"
    return "ok"


def whole_mac_power_estimate(mode, net_watts):
    if net_watts is None:
        return None, "n/a"

    if mode == "DISCHARGING":
        return abs(net_watts), "battery discharge estimate"

    if net_watts > 2:
        return None, f"AC total not exposed; battery accepting {net_watts:.2f} W"

    if -2 <= net_watts <= 2:
        return None, "AC total not exposed; battery flow near zero"

    return None, "AC total not exposed"


def charger_live_verdict(row):
    """
    Live charger verdict based on adapter rating, BMS SystemPower, and battery flow.

    On AC:
    - BMS SystemPower is treated as preferred system load.
    - Positive net battery watts means battery is accepting charge.
    - Visible charger load ≈ BMS SystemPower + positive battery charge watts.
    """
    adapter_w = row.get("adapter_reported_watts")
    net_w = row.get("net_battery_watts")
    load_percent = row.get("charger_load_percent")
    source = row.get("power_source")

    if source != "AC Power":
        return "Not on AC"

    if adapter_w is None:
        return "AC connected, adapter watts unknown"

    if net_w is not None and net_w < -5:
        return "Not keeping up — battery draining on AC"

    if load_percent is None:
        if net_w is not None and net_w > 2:
            return "Keeping up — battery charging"
        return "AC connected — load unknown"

    if load_percent >= 98:
        if net_w is not None and net_w > 2:
            return "Keeping up, at/near limit"
        return "At/near limit"
    if load_percent >= 90:
        return "Keeping up, near limit"
    if load_percent >= 75:
        return "Keeping up, moderate headroom"
    if net_w is not None and net_w > 2:
        return "Keeping up — battery charging"

    return "Keeping up / light load"


def read_phase(phase_file):
    try:
        path = os.path.expanduser(phase_file)
        if os.path.exists(path):
            text = open(path, "r", encoding="utf-8").read().strip()
            return text or "idle / unmarked"
    except Exception:
        pass
    return "idle / unmarked"


# -------------------------
# powermetrics
# -------------------------

def parse_power_value(value, unit):
    value = float(value)
    unit = unit.lower()
    if unit == "mw":
        return value / 1000.0
    return value



def derive_cpu_cluster_fields(row):
    """
    Derive useful CPU frequency display fields from Apple Silicon E/P cluster data.

    Recent powermetrics output often does not populate one simple cpu_frequency_mhz
    value. It exposes E-Cluster, P0-Cluster, and P1-Cluster active frequency and
    residency instead.
    """
    clusters = [
        ("E", row.get("e_cluster_freq_mhz"), row.get("e_cluster_active_percent")),
        ("P0", row.get("p0_cluster_freq_mhz"), row.get("p0_cluster_active_percent")),
        ("P1", row.get("p1_cluster_freq_mhz"), row.get("p1_cluster_active_percent")),
    ]

    parts = []
    weighted_num = 0.0
    weighted_den = 0.0
    freq_only = []

    for label, freq, active in clusters:
        if freq is None:
            continue
        try:
            freq = float(freq)
        except Exception:
            continue

        freq_only.append(freq)

        if active is not None:
            try:
                active = float(active)
            except Exception:
                active = None

        if active is not None:
            parts.append(f"{label} {freq:.0f}MHz/{active:.0f}%")
            if active > 0:
                weighted_num += freq * active
                weighted_den += active
        else:
            parts.append(f"{label} {freq:.0f}MHz")

    direct = row.get("cpu_frequency_mhz")
    if direct is not None:
        try:
            direct = float(direct)
        except Exception:
            direct = None

    if weighted_den > 0:
        row["cpu_frequency_estimate_mhz"] = weighted_num / weighted_den
        row["cpu_frequency_estimate_note"] = "weighted E/P cluster frequency"
    elif direct is not None:
        row["cpu_frequency_estimate_mhz"] = direct
        row["cpu_frequency_estimate_note"] = "direct powermetrics CPU frequency"
    elif freq_only:
        row["cpu_frequency_estimate_mhz"] = max(freq_only)
        row["cpu_frequency_estimate_note"] = "max active cluster frequency"
    else:
        row["cpu_frequency_estimate_mhz"] = None
        row["cpu_frequency_estimate_note"] = "CPU frequency unavailable"

    row["cpu_cluster_summary"] = " | ".join(parts) if parts else "n/a"
    return row


def derive_thermal_pressure_fields(row):
    """
    Combine macOS thermal pressure and MacPowerLab battery temperature trend.

    macOS can report pressure as Nominal while battery temperature is still rising.
    Both signals are useful, so the UI shows them together.
    """
    raw = row.get("thermal_pressure")
    raw_s = str(raw).strip() if raw is not None else ""

    battery_state = row.get("thermal_state")
    battery_s = str(battery_state).strip() if battery_state is not None else ""

    has_raw = raw_s and raw_s.lower() not in ("none", "n/a", "nan", "")
    has_battery = battery_s and battery_s.lower() not in ("none", "n/a", "nan", "")

    if has_raw and has_battery:
        row["thermal_pressure_effective"] = raw_s
        row["thermal_pressure_source"] = "powermetrics thermal sampler + battery trend"
        row["thermal_summary"] = f"macOS {raw_s} / battery {battery_s}"
    elif has_raw:
        row["thermal_pressure_effective"] = raw_s
        row["thermal_pressure_source"] = "powermetrics thermal sampler"
        row["thermal_summary"] = f"macOS {raw_s}"
    elif has_battery:
        row["thermal_pressure_effective"] = f"battery {battery_s}"
        row["thermal_pressure_source"] = "battery temp trend"
        row["thermal_summary"] = f"battery {battery_s}"
    else:
        row["thermal_pressure_effective"] = "n/a"
        row["thermal_pressure_source"] = "not exposed"
        row["thermal_summary"] = "n/a"

    return row

def parse_powermetrics_output(text):
    result = {
        "pm_status": "ok",
        "soc_power_w": None,
        "cpu_power_w": None,
        "gpu_power_w": None,
        "ane_power_w": None,
        "dram_power_w": None,
        "combined_power_w": None,
        "cpu_frequency_mhz": None,
        "gpu_frequency_mhz": None,
        "e_cluster_freq_mhz": None,
        "e_cluster_active_percent": None,
        "p0_cluster_freq_mhz": None,
        "p0_cluster_active_percent": None,
        "p1_cluster_freq_mhz": None,
        "p1_cluster_active_percent": None,
        "p_cluster_active_percent": None,
        "thermal_pressure": None,
        "cpu_frequency_estimate_mhz": None,
        "cpu_frequency_estimate_note": None,
        "cpu_cluster_summary": None,
        "thermal_pressure_effective": None,
        "thermal_pressure_source": None,
        "thermal_summary": None,
        "pm_updated": datetime.now().strftime("%H:%M:%S"),
    }

    patterns = [
        ("combined_power_w", r"(?im)^\s*Combined Power.*?:\s*([0-9.]+)\s*(mW|W)\b"),
        ("cpu_power_w", r"(?im)^\s*CPU Power:\s*([0-9.]+)\s*(mW|W)\b"),
        ("gpu_power_w", r"(?im)^\s*GPU Power:\s*([0-9.]+)\s*(mW|W)\b"),
        ("ane_power_w", r"(?im)^\s*ANE Power:\s*([0-9.]+)\s*(mW|W)\b"),
        ("dram_power_w", r"(?im)^\s*DRAM Power:\s*([0-9.]+)\s*(mW|W)\b"),
    ]

    for key, pattern in patterns:
        m = re.search(pattern, text)
        if m:
            result[key] = parse_power_value(m.group(1), m.group(2))

    # Flexible frequency/activity parsing. powermetrics changes between macOS releases.
    freq_patterns = [
        ("cpu_frequency_mhz", r"(?im)^\s*CPU Average frequency as fraction of nominal:\s*([0-9.]+)%"),
        ("gpu_frequency_mhz", r"(?im)^\s*GPU HW active frequency:\s*([0-9.]+)\s*MHz"),
    ]
    for key, pattern in freq_patterns:
        m = re.search(pattern, text)
        if m:
            try:
                result[key] = float(m.group(1))
            except Exception:
                pass

    thermal_patterns = [
        r"(?im)^\s*Current pressure level:\s*([A-Za-z0-9 _./-]+)",
        r"(?im)^\s*Thermal pressure:\s*([A-Za-z0-9 _./-]+)",
        r"(?im)^\s*thermal pressure:\s*([A-Za-z0-9 _./-]+)",
        r"(?im)^\s*Thermal level:\s*([A-Za-z0-9 _./-]+)",
        r"(?im)^\s*thermal level:\s*([A-Za-z0-9 _./-]+)",
        r"(?im)^\s*CPU thermal pressure:\s*([A-Za-z0-9 _./-]+)",
        r"(?im)^\s*GPU thermal pressure:\s*([A-Za-z0-9 _./-]+)",
    ]
    for pattern in thermal_patterns:
        m = re.search(pattern, text)
        if m:
            result["thermal_pressure"] = m.group(1).strip()
            break

    # macOS 27: CPU Power may stay 0 mW, but cluster frequency/residency is useful.
    cluster_patterns = [
        ("e_cluster_freq_mhz", r"(?im)^\s*E-Cluster HW active frequency:\s*([0-9.]+)\s*MHz"),
        ("e_cluster_active_percent", r"(?im)^\s*E-Cluster HW active residency:\s*([0-9.]+)%"),
        ("p0_cluster_freq_mhz", r"(?im)^\s*P0-Cluster HW active frequency:\s*([0-9.]+)\s*MHz"),
        ("p0_cluster_active_percent", r"(?im)^\s*P0-Cluster HW active residency:\s*([0-9.]+)%"),
        ("p1_cluster_freq_mhz", r"(?im)^\s*P1-Cluster HW active frequency:\s*([0-9.]+)\s*MHz"),
        ("p1_cluster_active_percent", r"(?im)^\s*P1-Cluster HW active residency:\s*([0-9.]+)%"),
    ]
    for key, pattern in cluster_patterns:
        m = re.search(pattern, text)
        if m:
            try:
                result[key] = float(m.group(1))
            except Exception:
                pass

    p_values = [result.get("p0_cluster_active_percent"), result.get("p1_cluster_active_percent")]
    p_values = [v for v in p_values if v is not None]
    if p_values:
        result["p_cluster_active_percent"] = max(p_values)

    subtotal = 0.0
    found = False
    for key in ("cpu_power_w", "gpu_power_w", "ane_power_w", "dram_power_w"):
        if result[key] is not None:
            subtotal += result[key]
            found = True

    if found:
        result["soc_power_w"] = result["combined_power_w"] if result["combined_power_w"] is not None else subtotal

    derive_cpu_cluster_fields(result)
    # thermal_state is not available inside the powermetrics-only parser; effective
    # thermal gets finalized after the full row is built.
    return result


class PowermetricsWorker:
    def __init__(self, interval=5.0, sample_ms=1000):
        self.interval = max(2.0, float(interval))
        self.sample_ms = max(250, int(sample_ms))
        self.latest = {
            "pm_status": "starting",
            "soc_power_w": None,
            "cpu_power_w": None,
            "gpu_power_w": None,
            "ane_power_w": None,
            "dram_power_w": None,
            "combined_power_w": None,
            "cpu_frequency_mhz": None,
            "gpu_frequency_mhz": None,
            "e_cluster_freq_mhz": None,
            "e_cluster_active_percent": None,
            "p0_cluster_freq_mhz": None,
            "p0_cluster_active_percent": None,
            "p1_cluster_freq_mhz": None,
            "p1_cluster_active_percent": None,
            "p_cluster_active_percent": None,
            "thermal_pressure": None,
            "cpu_frequency_estimate_mhz": None,
            "cpu_frequency_estimate_note": None,
            "cpu_cluster_summary": None,
            "thermal_pressure_effective": None,
            "thermal_pressure_source": None,
        }
        self.stop_event = threading.Event()
        self.thread = threading.Thread(target=self._run, daemon=True)

    def start(self):
        self.thread.start()

    def stop(self):
        self.stop_event.set()

    def snapshot(self):
        return dict(self.latest)

    def _try_powermetrics(self, cmd):
        return run_cmd(cmd, text=True, timeout=max(5, self.sample_ms / 1000 + 4))

    def _run_once(self):
        commands = [
            # Try thermal/smc samplers first when available. Older/newer macOS builds
            # may reject one of these sampler names, so we fall back cleanly.
            ["sudo", "-n", "powermetrics", "-n", "1", "-i", str(self.sample_ms), "--samplers", "cpu_power,gpu_power,thermal"],
            ["sudo", "-n", "powermetrics", "-n", "1", "-i", str(self.sample_ms), "--samplers", "cpu_power,gpu_power"],
            ["sudo", "-n", "powermetrics", "-n", "1", "-i", str(self.sample_ms)],
        ]

        last_error = None

        for cmd in commands:
            try:
                out = self._try_powermetrics(cmd)
                parsed = parse_powermetrics_output(out)
                if parsed.get("soc_power_w") is None:
                    parsed["pm_status"] = "no power fields found"
                self.latest = parsed
                return
            except subprocess.CalledProcessError as e:
                last_error = e.output.decode() if isinstance(e.output, bytes) else str(e.output)
            except Exception as e:
                last_error = str(e)

        if last_error and ("password" in last_error.lower() or "not allowed" in last_error.lower()):
            msg = "sudo required; run: sudo -v"
        else:
            msg = "unavailable"

        self.latest = {
            "pm_status": msg,
            "soc_power_w": None,
            "cpu_power_w": None,
            "gpu_power_w": None,
            "ane_power_w": None,
            "dram_power_w": None,
            "combined_power_w": None,
            "cpu_frequency_mhz": None,
            "gpu_frequency_mhz": None,
            "e_cluster_freq_mhz": None,
            "e_cluster_active_percent": None,
            "p0_cluster_freq_mhz": None,
            "p0_cluster_active_percent": None,
            "p1_cluster_freq_mhz": None,
            "p1_cluster_active_percent": None,
            "p_cluster_active_percent": None,
            "thermal_pressure": None,
            "cpu_frequency_estimate_mhz": None,
            "cpu_frequency_estimate_note": None,
            "cpu_cluster_summary": None,
            "thermal_pressure_effective": None,
            "thermal_pressure_source": None,
            "pm_updated": datetime.now().strftime("%H:%M:%S"),
        }

    def _run(self):
        while not self.stop_event.is_set():
            self._run_once()
            self.stop_event.wait(self.interval)


class SystemProfilerWorker:
    def __init__(self, interval=60.0):
        self.interval = max(15.0, float(interval))
        self.latest = {"sp_status": "starting"}
        self.stop_event = threading.Event()
        self.thread = threading.Thread(target=self._run, daemon=True)

    def start(self):
        self.thread.start()

    def stop(self):
        self.stop_event.set()

    def snapshot(self):
        return dict(self.latest)

    def _run(self):
        while not self.stop_event.is_set():
            data = read_system_profiler_power()
            data["sp_updated"] = datetime.now().strftime("%H:%M:%S")
            self.latest = data
            self.stop_event.wait(self.interval)


# -------------------------
# Logging / run files
# -------------------------

CSV_HEADERS = [
    "timestamp", "version", "run_id", "phase", "auto_phase", "event_count",
    "session_runtime", "mode", "power_source", "pmset_state",
    "battery_percent", "percent_rate_per_hour", "rolling_runtime_estimate", "rolling_time_to_full",
    "stable_health_percent", "stable_health_note",
    "battery_temp_c", "battery_temp_f", "virtual_temp_c", "avg_temp_c", "max_battery_temp_c", "temp_trend_c_per_min",
    "thermal_state", "possible_throttle",
    "bms_system_power_w", "bms_adapter_power_w", "bms_state_of_charge", "bms_daily_min_soc", "bms_daily_max_soc",
    "telemetry_battery_power_w", "telemetry_system_load_w", "telemetry_system_power_in_w", "telemetry_error_count",
    "telemetry_system_effective_total_load_raw",
    "telemetry_system_effective_total_load_w",
    "telemetry_accum_system_effective_total_load_raw",
    "telemetry_accum_system_effective_total_load_count",
    "pd_ipd_input_power_raw",
    "pd_ipd_input_power_w",
    "pd_ipd_input_voltage_raw",
    "pd_ipd_input_voltage_v",
    "pd_ipd_input_current_raw",
    "pd_ipd_input_current_a",
    "cell_voltage_min_mv", "cell_voltage_max_mv", "cell_voltage_delta_mv",
    "qmax_min", "qmax_max", "qmax_delta",
    "weighted_ra_min", "weighted_ra_max", "weighted_ra_delta",
    "lifetime_avg_temp", "lifetime_min_temp", "lifetime_max_temp",
    "lifetime_max_charge_current_ma", "lifetime_max_discharge_current_ma",
    "lifetime_max_pack_voltage_mv", "lifetime_min_pack_voltage_mv",
    "lifetime_cycle_count_last_qmax", "lifetime_total_operating_time",
    "net_battery_watts", "avg_net_battery_watts", "max_charge_watts", "max_discharge_watts",
    "whole_mac_watts_estimate", "avg_mac_use_watts", "max_mac_use_watts", "whole_mac_watts_note",
    "actual_battery_draw_w",
    "primary_total_load_w",
    "primary_total_load_source",
    "baseline_locked",
    "baseline_power_source",
    "baseline_primary_total_load_w",
    "baseline_cpu_power_w",
    "baseline_gpu_power_w",
    "load_delta_primary_total_load_w",
    "load_delta_cpu_power_w",
    "load_delta_gpu_power_w",
    "benchmark_load_w",
    "benchmark_power_source_note",
    "battery_wh_charged", "battery_wh_discharged", "battery_wh_net", "estimated_wh_remaining", "estimated_wh_full",
    "preferred_system_power_w", "battery_charge_acceptance_w", "charger_headroom_estimate_w", "visible_load_estimate_w", "charger_load_percent", "charger_live_verdict", "adapter_power_split_note",
    "phase_current_name", "phase_current_sample_count", "phase_current_net_avg_w", "phase_current_charge_peak_w", "phase_current_discharge_peak_w",
    "battery_voltage_v", "battery_amperage_a",
    "adapter_name", "adapter_reported_watts", "adapter_voltage_v", "adapter_current_a", "adapter_calculated_watts",
    "best_adapter_index", "charger_configuration", "apple_raw_external_connected",
    "cycle_count", "battery_health_percent", "battery_cell_disconnect_count", "apple_raw_current_capacity", "apple_raw_max_capacity", "design_capacity",
    "menu_time_estimate", "avg_time_to_full", "avg_time_to_empty",
    "pm_status", "soc_power_w", "avg_soc_power_w", "max_soc_power_w",
    "powermetrics_component_note",
    "stress_process_count",
    "stress_cpu_percent",
    "stress_mem_percent",
    "stress_rss_mb",
    "stress_process_names",
    "stress_workload_active",
    "app_power_status", "app_power_source", "app_power_confidence", "app_power_error",
    "app_power_sample_id", "app_power_sample_age_s",
    "app_power_total_system_w", "app_power_total_source",
    "app_power_baseline_w", "app_power_dynamic_w",
    "app_power_cpu_pool_w", "app_power_gpu_pool_w", "app_power_residual_pool_w", "app_power_method",
    "app_power_attributed_w", "app_power_unattributed_w",
    "app_power_top_summary", "app_power_top_json",
    "app_top_1_name", "app_top_1_category", "app_top_1_estimated_w", "app_top_1_dynamic_w", "app_top_1_cpu_w", "app_top_1_gpu_w", "app_top_1_residual_w", "app_top_1_share_percent", "app_top_1_energy_impact", "app_top_1_cpu_ms_per_s", "app_top_1_gpu_ms_per_s",
    "app_top_2_name", "app_top_2_category", "app_top_2_estimated_w", "app_top_2_dynamic_w", "app_top_2_cpu_w", "app_top_2_gpu_w", "app_top_2_residual_w", "app_top_2_share_percent", "app_top_2_energy_impact", "app_top_2_cpu_ms_per_s", "app_top_2_gpu_ms_per_s",
    "app_top_3_name", "app_top_3_category", "app_top_3_estimated_w", "app_top_3_dynamic_w", "app_top_3_cpu_w", "app_top_3_gpu_w", "app_top_3_residual_w", "app_top_3_share_percent", "app_top_3_energy_impact", "app_top_3_cpu_ms_per_s", "app_top_3_gpu_ms_per_s",
    "app_top_4_name", "app_top_4_category", "app_top_4_estimated_w", "app_top_4_dynamic_w", "app_top_4_cpu_w", "app_top_4_gpu_w", "app_top_4_residual_w", "app_top_4_share_percent", "app_top_4_energy_impact", "app_top_4_cpu_ms_per_s", "app_top_4_gpu_ms_per_s",
    "app_top_5_name", "app_top_5_category", "app_top_5_estimated_w", "app_top_5_dynamic_w", "app_top_5_cpu_w", "app_top_5_gpu_w", "app_top_5_residual_w", "app_top_5_share_percent", "app_top_5_energy_impact", "app_top_5_cpu_ms_per_s", "app_top_5_gpu_ms_per_s",
    "cpu_power_w", "avg_cpu_power_w", "max_cpu_power_w",
    "gpu_power_w", "avg_gpu_power_w", "max_gpu_power_w",
    "ane_power_w", "dram_power_w", "cpu_frequency_mhz", "cpu_frequency_estimate_mhz", "cpu_frequency_estimate_note", "cpu_cluster_summary", "gpu_frequency_mhz", "e_cluster_freq_mhz", "e_cluster_active_percent", "p0_cluster_freq_mhz", "p0_cluster_active_percent", "p1_cluster_freq_mhz", "p1_cluster_active_percent", "p_cluster_active_percent", "thermal_pressure", "thermal_pressure_effective", "thermal_pressure_source", "thermal_summary",
    "sp_status", "sp_ac_charger_watts", "sp_cycle_count", "sp_condition", "sp_full_charge_capacity", "sp_state_of_charge", "sp_ac_low_power_mode", "sp_battery_low_power_mode", "sp_ac_high_power_mode", "sp_battery_high_power_mode", "sp_ac_energy_mode", "sp_battery_energy_mode", "sp_charger_name", "sp_charger_connected", "sp_power_mode_warning",
]


class RunLogger:
    def __init__(self, log_arg, no_log=False, debug_every=30, keep_raw_debug=True):
        self.enabled = not no_log
        self.keep_raw_debug = keep_raw_debug
        self.debug_every = max(1, int(debug_every))
        self.sample_count = 0
        self.csv_fh = None
        self.csv_writer = None
        self.events_fh = None
        self.debug_path = None
        self.event_count = 0
        self.run_id = datetime.now().strftime("%Y%m%d_%H%M%S")
        self.base_dir = None
        self.csv_path = None
        self.events_path = None

        if not self.enabled:
            return

        script_dir = os.path.dirname(os.path.abspath(__file__))
        logs_dir = os.path.join(script_dir, "logs")
        os.makedirs(logs_dir, exist_ok=True)

        if log_arg and log_arg != "auto":
            self.csv_path = os.path.abspath(os.path.expanduser(log_arg))
            os.makedirs(os.path.dirname(self.csv_path), exist_ok=True)
            stem = os.path.splitext(os.path.basename(self.csv_path))[0]
            self.run_id = stem.replace("mac_power_", "") or self.run_id
            self.base_dir = os.path.dirname(self.csv_path)
        else:
            self.csv_path = os.path.join(logs_dir, f"mac_power_{self.run_id}.csv")
            self.base_dir = logs_dir

        stem = os.path.splitext(os.path.basename(self.csv_path))[0]
        self.events_path = os.path.join(self.base_dir, f"{stem}_events.jsonl")
        self.debug_path = os.path.join(self.base_dir, f"{stem}_debug.json")

        self.csv_fh = open(self.csv_path, "w", newline="", encoding="utf-8")
        self.csv_writer = csv.writer(self.csv_fh)
        self.csv_writer.writerow(CSV_HEADERS)

        self.events_fh = open(self.events_path, "w", encoding="utf-8")

        self.write_debug_header()

    def write_debug_header(self):
        if not self.enabled or not self.debug_path:
            return
        data = {
            "run_id": self.run_id,
            "version": VERSION,
            "created_at": datetime.now().isoformat(timespec="seconds"),
            "csv_path": self.csv_path,
            "events_path": self.events_path,
            "notes": [
                "Debug file is updated during the run.",
                "latest_raw_battery contains recursive AppleSmartBattery telemetry.",
                "debug_samples contains periodic snapshots only to keep file size reasonable.",
            ],
            "debug_samples": [],
        }
        with open(self.debug_path, "w", encoding="utf-8") as f:
            json.dump(data, f, indent=2)

    def write_csv(self, row):
        if not self.enabled or not self.csv_writer:
            return
        row["run_id"] = self.run_id
        row["event_count"] = self.event_count
        self.csv_writer.writerow([row.get(h) for h in CSV_HEADERS])
        self.csv_fh.flush()

    def write_event(self, event):
        if not self.enabled or not self.events_fh:
            return
        self.event_count += 1
        event = dict(event)
        event.setdefault("timestamp", datetime.now().isoformat(timespec="seconds"))
        event.setdefault("run_id", self.run_id)
        self.events_fh.write(json.dumps(event, ensure_ascii=False) + "\n")
        self.events_fh.flush()

    def maybe_write_debug(self, row, raw_battery):
        if not self.enabled or not self.debug_path or not self.keep_raw_debug:
            return

        self.sample_count += 1
        should_write = self.sample_count == 1 or (self.sample_count % self.debug_every == 0)
        if not should_write:
            return

        try:
            with open(self.debug_path, "r", encoding="utf-8") as f:
                data = json.load(f)
        except Exception:
            data = {"run_id": self.run_id, "debug_samples": []}

        sample = {
            "timestamp": row.get("timestamp"),
            "phase": row.get("phase"),
            "auto_phase": row.get("auto_phase"),
            "mode": row.get("mode"),
            "power_source": row.get("power_source"),
            "net_battery_watts": row.get("net_battery_watts"),
            "adapter_reported_watts": row.get("adapter_reported_watts"),
            "nested_power_fields": {
                "AppleRawAdapterDetails": row.get("apple_raw_adapter_details"),
                "ChargerData": row.get("charger_data"),
                "PortControllerInfo": row.get("port_controller_info"),
                "PowerTelemetryData": row.get("power_telemetry_data"),
                "FedDetails": row.get("fed_details"),
            },
            "parsed_bms_fields": {
                "bms_system_power_w": row.get("bms_system_power_w"),
                "telemetry_system_load_w": row.get("telemetry_system_load_w"),
                "telemetry_battery_power_w": row.get("telemetry_battery_power_w"),
                "cell_voltage_min_mv": row.get("cell_voltage_min_mv"),
                "cell_voltage_max_mv": row.get("cell_voltage_max_mv"),
                "cell_voltage_delta_mv": row.get("cell_voltage_delta_mv"),
                "qmax_min": row.get("qmax_min"),
                "qmax_max": row.get("qmax_max"),
                "qmax_delta": row.get("qmax_delta"),
                "lifetime_max_temp": row.get("lifetime_max_temp"),
                "lifetime_max_discharge_current_ma": row.get("lifetime_max_discharge_current_ma"),
            },
            "raw_battery": recursive_sanitize(raw_battery),
        }

        data["latest_sample"] = sample
        data.setdefault("debug_samples", []).append(sample)
        # Keep last 50 debug snapshots.
        data["debug_samples"] = data["debug_samples"][-50:]

        with open(self.debug_path, "w", encoding="utf-8") as f:
            json.dump(data, f, indent=2, ensure_ascii=False)

    def close(self):
        if self.csv_fh:
            self.csv_fh.close()
        if self.events_fh:
            self.events_fh.close()


# -------------------------
# Stats / events
# -------------------------

class StatsTracker:
    def __init__(self, window_seconds=60):
        self.window_seconds = float(window_seconds)
        self.start_time = time.time()
        self.samples = deque()
        self.max_temp_c = None
        self.max_charge_w = None
        self.max_discharge_w = None
        self.max_mac_estimate_w = None
        self.max_soc_w = None
        self.max_cpu_w = None
        self.max_gpu_w = None
        self.wh_charged = 0.0
        self.wh_discharged = 0.0
        self.wh_net = 0.0
        self.last_energy_t = None

    def add(self, row):
        now = time.time()
        net = row.get("net_battery_watts")

        if self.last_energy_t is not None and net is not None:
            dt_hours = max(0.0, (now - self.last_energy_t) / 3600.0)
            wh = net * dt_hours
            self.wh_net += wh
            if wh >= 0:
                self.wh_charged += wh
            else:
                self.wh_discharged += abs(wh)
        self.last_energy_t = now

        entry = {
            "t": now,
            "percent": row.get("battery_percent"),
            "raw_current": row.get("apple_raw_current_capacity"),
            "temp_c": row.get("battery_temp_c"),
            "net_w": row.get("net_battery_watts"),
            "mac_w": row.get("whole_mac_watts_estimate"),
            "primary_w": row.get("primary_total_load_w"),
            "soc_w": row.get("soc_power_w"),
            "cpu_w": row.get("cpu_power_w"),
            "gpu_w": row.get("gpu_power_w"),
        }
        self.samples.append(entry)

        cutoff = now - self.window_seconds
        while self.samples and self.samples[0]["t"] < cutoff:
            self.samples.popleft()

        temp = row.get("battery_temp_c")
        if temp is not None:
            self.max_temp_c = temp if self.max_temp_c is None else max(self.max_temp_c, temp)

        if net is not None:
            if net > 0:
                self.max_charge_w = net if self.max_charge_w is None else max(self.max_charge_w, net)
            elif net < 0:
                discharge = abs(net)
                self.max_discharge_w = discharge if self.max_discharge_w is None else max(self.max_discharge_w, discharge)

        mac_w = row.get("primary_total_load_w") or row.get("whole_mac_watts_estimate")
        if mac_w is not None:
            self.max_mac_estimate_w = mac_w if self.max_mac_estimate_w is None else max(self.max_mac_estimate_w, mac_w)

        for key, attr in [("soc_power_w", "max_soc_w"), ("cpu_power_w", "max_cpu_w"), ("gpu_power_w", "max_gpu_w")]:
            val = row.get(key)
            if val is not None:
                current = getattr(self, attr)
                setattr(self, attr, val if current is None else max(current, val))

    def avg(self, key):
        vals = [s[key] for s in self.samples if s.get(key) is not None]
        return sum(vals) / len(vals) if vals else None

    def trend_per_minute(self, key):
        vals = [s for s in self.samples if s.get(key) is not None]
        if len(vals) < 2:
            return None
        first = vals[0]
        last = vals[-1]
        dt_min = (last["t"] - first["t"]) / 60.0
        if dt_min <= 0:
            return None
        return (last[key] - first[key]) / dt_min

    def capacity_rate_percent_per_hour(self):
        vals = [s for s in self.samples if s.get("percent") is not None]
        if len(vals) < 2:
            return None
        first = vals[0]
        last = vals[-1]
        dt_hr = (last["t"] - first["t"]) / 3600.0
        if dt_hr <= 0:
            return None
        return (last["percent"] - first["percent"]) / dt_hr

    def runtime_remaining_from_rate(self, percent_now):
        rate = self.capacity_rate_percent_per_hour()
        if rate is None or percent_now is None or rate >= -0.1:
            return None
        hours = percent_now / abs(rate)
        return hours * 60

    def time_to_full_from_rate(self, percent_now):
        rate = self.capacity_rate_percent_per_hour()
        if rate is None or percent_now is None or rate <= 0.1:
            return None
        hours = (100.0 - percent_now) / rate
        return hours * 60

    def thermal_state(self):
        trend = self.trend_per_minute("temp_c")
        if trend is None:
            return "n/a"
        if trend > 0.25:
            return "rising"
        if trend < -0.25:
            return "falling"
        return "stable / plateau"

    def possible_throttle(self, row):
        """
        Conservative throttling candidate.

        v0.8.6: Do not flag just because powermetrics SoC/component watts drop.
        On recent macOS/Apple Silicon builds, component watts can go low while the
        real battery/BMS/load data and cluster activity show the workload is normal.
        """
        phase_text = ((row.get("phase") or "") + " " + (row.get("auto_phase") or "")).lower()
        if "stress" not in phase_text and "load" not in phase_text and "max" not in phase_text:
            return False

        trend = self.trend_per_minute("temp_c")
        if trend is not None and trend < -0.05:
            return False

        thermal_pressure = str(row.get("thermal_pressure") or "").lower()
        if thermal_pressure and thermal_pressure not in ("n/a", "nominal", "none"):
            return True

        stress_cpu = row.get("stress_cpu_percent") or 0
        primary = row.get("primary_total_load_w")
        primary_peak = self.max_mac_estimate_w

        # Only infer possible throttling from power drop when the workload is still
        # clearly trying to run hard.
        if primary is not None and primary_peak is not None and primary_peak >= 70:
            if primary < primary_peak * 0.60 and stress_cpu >= 300:
                return True

        # Last-resort old SoC/component fallback, but only under high process CPU.
        soc = row.get("soc_power_w")
        if soc is not None and self.max_soc_w is not None and self.max_soc_w >= 35 and stress_cpu >= 400:
            return soc < (self.max_soc_w * 0.55)

        return False

    def enrich(self, row):
        self.add(row)

        row["session_runtime"] = format_duration(time.time() - self.start_time)
        row["rolling_window_seconds"] = self.window_seconds

        row["avg_net_battery_watts"] = self.avg("net_w")
        row["avg_mac_use_watts"] = self.avg("mac_w")
        row["avg_temp_c"] = self.avg("temp_c")
        row["avg_soc_power_w"] = self.avg("soc_w")
        row["avg_cpu_power_w"] = self.avg("cpu_w")
        row["avg_gpu_power_w"] = self.avg("gpu_w")

        row["max_battery_temp_c"] = self.max_temp_c
        row["max_charge_watts"] = self.max_charge_w
        row["max_discharge_watts"] = self.max_discharge_w
        row["max_mac_use_watts"] = self.max_mac_estimate_w
        row["max_soc_power_w"] = self.max_soc_w
        row["max_cpu_power_w"] = self.max_cpu_w
        row["max_gpu_power_w"] = self.max_gpu_w

        row["temp_trend_c_per_min"] = self.trend_per_minute("temp_c")
        row["thermal_state"] = self.thermal_state()
        row["percent_rate_per_hour"] = self.capacity_rate_percent_per_hour()

        runtime_mins = self.runtime_remaining_from_rate(row.get("battery_percent"))
        charge_mins = self.time_to_full_from_rate(row.get("battery_percent"))

        row["rolling_runtime_estimate"] = format_minutes(int(runtime_mins)) if runtime_mins is not None else "n/a"
        row["rolling_time_to_full"] = format_minutes(int(charge_mins)) if charge_mins is not None else "n/a"

        row["battery_wh_charged"] = self.wh_charged
        row["battery_wh_discharged"] = self.wh_discharged
        row["battery_wh_net"] = self.wh_net

        row["possible_throttle"] = self.possible_throttle(row)

        return row



class PhaseStatsTracker:
    def __init__(self):
        self.current_phase = None
        self.samples = []

    def phase_name(self, row):
        phase = row.get("phase")
        auto = row.get("auto_phase")
        if phase and phase != "idle / unmarked":
            return phase
        return auto or phase or "unmarked"

    def enrich(self, row):
        phase = self.phase_name(row)
        if phase != self.current_phase:
            self.current_phase = phase
            self.samples = []

        net = row.get("net_battery_watts")
        self.samples.append(net)

        vals = [v for v in self.samples if v is not None]
        charges = [v for v in vals if v > 0]
        discharges = [abs(v) for v in vals if v < 0]

        row["phase_current_name"] = phase
        row["phase_current_sample_count"] = len(self.samples)
        row["phase_current_net_avg_w"] = (sum(vals) / len(vals)) if vals else None
        row["phase_current_charge_peak_w"] = max(charges) if charges else None
        row["phase_current_discharge_peak_w"] = max(discharges) if discharges else None
        return row


class StableHealthTracker:
    def __init__(self, max_samples=60):
        self.samples = deque(maxlen=max_samples)

    def is_stable_sample(self, row):
        source = row.get("power_source")
        if source != "AC Power":
            return False

        phase_text = ((row.get("phase") or "") + " " + (row.get("auto_phase") or "")).lower()
        if "load" in phase_text or "stress" in phase_text or "max" in phase_text:
            return False

        temp = row.get("battery_temp_c")
        if temp is not None and temp >= 35:
            return False

        sys_w = row.get("bms_system_power_w")
        if sys_w is not None and sys_w >= 45:
            return False

        return row.get("battery_health_percent") is not None

    def enrich(self, row):
        if self.is_stable_sample(row):
            self.samples.append(float(row["battery_health_percent"]))

        if self.samples:
            row["stable_health_percent"] = sum(self.samples) / len(self.samples)
            row["stable_health_note"] = "stable AC/light-load estimate"
        else:
            row["stable_health_percent"] = None
            row["stable_health_note"] = "not enough stable AC/light-load samples"

        return row

class BaselineTracker:
    def __init__(self, seconds=45):
        self.seconds = float(seconds)
        self.start = time.time()
        self.rows = []
        self.locked = False
        self.baseline = {}

    def add(self, row):
        if not self.locked and time.time() - self.start <= self.seconds:
            self.rows.append(dict(row))
        elif not self.locked:
            self.lock()

        self.enrich(row)
        return row

    def lock(self):
        self.locked = True
        def avg_field(field):
            vals = []
            for r in self.rows:
                v = r.get(field)
                if v is not None:
                    vals.append(v)
            return sum(vals) / len(vals) if vals else None

        power_sources = [str(r.get("power_source")) for r in self.rows if r.get("power_source")]
        baseline_power_source = max(set(power_sources), key=power_sources.count) if power_sources else None

        self.baseline = {
            "baseline_net_battery_watts": avg_field("net_battery_watts"),
            "baseline_primary_total_load_w": avg_field("primary_total_load_w"),
            "baseline_soc_power_w": avg_field("soc_power_w"),
            "baseline_cpu_power_w": avg_field("cpu_power_w"),
            "baseline_gpu_power_w": avg_field("gpu_power_w"),
            "baseline_temp_c": avg_field("battery_temp_c"),
            "baseline_power_source": baseline_power_source,
        }

    def enrich(self, row):
        if not self.locked and time.time() - self.start > self.seconds:
            self.lock()

        for k, v in self.baseline.items():
            row[k] = v

        row["baseline_locked"] = self.locked

        def delta(field, base_key, out_key):
            val = row.get(field)
            base = self.baseline.get(base_key)
            row[out_key] = (val - base) if val is not None and base is not None else None

        delta("net_battery_watts", "baseline_net_battery_watts", "load_delta_net_battery_watts")
        delta("primary_total_load_w", "baseline_primary_total_load_w", "load_delta_primary_total_load_w")
        delta("soc_power_w", "baseline_soc_power_w", "load_delta_soc_power_w")
        delta("cpu_power_w", "baseline_cpu_power_w", "load_delta_cpu_power_w")
        delta("gpu_power_w", "baseline_gpu_power_w", "load_delta_gpu_power_w")


class EventDetector:
    def __init__(self, logger):
        self.logger = logger
        self.prev = None
        self.auto_phase = "observing"
        self.ac_discharging_since = None
        self.load_started = False
        self.last_event_t = {}
        self.last_adapter_watts = None
        self.last_adapter_name = None

    def emit(self, event_type, row, details=None, cooldown=0):
        now = time.time()
        if cooldown:
            last = self.last_event_t.get(event_type, 0)
            if now - last < cooldown:
                return
            self.last_event_t[event_type] = now

        event = {
            "type": event_type,
            "phase": row.get("phase"),
            "auto_phase": self.auto_phase,
            "mode": row.get("mode"),
            "power_source": row.get("power_source"),
            "net_battery_watts": row.get("net_battery_watts"),
            "soc_power_w": row.get("soc_power_w"),
            "cpu_power_w": row.get("cpu_power_w"),
            "gpu_power_w": row.get("gpu_power_w"),
            "adapter_reported_watts": row.get("adapter_reported_watts"),
            "details": details or {},
        }
        self.logger.write_event(event)

    def update_auto_phase(self, row):
        soc = row.get("soc_power_w")
        net = row.get("net_battery_watts")
        source = row.get("power_source")
        phase = row.get("phase") or ""

        if phase != "idle / unmarked":
            self.auto_phase = phase
            return

        heavy_soc = soc is not None and soc >= 35
        heavy_discharge = net is not None and net <= -60

        if source == "Battery Power" and (heavy_soc or heavy_discharge):
            self.auto_phase = "auto: load on battery"
        elif source == "AC Power" and heavy_soc:
            self.auto_phase = "auto: load on AC"
        elif source == "Battery Power":
            self.auto_phase = "auto: battery idle/light"
        elif source == "AC Power":
            self.auto_phase = "auto: AC idle/light"
        else:
            self.auto_phase = "auto: unknown"

    def process(self, row):
        self.update_auto_phase(row)
        row["auto_phase"] = self.auto_phase

        # First sample.
        if self.prev is None:
            self.last_adapter_watts = row.get("adapter_reported_watts")
            self.last_adapter_name = row.get("adapter_name")
            self.emit("run_started", row, cooldown=0)
            self.prev = dict(row)
            return row

        prev = self.prev

        # Power source events.
        if row.get("power_source") != prev.get("power_source"):
            if row.get("power_source") == "Battery Power":
                self.emit("ac_unplugged", row, {"previous_source": prev.get("power_source")})
            elif row.get("power_source") == "AC Power":
                self.emit("ac_plugged_in", row, {"previous_source": prev.get("power_source")})
            else:
                self.emit("power_source_changed", row, {"previous_source": prev.get("power_source")})

        # Adapter changes.
        current_w = row.get("adapter_reported_watts")
        if current_w != self.last_adapter_watts:
            self.emit("adapter_wattage_changed", row, {"from": self.last_adapter_watts, "to": current_w})
            self.last_adapter_watts = current_w

        current_name = row.get("adapter_name")
        if current_name != self.last_adapter_name:
            self.emit("adapter_name_changed", row, {"from": self.last_adapter_name, "to": current_name})
            self.last_adapter_name = current_name

        # Load started / stopped.
        soc = row.get("soc_power_w")
        net = row.get("net_battery_watts")
        heavy_now = (soc is not None and soc >= 35) or (net is not None and net <= -60)
        if heavy_now and not self.load_started:
            self.load_started = True
            self.emit("load_started_detected", row)
        elif not heavy_now and self.load_started:
            self.load_started = False
            self.emit("load_stopped_detected", row)

        # AC connected but discharging.
        if row.get("power_source") == "AC Power" and net is not None and net < -5:
            if self.ac_discharging_since is None:
                self.ac_discharging_since = time.time()
            elif time.time() - self.ac_discharging_since >= 8:
                self.emit("warning_ac_connected_but_battery_discharging", row, {
                    "seconds": round(time.time() - self.ac_discharging_since, 1)
                }, cooldown=10)
        else:
            self.ac_discharging_since = None

        # Charge throttling / low charge at high load.
        if row.get("power_source") == "AC Power" and soc is not None and soc >= 35 and net is not None and 0 <= net <= 10:
            self.emit("possible_charge_throttling_or_low_headroom", row, cooldown=15)

        # Possible throttle.
        if row.get("possible_throttle"):
            self.emit("possible_thermal_or_power_throttling", row, cooldown=20)

        self.prev = dict(row)
        return row


# -------------------------
# Collect reading
# -------------------------

def collect_reading(phase_file, powermetrics_snapshot=None, system_profiler_snapshot=None):
    b = read_battery()
    banks = read_battery_banks()
    pmset_text = read_pmset_status()
    pmset_info = parse_pmset(pmset_text)

    electrical = battery_electrical(b)
    adapter = adapter_info(b)
    temp_raw, temp_c, temp_f, virtual_raw, virtual_c, virtual_f = temperature_from_battery(b, banks)
    bms_stats = battery_data_stats(b, banks)
    telemetry_stats = power_telemetry_stats(b)
    pd_stats = power_distribution_stats(b)

    ioreg_is_charging = get_bool(b, "IsCharging")
    ioreg_external_connected = get_bool(b, "ExternalConnected")

    mode = power_mode(pmset_info, ioreg_external_connected, ioreg_is_charging, electrical["net_battery_watts"])
    whole_mac_watts, whole_mac_note = whole_mac_power_estimate(mode, electrical["net_battery_watts"])

    wh_now = estimated_wh(b.get("AppleRawCurrentCapacity"), electrical["battery_voltage_v"])
    wh_full = estimated_wh(b.get("AppleRawMaxCapacity") or b.get("NominalChargeCapacity"), electrical["battery_voltage_v"])

    row = {
        "timestamp": datetime.now().isoformat(timespec="seconds"),
        "display_time": datetime.now().strftime("%Y-%m-%d %H:%M:%S"),
        "version": VERSION,
        "phase": read_phase(phase_file),
        "mode": mode,
        "power_source": pmset_info.get("source"),
        "pmset_state": pmset_info.get("state"),
        "battery_percent": calc_percent(b, pmset_info),
        "battery_temp_raw": temp_raw,
        "battery_temp_c": temp_c,
        "battery_temp_f": temp_f,
        "virtual_temp_raw": virtual_raw,
        "virtual_temp_c": virtual_c,
        "virtual_temp_f": virtual_f,
        "bms_system_power_w": bms_stats.get("bms_system_power_w"),
        "bms_adapter_power_w": bms_stats.get("bms_adapter_power_w"),
        "bms_state_of_charge": bms_stats.get("bms_state_of_charge"),
        "bms_filtered_current": bms_stats.get("bms_filtered_current"),
        "bms_iss_current": bms_stats.get("bms_iss_current"),
        "bms_gauge_flags_raw": bms_stats.get("bms_gauge_flags_raw"),
        "bms_daily_min_soc": bms_stats.get("bms_daily_min_soc"),
        "bms_daily_max_soc": bms_stats.get("bms_daily_max_soc"),
        "bms_data_flash_write_count": bms_stats.get("bms_data_flash_write_count"),
        "cell_voltage_min_mv": bms_stats.get("cell_voltage_min_mv"),
        "cell_voltage_max_mv": bms_stats.get("cell_voltage_max_mv"),
        "cell_voltage_delta_mv": bms_stats.get("cell_voltage_delta_mv"),
        "cell_voltage_values": bms_stats.get("cell_voltage_values"),
        "qmax_min": bms_stats.get("qmax_min"),
        "qmax_max": bms_stats.get("qmax_max"),
        "qmax_delta": bms_stats.get("qmax_delta"),
        "qmax_values": bms_stats.get("qmax_values"),
        "weighted_ra_min": bms_stats.get("weighted_ra_min"),
        "weighted_ra_max": bms_stats.get("weighted_ra_max"),
        "weighted_ra_delta": bms_stats.get("weighted_ra_delta"),
        "weighted_ra_values": bms_stats.get("weighted_ra_values"),
        "lifetime_avg_temp": bms_stats.get("lifetime_avg_temp"),
        "lifetime_min_temp": bms_stats.get("lifetime_min_temp"),
        "lifetime_max_temp": bms_stats.get("lifetime_max_temp"),
        "lifetime_max_charge_current_ma": bms_stats.get("lifetime_max_charge_current_ma"),
        "lifetime_max_discharge_current_ma": bms_stats.get("lifetime_max_discharge_current_ma"),
        "lifetime_max_pack_voltage_mv": bms_stats.get("lifetime_max_pack_voltage_mv"),
        "lifetime_min_pack_voltage_mv": bms_stats.get("lifetime_min_pack_voltage_mv"),
        "lifetime_cycle_count_last_qmax": bms_stats.get("lifetime_cycle_count_last_qmax"),
        "lifetime_total_operating_time": bms_stats.get("lifetime_total_operating_time"),
        "lifetime_temperature_samples": bms_stats.get("lifetime_temperature_samples"),
        "telemetry_battery_power_raw": telemetry_stats.get("telemetry_battery_power_raw"),
        "telemetry_battery_power_w": telemetry_stats.get("telemetry_battery_power_w"),
        "telemetry_system_load_raw": telemetry_stats.get("telemetry_system_load_raw"),
        "telemetry_system_load_w": telemetry_stats.get("telemetry_system_load_w"),
        "telemetry_system_power_in_raw": telemetry_stats.get("telemetry_system_power_in_raw"),
        "telemetry_system_power_in_w": telemetry_stats.get("telemetry_system_power_in_w"),
        "telemetry_system_effective_total_load_raw": telemetry_stats.get("telemetry_system_effective_total_load_raw"),
        "telemetry_system_effective_total_load_w": telemetry_stats.get("telemetry_system_effective_total_load_w"),
        "telemetry_accum_system_effective_total_load_raw": telemetry_stats.get("telemetry_accum_system_effective_total_load_raw"),
        "telemetry_accum_system_effective_total_load_count": telemetry_stats.get("telemetry_accum_system_effective_total_load_count"),
        "telemetry_wall_energy_estimate_raw": telemetry_stats.get("telemetry_wall_energy_estimate_raw"),
        "telemetry_adapter_efficiency_loss_raw": telemetry_stats.get("telemetry_adapter_efficiency_loss_raw"),
        "telemetry_accumulated_system_energy_raw": telemetry_stats.get("telemetry_accumulated_system_energy_raw"),
        "telemetry_accumulated_wall_energy_raw": telemetry_stats.get("telemetry_accumulated_wall_energy_raw"),
        "telemetry_error_count": telemetry_stats.get("telemetry_error_count"),
        "pd_ipd_input_power_raw": pd_stats.get("pd_ipd_input_power_raw"),
        "pd_ipd_input_power_w": pd_stats.get("pd_ipd_input_power_w"),
        "pd_ipd_input_voltage_raw": pd_stats.get("pd_ipd_input_voltage_raw"),
        "pd_ipd_input_voltage_v": pd_stats.get("pd_ipd_input_voltage_v"),
        "pd_ipd_input_current_raw": pd_stats.get("pd_ipd_input_current_raw"),
        "pd_ipd_input_current_a": pd_stats.get("pd_ipd_input_current_a"),
        "battery_installed": get_bool(b, "BatteryInstalled"),
        "is_charging": ioreg_is_charging,
        "plugged_in": ioreg_external_connected,
        "charge_capable": get_bool(b, "ExternalChargeCapable"),
        "fully_charged": get_bool(b, "FullyCharged"),
        "at_critical_level": b.get("AtCriticalLevel"),
        "battery_voltage_v": electrical["battery_voltage_v"],
        "battery_amperage_a": electrical["battery_amperage_a"],
        "battery_voltage_mv_raw": electrical["battery_voltage_mv"],
        "battery_amperage_ma_raw": electrical["battery_amperage_ma"],
        "net_battery_watts": electrical["net_battery_watts"],
        "whole_mac_watts_estimate": whole_mac_watts,
        "whole_mac_watts_note": whole_mac_note,
        "estimated_wh_remaining": wh_now,
        "estimated_wh_full": wh_full,
        "adapter_name": adapter["name"],
        "adapter_reported_watts": adapter["reported_watts"],
        "adapter_voltage_v": adapter["voltage_v"],
        "adapter_current_a": adapter["current_a"],
        "adapter_calculated_watts": adapter["calculated_watts"],
        "adapter_raw_voltage": adapter["raw_voltage"],
        "adapter_raw_current": adapter["raw_current"],
        "best_adapter_index": adapter["best_adapter_index"],
        "charger_configuration": adapter["charger_configuration"],
        "apple_raw_external_connected": adapter["apple_raw_external_connected"],
        "apple_raw_adapter_details": adapter["raw_adapter_details"],
        "charger_data": adapter["charger_data"],
        "port_controller_info": adapter["port_controller_info"],
        "power_telemetry_data": adapter["power_telemetry_data"],
        "fed_details": adapter["fed_details"],
        "battery_cell_disconnect_count": b.get("BatteryCellDisconnectCount"),
        "battery_shutdown_reason": b.get("BatteryShutdownReason"),
        "cycle_count": b.get("CycleCount"),
        "current_capacity": b.get("CurrentCapacity"),
        "max_capacity": b.get("MaxCapacity"),
        "design_capacity": b.get("DesignCapacity"),
        "design_cycle_count_9c": b.get("DesignCycleCount9C"),
        "apple_raw_current_capacity": b.get("AppleRawCurrentCapacity"),
        "apple_raw_max_capacity": b.get("AppleRawMaxCapacity"),
        "nominal_charge_capacity": b.get("NominalChargeCapacity"),
        "pack_reserve": b.get("PackReserve"),
        "battery_health_percent": estimate_battery_health(b),
        "menu_time_estimate": pmset_info.get("time_remaining") or format_minutes(time_field_minutes(b.get("TimeRemaining"))),
        "avg_time_to_full": format_minutes(time_field_minutes(b.get("AvgTimeToFull"))),
        "avg_time_to_empty": format_minutes(time_field_minutes(b.get("AvgTimeToEmpty"))),
        "instant_time_to_empty": format_minutes(time_field_minutes(b.get("InstantTimeToEmpty"))),
        "optimized_charging": b.get("OptimizedBatteryChargingEngaged"),
        "charging_override": b.get("ChargingOverride"),
        "operation_status": b.get("OperationStatus"),
        "failure_status": b.get("PermanentFailureStatus"),
        "raw_keys": sorted(str(k) for k in b.keys()),
    }

    if powermetrics_snapshot:
        row.update(powermetrics_snapshot)
    else:
        row.update({
            "pm_status": "off",
            "soc_power_w": None,
            "cpu_power_w": None,
            "gpu_power_w": None,
            "ane_power_w": None,
            "dram_power_w": None,
            "combined_power_w": None,
            "cpu_frequency_mhz": None,
            "gpu_frequency_mhz": None,
            "e_cluster_freq_mhz": None,
            "e_cluster_active_percent": None,
            "p0_cluster_freq_mhz": None,
            "p0_cluster_active_percent": None,
            "p1_cluster_freq_mhz": None,
            "p1_cluster_active_percent": None,
            "p_cluster_active_percent": None,
            "thermal_pressure": None,
            "cpu_frequency_estimate_mhz": None,
            "cpu_frequency_estimate_note": None,
            "cpu_cluster_summary": None,
            "thermal_pressure_effective": None,
            "thermal_pressure_source": None,
            "thermal_summary": None,
        })

    if system_profiler_snapshot:
        row.update(system_profiler_snapshot)
    else:
        row.update({
            "sp_status": "off",
            "sp_ac_charger_watts": None,
            "sp_cycle_count": None,
            "sp_condition": None,
            "sp_full_charge_capacity": None,
            "sp_state_of_charge": None,
            "sp_ac_low_power_mode": None,
            "sp_battery_low_power_mode": None,
            "sp_ac_high_power_mode": None,
            "sp_battery_high_power_mode": None,
            "sp_ac_energy_mode": None,
            "sp_battery_energy_mode": None,
            "sp_charger_name": None,
            "sp_charger_connected": None,
            "sp_power_mode_warning": None,
        })

    derive_cpu_cluster_fields(row)
    derive_thermal_pressure_fields(row)

    # Primary total-load estimate / benchmark hierarchy.
    net_w = row.get("net_battery_watts")
    row["actual_battery_draw_w"] = abs(net_w) if row.get("mode") == "DISCHARGING" and net_w is not None and net_w < 0 else None
    primary_w, primary_src = primary_total_load(row)
    row["primary_total_load_w"] = primary_w
    row["primary_total_load_source"] = primary_src
    row["benchmark_load_w"] = primary_w
    row["benchmark_power_source_note"] = primary_src

    # Charger visible load/headroom estimate.
    #
    # Important: adapter_calculated_watts is usually the negotiated USB-C PD
    # contract/capacity (for example 28V * 5A = 140W), not actual live wall draw.
    # Do NOT treat it as real charger load or the UI will show 100% all the time.
    #
    # Best available AC output estimate:
    #   charging on AC:    system load + battery charge acceptance
    #   battery assist AC: system load - battery discharge assist
    #   neutral AC:        system load
    #
    # True wall draw still requires a wall/USB-C watt meter.
    adapter_capacity_w = row.get("adapter_reported_watts")
    net_battery_w = row.get("net_battery_watts")
    primary_w = row.get("primary_total_load_w")
    ac_output_estimate_w = None
    ac_output_source = None
    battery_assist_w = None

    if row.get("power_source") == "AC Power" and primary_w is not None:
        try:
            primary_float = float(primary_w)
            net_float = float(net_battery_w) if net_battery_w is not None else 0.0

            if net_float > 1.0:
                ac_output_estimate_w = primary_float + net_float
                ac_output_source = "system load + battery charge acceptance"
            elif net_float < -1.0:
                battery_assist_w = abs(net_float)
                ac_output_estimate_w = max(0.0, primary_float - battery_assist_w)
                ac_output_source = "system load minus battery assist"
            else:
                ac_output_estimate_w = primary_float
                ac_output_source = "system load estimate"
        except Exception:
            ac_output_estimate_w = None
            ac_output_source = None

    row["ac_output_estimate_w"] = ac_output_estimate_w
    row["ac_output_estimate_source"] = ac_output_source
    row["battery_assist_w"] = battery_assist_w

    if adapter_capacity_w is not None and row.get("power_source") == "AC Power" and ac_output_estimate_w is not None:
        try:
            adapter_float = float(adapter_capacity_w)
            draw_float = float(ac_output_estimate_w)
            row["visible_load_estimate_w"] = draw_float
            row["charger_headroom_estimate_w"] = adapter_float - draw_float
            row["charger_load_percent"] = (draw_float / adapter_float * 100.0) if adapter_float > 0 else None
        except Exception:
            row["visible_load_estimate_w"] = None
            row["charger_headroom_estimate_w"] = None
            row["charger_load_percent"] = None
    else:
        row["visible_load_estimate_w"] = None
        row["charger_headroom_estimate_w"] = None
        row["charger_load_percent"] = None

    row.update(stress_process_activity())
    # AC power split: useful when plugged in and battery is charging quickly.
    net_for_split = row.get("net_battery_watts")
    row["battery_charge_acceptance_w"] = net_for_split if net_for_split is not None and net_for_split > 0 else 0
    if row.get("power_source") == "AC Power":
        row["adapter_power_split_note"] = (
            f"system {fmt(row.get('preferred_system_power_w'), ' W', 2)} / "
            f"battery {fmt(row.get('battery_charge_acceptance_w'), ' W', 2)} / "
            f"headroom {fmt(row.get('charger_headroom_estimate_w'), ' W', 2)}"
        )
    else:
        row["adapter_power_split_note"] = "battery power"

    row["charger_live_verdict"] = charger_live_verdict(row)
    row["powermetrics_component_note"] = powermetrics_component_note(row)

    return row, b


# -------------------------
# Display formatting
# -------------------------

def fmt(value, suffix="", digits=2):
    if value is None:
        return "n/a"
    if isinstance(value, bool):
        return str(value)
    if isinstance(value, float):
        return f"{value:.{digits}f}{suffix}"
    return f"{value}{suffix}"


def signed_fmt(value, suffix="", digits=2):
    if value is None:
        return "n/a"
    try:
        return f"{float(value):+.{digits}f}{suffix}"
    except Exception:
        return str(value)


def safe_str(value):
    return "n/a" if value is None else str(value)


def short(text, width):
    text = str(text)
    if width <= 0:
        return ""
    if len(text) <= width:
        return text
    if width == 1:
        return "…"
    return text[: width - 1] + "…"


class UI:
    def __init__(self, stdscr, plain=False):
        self.stdscr = stdscr
        self.plain = plain
        self.has_color = False
        self.setup()

    def setup(self):
        curses.curs_set(0)
        self.stdscr.nodelay(True)
        self.stdscr.keypad(True)

        if not self.plain and curses.has_colors():
            curses.start_color()
            try:
                curses.use_default_colors()
                default_bg = -1
            except Exception:
                default_bg = curses.COLOR_BLACK

            curses.init_pair(1, curses.COLOR_WHITE, curses.COLOR_BLUE)
            curses.init_pair(2, curses.COLOR_CYAN, default_bg)
            curses.init_pair(3, curses.COLOR_GREEN, default_bg)
            curses.init_pair(4, curses.COLOR_YELLOW, default_bg)
            curses.init_pair(5, curses.COLOR_RED, default_bg)
            curses.init_pair(6, curses.COLOR_WHITE, default_bg)
            curses.init_pair(8, curses.COLOR_MAGENTA, default_bg)
            self.has_color = True

    def attr(self, name):
        if not self.has_color:
            return curses.A_NORMAL

        attrs = {
            "title": curses.color_pair(1) | curses.A_BOLD,
            "section": curses.color_pair(2) | curses.A_BOLD,
            "good": curses.color_pair(3) | curses.A_BOLD,
            "warn": curses.color_pair(4) | curses.A_BOLD,
            "bad": curses.color_pair(5) | curses.A_BOLD,
            "value": curses.color_pair(6),
            "label": curses.A_DIM,
            "accent": curses.color_pair(8) | curses.A_BOLD,
            "normal": curses.A_NORMAL,
            "dim": curses.A_DIM,
        }
        return attrs.get(name, curses.A_NORMAL)

    def add(self, y, x, text, attr="normal"):
        max_y, max_x = self.stdscr.getmaxyx()
        if y < 0 or y >= max_y or x >= max_x:
            return
        available = max_x - x - 1
        if available <= 0:
            return
        try:
            self.stdscr.addnstr(y, x, short(text, available), available, self.attr(attr))
        except curses.error:
            pass

    def label_value(self, y, label, value, value_attr="value", x=0, label_width=25):
        self.add(y, x, f"{label}:", "label")
        self.add(y, x + label_width, value, value_attr)

    def section(self, y, title):
        self.add(y, 0, title, "section")

    def status_attr(self, state):
        if state is True:
            return "good"
        if state is False:
            return "bad"
        return "warn"

    def mode_attr(self, mode):
        if mode == "CHARGING":
            return "good"
        if mode == "DISCHARGING":
            return "bad"
        if mode.startswith("PLUGGED"):
            return "section"
        return "warn"

    def watts_attr(self, watts):
        if watts is None:
            return "warn"
        if watts > 5:
            return "good"
        if watts < -5:
            return "bad"
        return "warn"

    def temp_attr(self, temp_c):
        if temp_c is None:
            return "warn"
        if temp_c >= 45:
            return "bad"
        if temp_c >= 38:
            return "warn"
        return "good"

    def health_attr(self, health):
        if health is None:
            return "warn"
        if health >= 90:
            return "good"
        if health >= 80:
            return "warn"
        return "bad"

    def draw_bar(self, y, x, percent, width=27):
        if percent is None:
            self.add(y, x, "[" + "?" * width + "]", "warn")
            return

        percent = max(0.0, min(100.0, float(percent)))
        filled = int(round(width * percent / 100))
        empty = width - filled

        if percent >= 60:
            attr = "good"
        elif percent >= 25:
            attr = "warn"
        else:
            attr = "bad"

        self.add(y, x, "[", "value")
        self.add(y, x + 1, "█" * filled, attr)
        self.add(y, x + 1 + filled, "░" * empty, "dim")
        self.add(y, x + 1 + width, "]", "value")
        self.add(y, x + width + 4, fmt(percent, "%", 1), "value")

    def draw(self, row, args, logger):
        self.stdscr.erase()
        max_y, max_x = self.stdscr.getmaxyx()

        title = f" {APP_NAME} v{VERSION} "
        self.add(0, 0, title, "title")

        y = 2
        self.label_value(y, "Time", row["display_time"]); y += 1
        self.label_value(y, "Session", row["session_runtime"]); y += 1
        self.label_value(y, "Phase", row["phase"], "accent"); y += 1
        self.label_value(y, "Auto phase", row.get("auto_phase", "n/a"), "accent"); y += 1
        self.label_value(y, "Mode", row["mode"], self.mode_attr(row["mode"])); y += 1
        self.label_value(y, "Power source", safe_str(row["power_source"])); y += 2

        self.add(y, 0, "Battery:", "label")
        self.draw_bar(y, 25, row["battery_percent"]); y += 1

        temp = f"{fmt(row['battery_temp_c'], ' °C', 1)} / {fmt(row['battery_temp_f'], ' °F', 1)}"
        self.label_value(y, "Temperature", temp, self.temp_attr(row["battery_temp_c"])); y += 1
        self.label_value(y, "Temp avg/max/trend", f"{fmt(row['avg_temp_c'], ' °C', 1)} / {fmt(row['max_battery_temp_c'], ' °C', 1)} / {signed_fmt(row['temp_trend_c_per_min'], ' °C/min', 2)}"); y += 1
        self.label_value(y, "Thermal state", row.get("thermal_state", "n/a"), "warn" if row.get("possible_throttle") else "value"); y += 1
        self.label_value(y, "Net battery watts", fmt(row["net_battery_watts"], " W", 2), self.watts_attr(row["net_battery_watts"])); y += 1
        bms_line = f"BMS {fmt(row.get('bms_system_power_w'), ' W', 2)} / EffLoad {fmt(row.get('telemetry_system_effective_total_load_w'), ' W', 2)} / SoC {fmt(row.get('soc_power_w'), ' W', 2)}"
        self.label_value(y, "Internal power", bms_line); y += 1
        self.label_value(y, "Primary load", f"{fmt(row.get('primary_total_load_w'), ' W', 2)} via {safe_str(row.get('primary_total_load_source'))}", "accent"); y += 1
        self.label_value(y, "Power split", safe_str(row.get("adapter_power_split_note"))); y += 1
        stress_line = f"{fmt(row.get('stress_cpu_percent'), '%', 0)} CPU / {fmt(row.get('stress_rss_mb'), ' MB', 0)} RAM / {safe_str(row.get('stress_process_names'))}"
        stress_attr = "hot" if row.get("stress_workload_active") else "value"
        self.label_value(y, "Stress process load", stress_line, stress_attr); y += 1
        phase_line = f"avg {fmt(row.get('phase_current_net_avg_w'), ' W', 2)} / charge {fmt(row.get('phase_current_charge_peak_w'), ' W', 2)} / discharge {fmt(row.get('phase_current_discharge_peak_w'), ' W', 2)}"
        self.label_value(y, "Current phase W", phase_line); y += 1
        peak_line = f"charge {fmt(row['max_charge_watts'], ' W', 2)} / discharge {fmt(row['max_discharge_watts'], ' W', 2)}"
        self.label_value(y, "Session peaks", peak_line); y += 1

        if row["whole_mac_watts_estimate"] is not None:
            mac_line = f"{fmt(row['whole_mac_watts_estimate'], ' W', 2)}  avg {fmt(row['avg_mac_use_watts'], ' W', 2)}  max {fmt(row['max_mac_use_watts'], ' W', 2)}"
            self.label_value(y, "Mac use estimate", mac_line, "accent")
        else:
            self.label_value(y, "Mac use estimate", row["whole_mac_watts_note"], "warn")
        y += 1

        energy_line = f"charged {fmt(row['battery_wh_charged'], ' Wh', 2)} / discharged {fmt(row['battery_wh_discharged'], ' Wh', 2)} / net {signed_fmt(row['battery_wh_net'], ' Wh', 2)}"
        self.label_value(y, "Session energy", energy_line); y += 1

        drain_line = f"{signed_fmt(row['percent_rate_per_hour'], '%/hr', 1)}  runtime {row['rolling_runtime_estimate']}  full {row['rolling_time_to_full']}"
        self.label_value(y, "Rolling rate", drain_line); y += 1

        energy_est = f"{fmt(row['estimated_wh_remaining'], ' Wh', 2)} remaining / {fmt(row['estimated_wh_full'], ' Wh', 2)} full"
        self.label_value(y, "Energy estimate", energy_est); y += 1

        volts_amps = f"{fmt(row['battery_voltage_v'], ' V', 3)} / {fmt(row['battery_amperage_a'], ' A', 3)}"
        self.label_value(y, "Voltage / amps", volts_amps); y += 2

        self.section(y, "Charger / adapter"); y += 1
        self.label_value(y, "Adapter name", row["adapter_name"]); y += 1
        self.label_value(y, "Rated watts", f"{safe_str(row['adapter_reported_watts'])} W"); y += 1
        adapter_va = f"{fmt(row['adapter_voltage_v'], ' V', 2)} / {fmt(row['adapter_current_a'], ' A', 2)}"
        self.label_value(y, "Voltage / current", adapter_va); y += 1
        self.label_value(y, "Calculated watts", fmt(row["adapter_calculated_watts"], " W", 2)); y += 1
        self.label_value(y, "Load / headroom", f"{fmt(row['visible_load_estimate_w'], ' W', 2)} / {fmt(row['charger_headroom_estimate_w'], ' W', 2)}"); y += 1
        self.label_value(y, "Charger load", fmt(row.get("charger_load_percent"), "%", 1)); y += 1
        self.label_value(y, "Charger verdict", safe_str(row.get("charger_live_verdict")), "warn" if "near limit" in safe_str(row.get("charger_live_verdict")).lower() or "not keeping" in safe_str(row.get("charger_live_verdict")).lower() else "good"); y += 1
        self.label_value(y, "Best adapter index", safe_str(row.get("best_adapter_index"))); y += 2

        if args.powermetrics:
            self.section(y, "powermetrics estimate"); y += 1
            self.label_value(y, "Status", safe_str(row.get("pm_status"))); y += 1
            soc_line = f"now {fmt(row.get('soc_power_w'), ' W', 2)}  avg {fmt(row.get('avg_soc_power_w'), ' W', 2)}  max {fmt(row.get('max_soc_power_w'), ' W', 2)}"
            self.label_value(y, "SoC/component", soc_line, "accent"); y += 1
            cpu_gpu_line = f"CPU {fmt(row.get('cpu_power_w'), ' W', 2)} / GPU {fmt(row.get('gpu_power_w'), ' W', 2)} / ANE {fmt(row.get('ane_power_w'), ' W', 2)} / DRAM {fmt(row.get('dram_power_w'), ' W', 2)}"
            self.label_value(y, "Breakdown", cpu_gpu_line); y += 1
            cluster_line = safe_str(row.get("cpu_cluster_summary"))
            if cluster_line == "n/a":
                cluster_line = f"CPU est {fmt(row.get('cpu_frequency_estimate_mhz'), ' MHz', 0)}"
            self.label_value(y, "CPU clusters", cluster_line, "hot" if (row.get("p_cluster_active_percent") or 0) > 85 or (row.get("e_cluster_active_percent") or 0) > 85 else "value"); y += 1
            gpu_thermal_line = f"GPU {fmt(row.get('gpu_frequency_mhz'), ' MHz', 1)} / thermal {safe_str(row.get('thermal_summary'))} ({safe_str(row.get('thermal_pressure_source'))})"
            self.label_value(y, "GPU/thermal", gpu_thermal_line); y += 2

        if args.app_power:
            self.section(y, "Application power attribution (estimated)"); y += 1
            app_model = (
                f"{safe_str(row.get('app_power_status'))} / {safe_str(row.get('app_power_source'))} / "
                f"confidence {safe_str(row.get('app_power_confidence'))} / total {fmt(row.get('app_power_total_system_w'), ' W', 1)} / "
                f"baseline {fmt(row.get('app_power_baseline_w'), ' W', 1)} / "
                f"CPU pool {fmt(row.get('app_power_cpu_pool_w'), ' W', 1)} / GPU pool {fmt(row.get('app_power_gpu_pool_w'), ' W', 1)}"
            )
            self.label_value(y, "App power model", app_model, "accent"); y += 1
            for app_index in range(1, args.app_power_top + 1):
                prefix = f"app_top_{app_index}"
                app_name = row.get(f"{prefix}_name")
                if not app_name:
                    continue
                app_value = (
                    f"{safe_str(app_name)}  est {fmt(row.get(f'{prefix}_estimated_w'), ' W', 2)} / "
                    f"dyn {fmt(row.get(f'{prefix}_dynamic_w'), ' W', 2)} / "
                    f"CPU {fmt(row.get(f'{prefix}_cpu_w'), ' W', 1)} / GPU {fmt(row.get(f'{prefix}_gpu_w'), ' W', 1)} / "
                    f"share {fmt(row.get(f'{prefix}_share_percent'), '%', 1)} / EI {fmt(row.get(f'{prefix}_energy_impact'), '', 1)}"
                )
                category = safe_str(row.get(f"{prefix}_category"))
                app_attr = "accent" if category == "user_app" else ("warn" if category == "benchmark" else "value")
                self.label_value(y, f"Power app {app_index}", app_value, app_attr); y += 1
            if row.get("app_power_status") not in ("ok", "degraded"):
                note = row.get("app_power_error") or "Waiting for powermetrics task sample; ps fallback is automatic."
                self.label_value(y, "App power note", safe_str(note), "warn"); y += 1
            y += 1

        self.section(y, "Battery condition"); y += 1
        self.label_value(y, "Cycle count", safe_str(row["cycle_count"])); y += 1
        self.label_value(y, "Live health est", fmt(row["battery_health_percent"], "%", 1), self.health_attr(row["battery_health_percent"])); y += 1
        self.label_value(y, "Stable health est", f"{fmt(row.get('stable_health_percent'), '%', 1)}  {safe_str(row.get('stable_health_note'))}"); y += 1
        self.label_value(y, "Raw cap now/max", f"{row['apple_raw_current_capacity']} / {row['apple_raw_max_capacity']}"); y += 1
        self.label_value(y, "Cell disconnects", safe_str(row.get("battery_cell_disconnect_count"))); y += 1

        if args.full:
            y += 1
            self.section(y, "System Information cross-check"); y += 1
            self.label_value(y, "SP status", safe_str(row.get("sp_status"))); y += 1
            self.label_value(y, "SP adapter W", fmt(row.get("sp_ac_charger_watts"), " W", 0)); y += 1
            self.label_value(y, "SP charger", safe_str(row.get("sp_charger_name"))); y += 1
            self.label_value(y, "SP condition", safe_str(row.get("sp_condition"))); y += 1
            self.label_value(y, "SP Energy Mode", f"AC {safe_str(row.get('sp_ac_energy_mode'))} / Battery {safe_str(row.get('sp_battery_energy_mode'))}", "warn" if row.get("sp_power_mode_warning") else "value"); y += 1
            self.label_value(y, "SP raw modes", f"Low AC {safe_str(row.get('sp_ac_low_power_mode'))} / Low Batt {safe_str(row.get('sp_battery_low_power_mode'))} / High AC {safe_str(row.get('sp_ac_high_power_mode'))} / High Batt {safe_str(row.get('sp_battery_high_power_mode'))}"); y += 1
            y += 1
            self.section(y, "Cell / BMS health details"); y += 1
            self.label_value(y, "BMS model value", fmt(row.get("virtual_temp_c"), "", 1)); y += 1
            pt_line = f"load {fmt(row.get('telemetry_system_load_w'), ' W', 2)} / batt {fmt(row.get('telemetry_battery_power_w'), ' W', 2)}"
            self.label_value(y, "PT secondary est", pt_line); y += 1
            cell_line = f"{fmt(row.get('cell_voltage_min_mv'), ' mV', 0)} / {fmt(row.get('cell_voltage_max_mv'), ' mV', 0)} / delta {fmt(row.get('cell_voltage_delta_mv'), ' mV', 0)}"
            self.label_value(y, "Cell V min/max", cell_line); y += 1
            qmax_line = f"{fmt(row.get('qmax_min'), '', 0)} / {fmt(row.get('qmax_max'), '', 0)} / delta {fmt(row.get('qmax_delta'), '', 0)}"
            self.label_value(y, "Qmax min/max", qmax_line); y += 1
            ra_line = f"{fmt(row.get('weighted_ra_min'), '', 0)} / {fmt(row.get('weighted_ra_max'), '', 0)} / delta {fmt(row.get('weighted_ra_delta'), '', 0)}"
            self.label_value(y, "Weighted Ra", ra_line); y += 1
            life_line = f"max temp {fmt(row.get('lifetime_max_temp'), ' °C', 0)} / max disch {fmt(row.get('lifetime_max_discharge_current_ma'), ' mA', 0)}"
            self.label_value(y, "Lifetime", life_line); y += 1

        footer1 = "+ watts = battery charging, - watts = battery discharging."
        footer2 = "q or Ctrl+C = quit"
        if logger and logger.enabled:
            footer2 += f"    CSV: {os.path.basename(logger.csv_path)}"
        if row.get("possible_throttle"):
            footer1 = "Possible thermal/power throttling detected. " + footer1

        if max_y >= 4:
            self.add(max_y - 2, 0, footer1, "warn" if row.get("possible_throttle") else "dim")
            self.add(max_y - 1, 0, footer2, "dim")

        if y >= max_y - 2:
            self.add(max_y - 3, 0, "Terminal is too short. Enlarge window or run without --full.", "warn")

        self.stdscr.refresh()


# -------------------------
# Non-curses outputs
# -------------------------

def default_phase_file():
    return os.path.join(os.path.dirname(os.path.abspath(__file__)), "current_phase.txt")


def default_log_arg():
    return "auto"


def print_raw_keys(phase_file):
    row, _ = collect_reading(phase_file)
    print(f"{APP_NAME} v{VERSION} - AppleSmartBattery keys")
    print()
    for key in row["raw_keys"]:
        print(key)


def print_recursive_debug(phase_file):
    row, raw = collect_reading(phase_file)
    print(json.dumps(recursive_sanitize(raw), indent=2, ensure_ascii=False))


def print_once(phase_file):
    row, _ = collect_reading(phase_file)
    print(f"{APP_NAME} v{VERSION}")
    print(f"Time: {row['display_time']}")
    print(f"Phase: {row['phase']}")
    print(f"Mode: {row['mode']}")
    print(f"Power source: {row['power_source']}")
    print(f"Battery: {fmt(row['battery_percent'], '%', 1)}")
    print(f"Temperature: {fmt(row['battery_temp_c'], ' °C', 1)} / {fmt(row['battery_temp_f'], ' °F', 1)}")
    print(f"Virtual temp: {fmt(row['virtual_temp_c'], ' °C', 1)}")
    print(f"Net battery watts: {fmt(row['net_battery_watts'], ' W', 2)}")
    print(f"Internal BMS power: {fmt(row.get('bms_system_power_w'), ' W', 2)}")
    print(f"Preferred system power: {fmt(row.get('preferred_system_power_w'), ' W', 2)}")
    print(f"Charger verdict: {safe_str(row.get('charger_live_verdict'))}")
    print(f"Mac use estimate: {fmt(row['whole_mac_watts_estimate'], ' W', 2)} ({row['whole_mac_watts_note']})")
    print(f"Voltage / amps: {fmt(row['battery_voltage_v'], ' V', 3)} / {fmt(row['battery_amperage_a'], ' A', 3)}")
    print(f"Adapter rated watts: {safe_str(row['adapter_reported_watts'])} W")
    print(f"Live health estimate: {fmt(row['battery_health_percent'], '%', 1)}")


# -------------------------
# Main loop
# -------------------------

def curses_main(stdscr, args):
    logger = RunLogger(args.log, no_log=args.no_log, debug_every=args.debug_every, keep_raw_debug=not args.no_debug_json)
    ui = UI(stdscr, plain=args.plain)
    tracker = StatsTracker(window_seconds=args.rolling_window)
    baseline = BaselineTracker(seconds=args.baseline_seconds)
    detector = EventDetector(logger)
    phase_tracker = PhaseStatsTracker()
    health_tracker = StableHealthTracker()

    app_worker = None
    app_engine = None
    app_logger = None
    if args.app_power:
        app_worker = AppPowerWorker(
            interval_s=args.app_power_every,
            sample_ms=args.app_power_sample_ms,
            max_activities=args.app_power_max_activities,
            resolve_bundles=args.app_power_resolve_bundles,
        )
        app_worker.start()
        app_engine = AppPowerAttributionEngine(top_slots=APP_POWER_TOP_SLOTS, min_score=args.app_power_min_score)
        run_stem = os.path.splitext(os.path.basename(logger.csv_path))[0] if logger.csv_path else f"mac_power_{logger.run_id}"
        app_base_dir = logger.base_dir or os.path.join(os.path.dirname(os.path.abspath(__file__)), "logs")
        app_logger = AppPowerSessionLogger(
            app_base_dir,
            run_stem,
            enabled=logger.enabled,
            flush_every=args.app_power_summary_every,
        )

    pm_worker = None
    if args.powermetrics:
        pm_worker = PowermetricsWorker(interval=args.powermetrics_every, sample_ms=args.powermetrics_sample_ms)
        pm_worker.start()

    sp_worker = None
    if args.system_profiler:
        sp_worker = SystemProfilerWorker(interval=args.system_profiler_every)
        sp_worker.start()

    try:
        while True:
            try:
                pm_snapshot = pm_worker.snapshot() if pm_worker else None
                sp_snapshot = sp_worker.snapshot() if sp_worker else None
                row, raw_battery = collect_reading(args.phase_file, pm_snapshot, sp_snapshot)
                row = baseline.add(row)
                row = tracker.enrich(row)
                row = detector.process(row)
                row = phase_tracker.enrich(row)
                row = health_tracker.enrich(row)

                if app_worker and app_engine:
                    try:
                        app_result = app_engine.attribute(row, app_worker.snapshot())
                        row.update(app_result.to_row_fields(APP_POWER_TOP_SLOTS))
                        if app_logger:
                            app_logger.write(app_result, row)
                    except Exception as app_exc:
                        # Application attribution is auxiliary. A parser, sudo,
                        # compatibility, or disk error must never interrupt the
                        # primary battery/charger log.
                        row.update(empty_app_power_row_fields(APP_POWER_TOP_SLOTS))
                        row["app_power_status"] = "error"
                        row["app_power_error"] = safe_str(app_exc)
                        if logger:
                            logger.write_event(
                                {
                                    "type": "app_power_error",
                                    "details": {"error": safe_str(app_exc)},
                                }
                            )
                else:
                    row.update(empty_app_power_row_fields(APP_POWER_TOP_SLOTS))

                ui.draw(row, args, logger)

                logger.write_csv(row)
                logger.maybe_write_debug(row, raw_battery)

            except Exception as e:
                stdscr.erase()
                max_y, max_x = stdscr.getmaxyx()
                stdscr.addnstr(0, 0, f"{APP_NAME} v{VERSION}", max(10, max_x - 1))
                stdscr.addnstr(2, 0, f"ERROR: {str(e)}", max(10, max_x - 1))
                stdscr.addnstr(4, 0, "Press q or Ctrl+C to quit.", max(10, max_x - 1))
                stdscr.refresh()
                if logger:
                    logger.write_event({"type": "monitor_error", "details": {"error": str(e)}})

            end_time = time.time() + max(0.1, args.interval)
            while time.time() < end_time:
                try:
                    ch = stdscr.getch()
                    if ch in (ord("q"), ord("Q")):
                        return
                except Exception:
                    pass
                time.sleep(0.05)

    finally:
        if app_worker:
            try:
                app_worker.stop()
                app_worker.join(timeout=3.0)
            except Exception:
                pass
        if app_logger:
            try:
                app_logger.close()
            except Exception as app_close_exc:
                if logger:
                    try:
                        logger.write_event(
                            {
                                "type": "app_power_close_error",
                                "details": {"error": safe_str(app_close_exc)},
                            }
                        )
                    except Exception:
                        pass
        if pm_worker:
            try:
                pm_worker.stop()
            except Exception:
                pass
        if sp_worker:
            try:
                sp_worker.stop()
            except Exception:
                pass
        if logger:
            try:
                logger.write_event({"type": "run_stopped", "details": {}})
            finally:
                logger.close()


def main():
    parser = argparse.ArgumentParser(description=f"{APP_NAME} v{VERSION}")
    parser.add_argument("--version", action="version", version=f"{APP_NAME} v{VERSION}")
    parser.add_argument("--interval", type=float, default=0.5, help="Refresh interval in seconds. Default: 0.5")
    parser.add_argument("--rolling-window", type=float, default=60.0, help="Rolling stats window in seconds. Default: 60")
    parser.add_argument("--baseline-seconds", type=float, default=45.0, help="Initial baseline capture seconds. Default: 45")
    parser.add_argument("--log", default=default_log_arg(), help="CSV log path or 'auto'. Default: auto")
    parser.add_argument("--no-log", action="store_true", help="Disable CSV/events/debug logging")
    parser.add_argument("--no-debug-json", action="store_true", help="Disable debug JSON snapshots")
    parser.add_argument("--debug-every", type=int, default=30, help="Write raw debug snapshot every N samples. Default: 30")
    parser.add_argument("--phase-file", default=default_phase_file(), help="Phase marker file path")
    parser.add_argument("--plain", action="store_true", help="Disable colour")
    parser.add_argument("--full", action="store_true", help="Show extra details")
    parser.add_argument("--once", action="store_true", help="Print one reading and exit")
    parser.add_argument("--show-raw-keys", action="store_true", help="Show available AppleSmartBattery keys once and exit")
    parser.add_argument("--dump-raw-json", action="store_true", help="Dump raw AppleSmartBattery JSON once and exit")
    parser.add_argument("--powermetrics", action="store_true", help="Enable optional CPU/GPU/ANE/DRAM power estimates using sudo powermetrics")
    parser.add_argument("--powermetrics-every", type=float, default=5.0, help="Seconds between powermetrics refreshes. Default: 5")
    parser.add_argument("--powermetrics-sample-ms", type=int, default=1000, help="powermetrics sample window in ms. Default: 1000")
    app_group = parser.add_mutually_exclusive_group()
    app_group.add_argument("--app-power", dest="app_power", action="store_true", help="Enable per-application Energy Impact and estimated watts attribution")
    app_group.add_argument("--no-app-power", dest="app_power", action="store_false", help="Disable per-application power attribution")
    parser.set_defaults(app_power=None)
    parser.add_argument("--app-power-every", type=float, default=10.0, help="Seconds between app Energy Impact samples. Range: 2-300. Default: 10")
    parser.add_argument("--app-power-sample-ms", type=int, default=1000, help="App powermetrics sample window in ms. Range: 250-60000. Default: 1000")
    parser.add_argument("--app-power-top", type=int, default=3, help="Top apps shown in the live UI. Range: 1-5. Default: 3")
    parser.add_argument("--app-power-max-activities", type=int, default=160, help="Maximum app/process records retained per sample. Range: 10-1000")
    parser.add_argument("--app-power-min-score", type=float, default=0.001, help="Minimum positive Energy Impact/activity score. Default: 0.001")
    parser.add_argument("--app-power-summary-every", type=int, default=3, help="Flush incremental app summary every N app samples. Range: 1-120")
    bundle_group = parser.add_mutually_exclusive_group()
    bundle_group.add_argument("--app-power-resolve-bundles", dest="app_power_resolve_bundles", action="store_true", help="Resolve bundle identifiers to app names using Spotlight")
    bundle_group.add_argument("--no-app-power-resolve-bundles", dest="app_power_resolve_bundles", action="store_false", help="Disable Spotlight bundle-name resolution")
    parser.set_defaults(app_power_resolve_bundles=True)
    parser.add_argument("--system-profiler", action="store_true", help="Enable slow System Information power cross-check")
    parser.add_argument("--system-profiler-every", type=float, default=60.0, help="Seconds between system_profiler checks. Default: 60")
    args = parser.parse_args()

    if args.app_power is None:
        args.app_power = bool(args.powermetrics)
    if not (2.0 <= args.app_power_every <= 300.0):
        parser.error("--app-power-every must be between 2 and 300 seconds")
    if not (250 <= args.app_power_sample_ms <= 60000):
        parser.error("--app-power-sample-ms must be between 250 and 60000")
    if not (1 <= args.app_power_top <= APP_POWER_TOP_SLOTS):
        parser.error(f"--app-power-top must be between 1 and {APP_POWER_TOP_SLOTS}")
    if not (10 <= args.app_power_max_activities <= 1000):
        parser.error("--app-power-max-activities must be between 10 and 1000")
    if not (0.0 <= args.app_power_min_score <= 1000000.0):
        parser.error("--app-power-min-score must be between 0 and 1000000")
    if not (1 <= args.app_power_summary_every <= 120):
        parser.error("--app-power-summary-every must be between 1 and 120")

    if args.show_raw_keys:
        print_raw_keys(args.phase_file)
        return

    if args.dump_raw_json:
        print_recursive_debug(args.phase_file)
        return

    if args.once:
        print_once(args.phase_file)
        return

    curses.wrapper(curses_main, args)


if __name__ == "__main__":
    main()
