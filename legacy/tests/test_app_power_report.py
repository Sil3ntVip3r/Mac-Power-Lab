#!/usr/bin/env python3
"""Integration tests for MacPowerLab application power reports."""

from __future__ import annotations

import json
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from generate_app_power_report import generate_report  # noqa: E402


class AppPowerReportTests(unittest.TestCase):
    """Validate report generation from incremental app-power files."""

    def test_markdown_and_html_reports(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            directory = Path(temp_dir)
            summary_path = directory / "mac_power_test_apps_summary.json"
            summary_path.write_text(
                json.dumps(
                    {
                        "sample_count": 3,
                        "observed_seconds": 30.0,
                        "estimated_system_wh": 0.5,
                        "apps": [
                            {
                                "display_name": "Safari",
                                "raw_name": "com.apple.Safari",
                                "category": "user_app",
                                "estimated_share_wh": 0.3,
                                "estimated_dynamic_wh": 0.2,
                                "average_estimated_w": 36.0,
                                "peak_estimated_w": 45.0,
                                "average_energy_impact": 75.0,
                                "average_cpu_ms_per_s": 300.0,
                                "average_gpu_ms_per_s": 20.0,
                                "disk_read_bytes": 1024,
                                "disk_write_bytes": 2048,
                                "network_rx_bytes": 4096,
                                "network_tx_bytes": 512,
                            }
                        ],
                    }
                ),
                encoding="utf-8",
            )
            (directory / "mac_power_test_apps.jsonl").write_text("", encoding="utf-8")

            markdown_path, html_path = generate_report(summary_path, top=20)
            self.assertTrue(markdown_path.exists())
            self.assertTrue(html_path.exists())
            self.assertIn("Safari", markdown_path.read_text(encoding="utf-8"))
            self.assertIn("Estimated", html_path.read_text(encoding="utf-8"))


if __name__ == "__main__":
    unittest.main()
