#!/usr/bin/env python3
"""
MacPowerLab System Sensor Snapshot Runner v0.7.1

Timeout-safe replacement for the old shell snapshot. No command should hang the menu.
"""

import json
import subprocess
import time
from datetime import datetime
from pathlib import Path
import argparse

VERSION = "0.9.0"


def run_cmd(cmd, out_path, timeout):
    rec = {
        "command": cmd,
        "output": str(out_path),
        "timeout_seconds": timeout,
        "start_time": datetime.now().isoformat(timespec="seconds"),
        "status": None,
        "elapsed_seconds": None,
        "exit_code": None,
    }
    start = time.time()
    try:
        with out_path.open("w", encoding="utf-8", errors="replace") as f:
            proc = subprocess.run(cmd, stdout=f, stderr=subprocess.STDOUT, text=True, timeout=timeout)
        rec["exit_code"] = proc.returncode
        rec["status"] = "complete"
    except subprocess.TimeoutExpired:
        with out_path.open("a", encoding="utf-8", errors="replace") as f:
            f.write("\n\n--- TIMEOUT ---\n")
            f.write(f"Command exceeded {timeout} seconds and was stopped.\n")
        rec["status"] = "timeout"
    except Exception as e:
        with out_path.open("a", encoding="utf-8", errors="replace") as f:
            f.write("\n\n--- ERROR ---\n")
            f.write(str(e) + "\n")
        rec["status"] = "error"
    rec["elapsed_seconds"] = round(time.time() - start, 3)
    return rec


def main():
    parser = argparse.ArgumentParser(description=f"MacPowerLab System Sensor Snapshot v{VERSION}")
    parser.add_argument("--timeout", type=int, default=12, help="Timeout per command. Default: 12 seconds.")
    args = parser.parse_args()

    root = Path(__file__).resolve().parent
    stamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    out = root / "logs" / f"system_sensor_snapshot_{stamp}"
    out.mkdir(parents=True, exist_ok=True)

    print("MacPowerLab system sensor snapshot")
    print(f"Output folder: {out.relative_to(root)}")
    print(f"Timeout per command: {args.timeout}s")
    print()

    commands = [
        ("sw_vers.txt", ["sw_vers"], 6),
        ("uname.txt", ["uname", "-a"], 6),
        ("uptime.txt", ["uptime"], 6),
        ("vm_stat.txt", ["vm_stat"], 6),
        ("df_root.txt", ["df", "-h", "/"], 6),
        ("pmset_batt.txt", ["pmset", "-g", "batt"], 6),
        ("pmset_thermlog.txt", ["pmset", "-g", "thermlog"], args.timeout),
        ("top.txt", ["top", "-l", "1", "-n", "20"], args.timeout),
        ("top_processes.txt", ["ps", "-arcwwwxo", "pid,pcpu,pmem,command"], args.timeout),
        ("system_profiler_hardware.json", ["system_profiler", "SPHardwareDataType", "-json"], args.timeout),
        ("system_profiler_memory.json", ["system_profiler", "SPMemoryDataType", "-json"], args.timeout),
        ("system_profiler_power.json", ["system_profiler", "SPPowerDataType", "-json"], args.timeout),
        ("apple_smart_battery.plist", ["ioreg", "-r", "-c", "AppleSmartBattery", "-a"], args.timeout),
    ]

    print("Checking sudo for powermetrics...")
    subprocess.run(["sudo", "-v"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

    commands.extend([
        ("powermetrics_sensors.txt", ["sudo", "powermetrics", "-n", "1", "-i", "1000", "--samplers", "cpu_power,gpu_power,thermal"], args.timeout),
        ("powermetrics_raw.txt", ["sudo", "powermetrics", "-n", "1", "-i", "1000"], args.timeout),
    ])

    records = []
    for filename, cmd, timeout in commands:
        print(f"Capturing: {filename}")
        records.append(run_cmd(cmd, out / filename, timeout))

    (out / "snapshot_manifest.json").write_text(json.dumps({
        "created_at": datetime.now().isoformat(timespec="seconds"),
        "tool_version": VERSION,
        "records": records,
    }, indent=2), encoding="utf-8")

    analyzer = root / "analyze_system_sensor_snapshot.py"
    if analyzer.exists():
        subprocess.run([str(analyzer), str(out)], cwd=str(root))

    print()
    print("System sensor snapshot complete.")
    print(f"Folder: {out.relative_to(root)}")
    print("Pack logs with:")
    print("  ./pack_logs.sh")


if __name__ == "__main__":
    main()
