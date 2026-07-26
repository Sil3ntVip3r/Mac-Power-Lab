#!/usr/bin/env python3
"""Regression tests for the active legacy monitor's one-shot lifecycle."""

from __future__ import annotations

import csv
import io
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock, patch

ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

import mac_power_watch as monitor  # noqa: E402


def sample_row():
    return {
        "timestamp": "2026-07-26T00:00:00+00:00",
        "version": monitor.VERSION,
        "display_time": "2026-07-26 00:00:00",
        "phase": "test",
        "mode": "discharging",
        "power_source": "Battery Power",
        "battery_percent": 50.0,
        "battery_temp_c": 30.0,
        "battery_temp_f": 86.0,
        "virtual_temp_c": 30.0,
        "net_battery_watts": -12.0,
        "bms_system_power_w": 12.0,
        "preferred_system_power_w": 12.0,
        "charger_live_verdict": "battery power",
        "whole_mac_watts_estimate": 12.0,
        "whole_mac_watts_note": "test",
        "battery_voltage_v": 12.0,
        "battery_amperage_a": -1.0,
        "adapter_reported_watts": 0.0,
        "battery_health_percent": 95.0,
        "pm_status": "off",
    }


def once_args(**overrides):
    values = {
        "phase_file": "/tmp/phase",
        "powermetrics": False,
        "powermetrics_sample_ms": 1000,
        "log": "auto",
        "no_log": False,
        "debug_every": 30,
        "no_debug_json": True,
    }
    values.update(overrides)
    return SimpleNamespace(**values)


class OnceLoggingTests(unittest.TestCase):
    def test_bare_once_keeps_stdout_only_behavior(self):
        with (
            patch.object(monitor, "collect_reading", return_value=(sample_row(), {})),
            patch.object(monitor, "RunLogger") as logger,
            redirect_stdout(io.StringIO()) as output,
        ):
            result = monitor.run_once(once_args(), log_requested=False)

        self.assertEqual(result["battery_percent"], 50.0)
        self.assertIn("MacBook Power Monitor", output.getvalue())
        logger.assert_not_called()

    def test_explicit_log_writes_one_header_and_one_data_row(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "one-shot.csv"
            with (
                patch.object(monitor, "collect_reading", return_value=(sample_row(), {"raw": True})),
                redirect_stdout(io.StringIO()),
            ):
                monitor.run_once(once_args(log=str(path)), log_requested=True)

            with path.open(newline="", encoding="utf-8") as handle:
                rows = list(csv.reader(handle))

        self.assertEqual(len(rows), 2)
        self.assertEqual(rows[0], monitor.CSV_HEADERS)
        self.assertEqual(rows[1][monitor.CSV_HEADERS.index("battery_percent")], "50.0")

    def test_requested_powermetrics_snapshot_is_used_for_reading(self):
        snapshot = {"pm_status": "ok", "soc_power_w": 9.5}
        collector = Mock(return_value=(sample_row(), {}))
        with (
            patch.object(monitor, "collect_powermetrics_once", return_value=snapshot) as powermetrics,
            patch.object(monitor, "collect_reading", collector),
            redirect_stdout(io.StringIO()),
        ):
            monitor.run_once(once_args(powermetrics=True, powermetrics_sample_ms=750))

        powermetrics.assert_called_once_with(750)
        collector.assert_called_once_with("/tmp/phase", snapshot)

    def test_powermetrics_collection_runs_once_synchronously(self):
        worker = Mock()
        worker.snapshot.return_value = {"pm_status": "ok"}
        with patch.object(monitor, "PowermetricsWorker", return_value=worker) as worker_type:
            result = monitor.collect_powermetrics_once(625)

        worker_type.assert_called_once_with(sample_ms=625)
        worker._run_once.assert_called_once_with()
        worker.snapshot.assert_called_once_with()
        self.assertEqual(result, {"pm_status": "ok"})

    def test_main_distinguishes_default_and_explicit_log(self):
        with patch.object(monitor, "run_once") as run:
            monitor.main(["--once"])
            self.assertFalse(run.call_args.kwargs["log_requested"])

            monitor.main(["--once", "--log=/tmp/one-shot.csv"])
            self.assertTrue(run.call_args.kwargs["log_requested"])

    def test_option_supplied_accepts_separate_and_equals_forms(self):
        self.assertFalse(monitor.option_supplied(["--once"], "--log"))
        self.assertTrue(monitor.option_supplied(["--once", "--log", "out.csv"], "--log"))
        self.assertTrue(monitor.option_supplied(["--once", "--log=out.csv"], "--log"))


if __name__ == "__main__":
    unittest.main()
