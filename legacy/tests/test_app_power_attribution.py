#!/usr/bin/env python3
"""Unit tests for MacPowerLab app-power attribution."""

from __future__ import annotations

import json
import plistlib
import tempfile
import unittest
from pathlib import Path

import sys

ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from app_power_attribution import (  # noqa: E402
    ActivitySnapshot,
    AppActivity,
    AppPowerAttributionEngine,
    AppPowerSessionLogger,
    BundleNameResolver,
    PowermetricsTaskSampler,
)


class PowermetricsParserTests(unittest.TestCase):
    """Validate coalition/task plist parsing and power allocation."""

    def setUp(self) -> None:
        self.sampler = PowermetricsTaskSampler(
            sample_ms=1000,
            resolver=BundleNameResolver(enable_spotlight=False),
        )

    def test_decode_nul_separated_plist(self) -> None:
        sample = {
            "elapsed_ns": 1_000_000_000,
            "coalitions": [
                {
                    "name": "com.google.Chrome",
                    "energy_impact": 80.0,
                    "cputime_sample_ms_per_s": 500.0,
                    "gputime_ms": 50.0,
                    "diskio_bytesread": 1_048_576,
                    "bytes_received": 524_288,
                },
                {
                    "name": "com.apple.WindowServer",
                    "energy_impact": 20.0,
                    "cputime_sample_ms_per_s": 100.0,
                },
            ],
        }
        data = plistlib.dumps(sample) + b"\x00"
        decoded = self.sampler._decode_plists(data)
        self.assertEqual(len(decoded), 1)
        self.assertEqual(decoded[0]["elapsed_ns"], 1_000_000_000)

    def test_apple_foreground_app_is_not_classified_as_system(self) -> None:
        """Apple bundle IDs can represent foreground apps as well as services."""

        activity = self.sampler._activity_from_record(
            {"name": "com.apple.Safari", "energy_impact": 10.0},
            "powermetrics-coalition",
            1.0,
            resolve_name=True,
        )
        self.assertIsNotNone(activity)
        assert activity is not None
        self.assertEqual(activity.display_name, "Safari")
        self.assertEqual(activity.category, "user_app")

    def test_coalition_record_parsing(self) -> None:
        record = {
            "name": "com.google.Chrome",
            "energy_impact": 80.0,
            "cputime_sample_ms_per_s": 500.0,
            "gputime_ms": 50.0,
            "diskio_bytesread": 1_048_576,
            "bytes_received": 524_288,
        }
        activity = self.sampler._activity_from_record(
            record,
            "powermetrics-coalition",
            1.0,
            resolve_name=True,
        )
        self.assertIsNotNone(activity)
        assert activity is not None
        self.assertEqual(activity.display_name, "Google Chrome")
        self.assertEqual(activity.category, "user_app")
        self.assertAlmostEqual(activity.gpu_ms_per_s or 0.0, 50.0)
        self.assertAlmostEqual(activity.disk_read_bytes_per_s or 0.0, 1_048_576.0)
        self.assertAlmostEqual(activity.network_rx_bytes_per_s or 0.0, 524_288.0)
        self.assertAlmostEqual(activity.score, 80.0)


    def test_all_tasks_aggregate_is_ignored(self) -> None:
        """Aggregate ALL_TASKS rows must not double-count individual activity."""

        activity = self.sampler._activity_from_record(
            {"name": "ALL_TASKS", "energy_impact": 999.0},
            "powermetrics-task",
            1.0,
            resolve_name=False,
        )
        self.assertIsNone(activity)

    def test_responsible_pid_groups_helper_under_foreground_app(self) -> None:
        """Task fallback should bill a helper to its macOS responsible app PID."""

        activity = self.sampler._activity_from_record(
            {
                "pid": 202,
                "name": "com.apple.WebKit.WebContent",
                "responsible_pid": 101,
                "_responsible_name": "com.apple.Safari",
                "energy_impact": 20.0,
            },
            "powermetrics-task",
            1.0,
            resolve_name=True,
        )
        self.assertIsNotNone(activity)
        assert activity is not None
        self.assertEqual(activity.key, "responsible:101")
        self.assertEqual(activity.responsible_pid, 101)
        self.assertEqual(activity.display_name, "Safari")
        self.assertEqual(activity.category, "user_app")
        self.assertEqual(activity.extra_metrics.get("sample_process_name"), "com.apple.WebKit.WebContent")

    def test_current_task_schema_parsing(self) -> None:
        """Parse the current plist task keys used by recent powermetrics builds."""

        record = {
            "pid": 4242,
            "name": "Safari",
            "energy_impact": 12.5,
            "energy_impact_per_s": 10.0,
            "cputime_ms_per_s": 225.0,
            "cputime_userland_ratio": 0.85,
            "intr_wakeups_per_s": 8.0,
            "idle_wakeups_per_s": 2.0,
            "diskio_bytesread_per_s": 4096.0,
            "diskio_byteswritten_per_s": 2048.0,
            "bytes_received": 1000,
            "bytes_sent": 500,
        }
        activity = self.sampler._activity_from_record(
            record,
            "powermetrics-task",
            1.0,
            resolve_name=True,
        )
        self.assertIsNotNone(activity)
        assert activity is not None
        self.assertEqual(activity.pid, 4242)
        self.assertAlmostEqual(activity.score, 10.0)
        self.assertAlmostEqual(activity.cpu_ms_per_s or 0.0, 225.0)
        self.assertAlmostEqual(activity.network_rx_bytes_per_s or 0.0, 1000.0)

    def test_same_activity_snapshot_recalculates_live_watts(self) -> None:
        """Live total watts must update even between slower app-activity samples."""

        snapshot = ActivitySnapshot(
            sample_id=7,
            timestamp="2026-07-13T00:00:00",
            monotonic_time=100.0,
            sample_window_s=1.0,
            source="powermetrics-coalition",
            status="ok",
            activities=[
                AppActivity(
                    key="coalition:com.apple.Safari",
                    raw_name="com.apple.Safari",
                    display_name="Safari",
                    category="user_app",
                    energy_impact=100.0,
                    score=100.0,
                    source="powermetrics-coalition",
                )
            ],
        )
        engine = AppPowerAttributionEngine(top_slots=5)
        base_row = {
            "primary_total_load_source": "battery discharge watts",
            "baseline_primary_total_load_w": 10.0,
            "baseline_power_source": "Battery Power",
            "power_source": "Battery Power",
            "phase": "test",
            "stress_cpu_percent": 500.0,
        }
        first = engine.attribute({**base_row, "primary_total_load_w": 40.0}, snapshot)
        second = engine.attribute({**base_row, "primary_total_load_w": 80.0}, snapshot)
        self.assertAlmostEqual(first.apps[0].estimated_share_w or 0.0, 40.0)
        self.assertAlmostEqual(second.apps[0].estimated_share_w or 0.0, 80.0)
        self.assertAlmostEqual(second.apps[0].estimated_dynamic_w or 0.0, 70.0)

    def test_component_aware_cpu_gpu_allocation_preserves_total(self) -> None:
        """CPU/GPU pools use activity times and still sum to total watts."""

        snapshot = ActivitySnapshot(
            sample_id=8,
            timestamp="2026-07-13T00:00:00",
            monotonic_time=100.0,
            sample_window_s=1.0,
            source="powermetrics-coalition",
            status="ok",
            activities=[
                AppActivity(
                    key="coalition:app.a",
                    raw_name="app.a",
                    display_name="App A",
                    category="user_app",
                    energy_impact=60.0,
                    cpu_ms_per_s=300.0,
                    gpu_ms_per_s=0.0,
                    score=60.0,
                    source="powermetrics-coalition",
                ),
                AppActivity(
                    key="coalition:app.b",
                    raw_name="app.b",
                    display_name="App B",
                    category="user_app",
                    energy_impact=40.0,
                    cpu_ms_per_s=100.0,
                    gpu_ms_per_s=200.0,
                    score=40.0,
                    source="powermetrics-coalition",
                ),
            ],
        )
        result = AppPowerAttributionEngine(top_slots=5).attribute(
            {
                "primary_total_load_w": 100.0,
                "primary_total_load_source": "battery discharge watts",
                "baseline_primary_total_load_w": 20.0,
                "baseline_power_source": "Battery Power",
                "power_source": "Battery Power",
                "phase": "benchmark",
                "stress_cpu_percent": 1000.0,
                "cpu_power_w": 30.0,
                "gpu_power_w": 20.0,
            },
            snapshot,
        )
        by_name = {app.display_name: app for app in result.apps}
        self.assertAlmostEqual(result.cpu_component_pool_w or 0.0, 30.0)
        self.assertAlmostEqual(result.gpu_component_pool_w or 0.0, 20.0)
        self.assertAlmostEqual(result.residual_dynamic_pool_w or 0.0, 30.0)
        self.assertAlmostEqual(by_name["App A"].estimated_cpu_w or 0.0, 22.5)
        self.assertAlmostEqual(by_name["App B"].estimated_gpu_w or 0.0, 20.0)
        self.assertAlmostEqual(sum(app.estimated_share_w or 0.0 for app in result.apps), 100.0)

    def test_unlearned_baseline_does_not_claim_high_confidence(self) -> None:
        """A zero placeholder baseline must not be mistaken for calibration."""

        snapshot = ActivitySnapshot(
            sample_id=9,
            timestamp="2026-07-13T00:00:00",
            monotonic_time=100.0,
            sample_window_s=1.0,
            source="powermetrics-coalition",
            status="ok",
            activities=[
                AppActivity(
                    key="coalition:com.apple.Safari",
                    raw_name="com.apple.Safari",
                    display_name="Safari",
                    category="user_app",
                    energy_impact=100.0,
                    score=100.0,
                    source="powermetrics-coalition",
                )
            ],
        )
        result = AppPowerAttributionEngine(top_slots=5).attribute(
            {
                "primary_total_load_w": 40.0,
                "primary_total_load_source": "battery discharge watts",
                "power_source": "Battery Power",
                "phase": "benchmark",
                "stress_cpu_percent": 500.0,
            },
            snapshot,
        )
        self.assertEqual(result.confidence, "medium")

    def test_attribution_sums_to_total_power(self) -> None:
        snapshot = ActivitySnapshot(
            sample_id=1,
            timestamp="2026-07-13T00:00:00",
            monotonic_time=100.0,
            sample_window_s=1.0,
            source="powermetrics-coalition",
            status="ok",
            activities=[
                AppActivity(
                    key="coalition:com.google.Chrome",
                    raw_name="com.google.Chrome",
                    display_name="Google Chrome",
                    category="user_app",
                    energy_impact=75.0,
                    score=75.0,
                    source="powermetrics-coalition",
                ),
                AppActivity(
                    key="coalition:com.apple.WindowServer",
                    raw_name="com.apple.WindowServer",
                    display_name="WindowServer",
                    category="system",
                    energy_impact=25.0,
                    score=25.0,
                    source="powermetrics-coalition",
                ),
            ],
        )
        engine = AppPowerAttributionEngine(top_slots=5)
        result = engine.attribute(
            {
                "primary_total_load_w": 80.0,
                "primary_total_load_source": "battery discharge watts",
                "baseline_primary_total_load_w": 20.0,
                "baseline_power_source": "Battery Power",
                "power_source": "Battery Power",
                "phase": "benchmark",
                "stress_cpu_percent": 1000.0,
            },
            snapshot,
        )
        self.assertAlmostEqual(result.attributed_power_w or 0.0, 80.0)
        self.assertAlmostEqual(result.apps[0].estimated_share_w or 0.0, 60.0)
        self.assertAlmostEqual(result.apps[0].estimated_dynamic_w or 0.0, 45.0)
        self.assertEqual(result.confidence, "high")


