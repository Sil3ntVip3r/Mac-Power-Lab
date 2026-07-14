#!/usr/bin/env python3
import json
from pathlib import Path

path = Path("logs/test_runs.jsonl")
if not path.exists():
    print("No test run log found yet.")
    raise SystemExit(0)

rows = []
for line in path.read_text(encoding="utf-8").splitlines():
    try:
        rows.append(json.loads(line))
    except Exception:
        pass

print("MacPowerLab Test Runs")
print("=====================")
print()
print("Time | Test | Duration | Status | Power log")
print("--- | --- | --- | --- | ---")
for r in rows[-30:]:
    print(" | ".join([
        str(r.get("start_time", "n/a")),
        str(r.get("title", "n/a")),
        str(r.get("elapsed_seconds", "n/a")),
        str(r.get("status", "n/a")),
        str(r.get("nearest_power_log", "n/a")),
    ]))
