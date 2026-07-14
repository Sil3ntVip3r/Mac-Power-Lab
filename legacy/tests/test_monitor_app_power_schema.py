#!/usr/bin/env python3
"""Schema integration tests between app attribution and the main monitor CSV."""

from __future__ import annotations

import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from app_power_attribution import (  # noqa: E402
    ActivitySnapshot,
    AppActivity,
    AppPowerAttributionEngine,
    empty_app_power_row_fields,
)
from mac_power_watch import CSV_HEADERS  # noqa: E402


class MonitorAppPowerSchemaTests(unittest.TestCase):
    """Prevent app-power row fields from silently disappearing from CSV logs."""

    def test_empty_fields_are_in_csv_headers(self) -> None:
        missing = set(empty_app_power_row_fields().keys()) - set(CSV_HEADERS)
        self.assertEqual(missing, set())

    def test_attributed_fields_are_in_csv_headers(self) -> None:
        snapshot = ActivitySnapshot(
            sample_id=1,
            timestamp="2026-07-13T00:00:00",
            monotonic_time=1.0,
            sample_window_s=1.0,
            source="powermetrics-coalition",
            status="ok",
            activities=[
                AppActivity(
                    key="coalition:com.apple.Safari",
                    raw_name="com.apple.Safari",
                    display_name="Safari",
                    category="user_app",
                    energy_impact=10.0,
                    cpu_ms_per_s=100.0,
                    gpu_ms_per_s=10.0,
                    score=10.0,
                    source="powermetrics-coalition",
                )
            ],
        )
        result = AppPowerAttributionEngine(top_slots=5).attribute(
            {
                "primary_total_load_w": 50.0,
                "primary_total_load_source": "battery discharge watts",
                "power_source": "Battery Power",
                "phase": "test",
                "cpu_power_w": 15.0,
                "gpu_power_w": 5.0,
            },
            snapshot,
        )
        missing = set(result.to_row_fields().keys()) - set(CSV_HEADERS)
        self.assertEqual(missing, set())


if __name__ == "__main__":
    unittest.main()
