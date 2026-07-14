#!/usr/bin/env python3
"""
MacPowerLab battery score history repair v0.8.6

Use this after upgrading from v0.7.8-v0.8.6 if old scorecards show an
overall score around 25/100 despite excellent component scores.

It rebuilds score history from all test_run_power_summary_*.json files using the
current fixed generate_battery_scorecard.py logic.
"""

import importlib.util
import json
import shutil
from datetime import datetime
from pathlib import Path

VERSION = "0.9.0"

def load_score_module():
    path = Path(__file__).resolve().parent / "generate_battery_scorecard.py"
    spec = importlib.util.spec_from_file_location("mpl_scorecard", path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod

def main():
    root = Path(__file__).resolve().parent
    logs = root / "logs"
    summaries = sorted(logs.glob("test_run_power_summary_*.json"), key=lambda p: p.stat().st_mtime)

    if not summaries:
        print("No logs/test_run_power_summary_*.json files found.")
        raise SystemExit(1)

    mod = load_score_module()

    hist = logs / "battery_benchmark_scores.jsonl"
    if hist.exists():
        backup = logs / f"battery_benchmark_scores_backup_before_v084_repair_{datetime.now().strftime('%Y%m%d_%H%M%S')}.jsonl"
        shutil.copy2(hist, backup)
        print(f"Backed up old history: {backup}")

    repaired = []
    for src in summaries:
        summary = json.loads(src.read_text(encoding="utf-8", errors="replace"))
        card = mod.compute_scorecard(summary)
        card["repaired_from_summary"] = str(src)
        card["repair_tool_version"] = VERSION
        repaired.append(card)

    hist.write_text("\\n".join(json.dumps(row) for row in repaired) + "\\n", encoding="utf-8")
    print(f"Rebuilt score history: {hist}")
    print(f"Entries: {len(repaired)}")
    print("Run ./generate_battery_scorecard.sh after new benchmarks to append future scores.")

if __name__ == "__main__":
    main()