class SessionLoggerTests(unittest.TestCase):
    """Validate streaming JSONL and incremental summary output."""

    def test_session_summary_files(self) -> None:
        snapshot = ActivitySnapshot(
            sample_id=1,
            timestamp="2026-07-13T00:00:00",
            monotonic_time=100.0,
            sample_window_s=1.0,
            source="powermetrics-coalition",
            status="ok",
            activities=[
                AppActivity(
                    key="coalition:com.google.Chrome",
                    raw_name="com.google.Chrome",
                    display_name="Google Chrome",
                    category="user_app",
                    energy_impact=100.0,
                    score=100.0,
                    source="powermetrics-coalition",
                )
            ],
        )
        engine = AppPowerAttributionEngine(top_slots=5)
        result = engine.attribute(
            {
                "primary_total_load_w": 60.0,
                "primary_total_load_source": "battery discharge watts",
                "power_source": "Battery Power",
                "phase": "test",
                "stress_cpu_percent": 100.0,
            },
            snapshot,
        )
        with tempfile.TemporaryDirectory() as temp_dir:
            logger = AppPowerSessionLogger(temp_dir, "mac_power_test", flush_every=1)
            logger.write(result, {"timestamp": "2026-07-13T00:00:00", "phase": "test"})
            logger.close()

            summary_path = Path(temp_dir) / "mac_power_test_apps_summary.json"
            jsonl_path = Path(temp_dir) / "mac_power_test_apps.jsonl"
            csv_path = Path(temp_dir) / "mac_power_test_apps_summary.csv"
            self.assertTrue(summary_path.exists())
            self.assertTrue(jsonl_path.exists())
            self.assertTrue(csv_path.exists())

            summary = json.loads(summary_path.read_text(encoding="utf-8"))
            self.assertEqual(summary["sample_count"], 1)
            self.assertEqual(summary["apps"][0]["display_name"], "Google Chrome")
            self.assertGreater(summary["apps"][0]["estimated_share_wh"], 0.0)


if __name__ == "__main__":
    unittest.main()
