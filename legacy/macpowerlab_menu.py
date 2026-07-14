#!/usr/bin/env python3
"""
MacPowerLab user-friendly menu v0.6.2
"""

import subprocess
import sys
from pathlib import Path

VERSION = "0.9.0"


def run(cmd):
    print()
    print("Running:")
    print("  " + " ".join(cmd))
    print()
    return subprocess.call(cmd)


def ask_choice(prompt, choices, default=None):
    while True:
        print(prompt)
        for key, desc in choices:
            print(f"  {key}) {desc}")
        if default:
            ans = input(f"Choice [{default}]: ").strip() or default
        else:
            ans = input("Choice: ").strip()
        if ans.lower() in ("q", "quit", "exit"):
            return "q"
        if ans.lower() in ("b", "back"):
            return "b"
        valid = {key for key, _ in choices}
        if ans in valid:
            return ans
        print("Invalid choice. Enter one of the listed options, b for back, or q to quit.")
        print()


def ask_duration(default=300):
    presets = [
        ("1", "60 seconds quick check"),
        ("2", "120 seconds short test"),
        ("3", "300 seconds standard test"),
        ("4", "600 seconds long test"),
        ("5", "900 seconds extended test"),
        ("6", "custom seconds"),
        ("b", "back"),
        ("q", "quit"),
    ]
    while True:
        print()
        print("How long should this test run?")
        choice = ask_choice("", presets, "3")
        if choice in ("q", "b"):
            return choice
        if choice == "1":
            return "60"
        if choice == "2":
            return "120"
        if choice == "3":
            return "300"
        if choice == "4":
            return "600"
        if choice == "5":
            return "900"
        if choice == "6":
            while True:
                v = input(f"Custom duration seconds [{default}]: ").strip() or str(default)
                if v.lower() in ("q", "quit", "exit"):
                    return "q"
                if v.lower() in ("b", "back"):
                    return "b"
                if v.isdigit() and int(v) > 0:
                    return v
                print("Enter a positive number, b for back, or q to quit.")


def after_test():
    print()
    ans = input("Generate/open latest report now? [Y/n]: ").strip().lower()
    if ans in ("", "y", "yes"):
        run(["./generate_report.sh"])
        run(["./open_latest_report.sh"])

    print()
    ans = input("Pack logs for upload/send now? [Y/n]: ").strip().lower()
    if ans in ("", "y", "yes"):
        run(["./pack_logs.sh"])


def main():
    root = Path(__file__).resolve().parent
    try:
        import os
        os.chdir(root)
    except Exception:
        pass

    while True:
        print("\033[2J\033[H", end="")
        print(f"MacPowerLab v{VERSION}")
        print("=" * 18)
        print()
        print("Power-first Mac system monitor")
        print()
        choice = ask_choice(
            "Choose an action:",
            [
                ("1", "Open Power Monitor in a new Terminal window"),
                ("2", "Best max power test — CPU + GPU + memory"),
                ("3", "Extreme max power test — CPU + extreme GPU + memory"),
                ("4", "CPU-only stress test"),
                ("5", "GPU-only stress test"),
                ("6", "Memory bandwidth stress test"),
                ("7", "System sensor snapshot"),
                ("8", "Deep sensor probe"),
                ("9", "macOS sensor scan"),
                ("10", "Generate/open latest report"),
                ("11", "Pack logs for upload"),
                ("12", "List logs/reports/exports"),
                ("13", "List test runs"),
                ("14", "CPU sanity check"),
                ("15", "Battery benchmark suite"),
                ("16", "One-shot app Energy Impact sample"),
                ("17", "Generate application power report"),
                ("18", "Battery discharge benchmark"),
                ("19", "AC adapter/charging benchmark"),
                ("20", "Extreme soak benchmark"),
                ("0", "Exit"),
            ],
        )

        if choice in ("q", "0"):
            return 0
        if choice == "b":
            continue

        if choice in ("2", "3", "4", "5", "6"):
            duration = ask_duration()
            if duration == "q":
                return 0
            if duration == "b":
                continue

        if choice == "1":
            run(["./open_power_monitor_window.sh"])
            input("\nPower Monitor should now be open in a new Terminal window. Press Enter to continue...")
        elif choice == "2":
            run(["./run_max_power_test_pretty.sh", duration])
            after_test()
        elif choice == "3":
            run(["./run_max_power_extreme_pretty.sh", duration])
            after_test()
        elif choice == "4":
            run(["./run_cpu_stress_pretty.sh", duration])
            after_test()
        elif choice == "5":
            profile = ask_choice(
                "Choose GPU profile:",
                [("1", "normal"), ("2", "high"), ("3", "extreme"), ("b", "back"), ("q", "quit")],
                "2",
            )
            if profile == "q":
                return 0
            if profile == "b":
                continue
            profile_name = {"1": "normal", "2": "high", "3": "extreme"}[profile]
            run(["./run_gpu_stress_pretty.sh", duration, profile_name])
            after_test()
        elif choice == "6":
            run(["./run_memory_stress_pretty.sh", duration])
            after_test()
        elif choice == "7":
            run(["./run_system_sensor_snapshot.sh"])
            input("\nPress Enter to continue...")
        elif choice == "8":
            run(["./run_deep_sensor_probe.sh"])
            input("\nPress Enter to continue...")
        elif choice == "9":
            run(["./run_macos_sensor_scan.sh"])
            input("\nPress Enter to continue...")
        elif choice == "10":
            run(["./generate_report.sh"])
            run(["./open_latest_report.sh"])
            input("\nPress Enter to continue...")
        elif choice == "11":
            run(["./pack_logs.sh"])
            input("\nPress Enter to continue...")
        elif choice == "12":
            run(["./list_logs.sh"])
            input("\nPress Enter to continue...")
        elif choice == "13":
            run(["./list_test_runs.sh"])
            input("\nPress Enter to continue...")
        elif choice == "14":
            duration = ask_duration(20)
            if duration == "q":
                return 0
            if duration == "b":
                continue
            run(["./run_cpu_sanity_check.sh", duration])
            input("\nPress Enter to continue...")
        elif choice == "15":
            run(["./run_battery_benchmark.sh"])
            input("\nPress Enter to continue...")
        elif choice == "16":
            run(["./run_app_power_watch.sh", "--top", "15"])
            input("\nPress Enter to continue...")
        elif choice == "17":
            run(["./generate_app_power_report.sh"])
            input("\nPress Enter to continue...")
        elif choice == "18":
            run(["./run_battery_discharge_benchmark.sh"])
            input("\nPress Enter to continue...")
        elif choice == "19":
            run(["./run_ac_adapter_benchmark.sh"])
            input("\nPress Enter to continue...")
        elif choice == "20":
            duration = ask_duration(900)
            if duration == "q":
                return 0
            if duration == "b":
                continue
            run(["./run_extreme_soak_benchmark.sh", duration])
            input("\nPress Enter to continue...")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
