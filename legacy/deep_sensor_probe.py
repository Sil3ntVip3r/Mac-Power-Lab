#!/usr/bin/env python3
"""
MacPowerLab Deep Sensor Probe v0.7.1

Runs powermetrics sampler probes with per-command timeouts so the menu cannot hang.
"""

import argparse
import json
import subprocess
import time
from datetime import datetime
from pathlib import Path

VERSION = "0.9.0"


def run_cmd(cmd, out_path, timeout):
    start = time.time()
    rec = {
        "command": cmd,
        "output": str(out_path),
        "timeout_seconds": timeout,
        "start_time": datetime.now().isoformat(timespec="seconds"),
        "status": None,
        "elapsed_seconds": None,
        "exit_code": None,
    }
    try:
        with out_path.open("w", encoding="utf-8", errors="replace") as f:
            proc = subprocess.run(cmd, stdout=f, stderr=subprocess.STDOUT, text=True, timeout=timeout)
        rec["exit_code"] = proc.returncode
        rec["status"] = "complete"
    except subprocess.TimeoutExpired as e:
        with out_path.open("a", encoding="utf-8", errors="replace") as f:
            f.write("\n\n--- TIMEOUT ---\n")
            f.write(f"Command exceeded {timeout} seconds and was stopped.\n")
        rec["exit_code"] = None
        rec["status"] = "timeout"
    except Exception as e:
        with out_path.open("a", encoding="utf-8", errors="replace") as f:
            f.write("\n\n--- ERROR ---\n")
            f.write(str(e) + "\n")
        rec["status"] = "error"
    rec["elapsed_seconds"] = round(time.time() - start, 3)
    return rec


def safe_name(s):
    return s.replace(",", "_").replace("/", "_").replace(" ", "_")


def main():
    parser = argparse.ArgumentParser(description=f"MacPowerLab Deep Sensor Probe v{VERSION}")
    parser.add_argument("--timeout", type=int, default=12, help="Timeout per probe command. Default: 12 seconds")
    args = parser.parse_args()

    root = Path(__file__).resolve().parent
    stamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    out = root / "logs" / f"deep_sensor_probe_{stamp}"
    out.mkdir(parents=True, exist_ok=True)

    print("MacPowerLab deep sensor probe")
    print(f"Output folder: {out.relative_to(root)}")
    print(f"Timeout per command: {args.timeout}s")
    print()

    commands = [
        ("sw_vers", ["sw_vers"], 6),
        ("pmset_thermlog", ["pmset", "-g", "thermlog"], args.timeout),
        ("powermetrics_help", ["powermetrics", "--help"], args.timeout),
    ]

    samplers = [
        "smc",
        "thermal",
        "cpu_power",
        "gpu_power",
        "ane_power",
        "battery",
        "tasks",
        "disk",
        "network",
        "interrupts",
        "smc,cpu_power,gpu_power,thermal",
    ]

    print("Checking sudo for powermetrics...")
    subprocess.run(["sudo", "-v"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

    for sampler in samplers:
        commands.append((f"powermetrics_{safe_name(sampler)}", ["sudo", "powermetrics", "-n", "1", "-i", "1000", "--samplers", sampler], args.timeout))

    commands.extend([
        ("apple_smart_battery_plist", ["ioreg", "-r", "-c", "AppleSmartBattery", "-a"], args.timeout),
        ("ioreg_sensor_keyword_scan", ["bash", "-lc", "ioreg -l -w0 | grep -Ei 'fan|temp|thermal|rpm|smc|power|battery' | head -n 500"], args.timeout),
    ])

    records = []
    for name, cmd, timeout in commands:
        print(f"Testing: {name}")
        records.append(run_cmd(cmd, out / f"{name}.txt", timeout))

    manifest = {
        "created_at": datetime.now().isoformat(timespec="seconds"),
        "tool_version": VERSION,
        "timeout_seconds_default": args.timeout,
        "records": records,
    }
    (out / "deep_sensor_probe_manifest.json").write_text(json.dumps(manifest, indent=2), encoding="utf-8")
    (out / "README.txt").write_text(
        "MacPowerLab Deep Sensor Probe\n\n"
        "This folder records which macOS powermetrics samplers and raw sensor keywords are available.\n"
        "Timeout entries are useful: they show which probes are slow or unsupported on this Mac/macOS build.\n",
        encoding="utf-8",
    )

    print()
    print("Deep sensor probe complete.")
    print(f"Folder: {out.relative_to(root)}")
    print("Pack logs with:")
    print("  ./pack_logs.sh")


if __name__ == "__main__":
    main()
