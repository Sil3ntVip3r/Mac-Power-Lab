#!/usr/bin/env python3
"""Apply the focused active-legacy one-shot lifecycle patch."""

from __future__ import annotations

import hashlib
from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    if text.count(old) != 1:
        raise SystemExit(f"{label} target was not unique")
    return text.replace(old, new)


source = Path("legacy/mac_power_watch.py")
text = source.read_text()

text = replace_once(
    text,
    "import subprocess\nimport threading\n",
    "import subprocess\nimport sys\nimport threading\n",
    "import insertion",
)

text = replace_once(
    text,
    '''def default_log_arg():
    return "auto"


def print_raw_keys(phase_file):
''',
    '''def default_log_arg():
    return "auto"


def option_supplied(arguments, option):
    return any(argument == option or argument.startswith(option + "=") for argument in arguments)


def collect_powermetrics_once(sample_ms=1000):
    worker = PowermetricsWorker(sample_ms=sample_ms)
    worker._run_once()
    return worker.snapshot()


def print_raw_keys(phase_file):
''',
    "one-shot helper insertion",
)

text = replace_once(
    text,
    '''def print_once(phase_file):
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
''',
    '''def print_once(phase_file, pm_snapshot=None):
    row, raw = collect_reading(phase_file, pm_snapshot)
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
    return row, raw


def run_once(args, log_requested=False):
    pm_snapshot = collect_powermetrics_once(args.powermetrics_sample_ms) if args.powermetrics else None
    row, raw = print_once(args.phase_file, pm_snapshot)
    if not log_requested or args.no_log:
        return row

    logger = RunLogger(
        args.log,
        no_log=False,
        debug_every=args.debug_every,
        keep_raw_debug=not args.no_debug_json,
    )
    try:
        logger.write_csv(row)
        logger.maybe_write_debug(row, raw)
    finally:
        logger.close()
    return row
''',
    "print_once replacement",
)

text = replace_once(
    text,
    '''def main():
    parser = argparse.ArgumentParser(description=f"{APP_NAME} v{VERSION}")
''',
    '''def main(argv=None):
    raw_args = list(sys.argv[1:] if argv is None else argv)
    parser = argparse.ArgumentParser(description=f"{APP_NAME} v{VERSION}")
''',
    "main signature",
)

text = replace_once(
    text,
    "    args = parser.parse_args()\n",
    "    args = parser.parse_args(raw_args)\n",
    "argument parsing",
)

text = replace_once(
    text,
    '''    if args.once:
        print_once(args.phase_file)
        return
''',
    '''    if args.once:
        run_once(args, log_requested=option_supplied(raw_args, "--log"))
        return
''',
    "one-shot dispatch",
)

source.write_text(text)

manifest = Path("legacy/MANIFEST_SHA256.txt")
entries: dict[str, str] = {}
for line in manifest.read_text().splitlines():
    digest, relative = line.split("  ", 1)
    entries[relative] = digest

for relative in ("mac_power_watch.py", "tests/test_once_logging.py"):
    entries[relative] = hashlib.sha256((Path("legacy") / relative).read_bytes()).hexdigest()

manifest.write_text("".join(f"{entries[path]}  {path}\n" for path in sorted(entries)))
