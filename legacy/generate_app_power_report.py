#!/usr/bin/env python3
"""Generate MacPowerLab per-application power and energy reports.

The report consumes the incremental files created by
``AppPowerSessionLogger``:

* ``mac_power_<run>_apps_summary.json`` — session aggregate.
* ``mac_power_<run>_apps.jsonl`` — timestamped app-power samples.
* ``test_runs.jsonl`` — optional benchmark windows.

Output is written as Markdown and self-contained HTML. Per-app watts are
model-based estimates calculated by distributing measured/estimated system power
according to Apple's Energy Impact or a documented low-confidence fallback.
"""

from __future__ import annotations

import argparse
import html
import json
import math
from datetime import datetime
from pathlib import Path
from typing import Any, Iterable, Iterator, Mapping

VERSION = "0.9.0"


def finite_float(value: Any) -> float | None:
    """Return a finite float or ``None``."""

    try:
        result = float(value)
    except (TypeError, ValueError, OverflowError):
        return None
    return result if math.isfinite(result) else None


def parse_timestamp(value: Any) -> datetime | None:
    """Parse an ISO-8601 timestamp without raising."""

    try:
        return datetime.fromisoformat(str(value))
    except (TypeError, ValueError):
        return None


def format_number(value: Any, suffix: str = "", digits: int = 2) -> str:
    """Format a numeric value for human-readable reports."""

    numeric = finite_float(value)
    return f"{numeric:.{digits}f}{suffix}" if numeric is not None else "n/a"


def human_bytes(value: Any) -> str:
    """Format a byte count using binary units."""

    numeric = finite_float(value)
    if numeric is None:
        return "n/a"
    units = ("B", "KiB", "MiB", "GiB", "TiB")
    size = max(0.0, numeric)
    for unit in units:
        if size < 1024.0 or unit == units[-1]:
            return f"{size:.1f} {unit}"
        size /= 1024.0
    return f"{size:.1f} TiB"


def latest_summary(logs_dir: Path) -> Path | None:
    """Return the newest app-power summary JSON in ``logs_dir``."""

    files = sorted(logs_dir.glob("mac_power_*_apps_summary.json"), key=lambda path: path.stat().st_mtime, reverse=True)
    return files[0] if files else None


def load_json(path: Path) -> dict[str, Any]:
    """Load a JSON object with a clear error message."""

    try:
        data = json.loads(path.read_text(encoding="utf-8", errors="replace"))
    except (OSError, json.JSONDecodeError) as exc:
        raise RuntimeError(f"cannot read JSON {path}: {exc}") from exc
    if not isinstance(data, dict):
        raise RuntimeError(f"expected JSON object in {path}")
    return data


def load_jsonl(path: Path) -> list[dict[str, Any]]:
    """Load valid objects from a JSONL file while skipping malformed lines."""

    rows: list[dict[str, Any]] = []
    if not path.exists():
        return rows
    for line_number, line in enumerate(path.read_text(encoding="utf-8", errors="replace").splitlines(), 1):
        if not line.strip():
            continue
        try:
            item = json.loads(line)
        except json.JSONDecodeError:
            continue
        if isinstance(item, dict):
            item.setdefault("_line_number", line_number)
            rows.append(item)
    return rows


def iter_jsonl(path: Path) -> Iterator[dict[str, Any]]:
    """Yield valid JSON objects from a JSONL file without loading it all.

    Streaming keeps report memory use bounded for multi-hour or multi-day power
    captures. Malformed lines are skipped so one interrupted write cannot prevent
    the rest of the report from being generated.
    """

    if not path.exists():
        return
    with path.open("r", encoding="utf-8", errors="replace") as handle:
        for line_number, line in enumerate(handle, 1):
            if not line.strip():
                continue
            try:
                item = json.loads(line)
            except json.JSONDecodeError:
                continue
            if isinstance(item, dict):
                item.setdefault("_line_number", line_number)
                yield item


def category_title(category: str) -> str:
    """Return a user-facing title for a normalized app category."""

    return {
        "user_app": "User applications",
        "background_service": "Background services",
        "system": "System services",
        "benchmark": "Benchmark workloads",
        "measurement": "Measurement overhead",
        "unattributed": "Unattributed/transient work",
    }.get(category, category.replace("_", " ").title())


def markdown_table(apps: Iterable[Mapping[str, Any]], limit: int) -> list[str]:
    """Build a Markdown table for the top ``limit`` applications."""

    lines = [
        "| Application | Category | Est. energy | Avg share | Peak share | Dynamic energy | CPU est. | GPU est. | Avg Energy Impact | CPU activity | GPU activity | Wakeups I/P | Disk R/W | Network R/T |",
        "|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|",
    ]
    for app in list(apps)[:limit]:
        disk = f"{human_bytes(app.get('disk_read_bytes'))} / {human_bytes(app.get('disk_write_bytes'))}"
        network = f"{human_bytes(app.get('network_rx_bytes'))} / {human_bytes(app.get('network_tx_bytes'))}"
        lines.append(
            f"| {app.get('display_name') or app.get('raw_name') or 'Unknown'} | {category_title(str(app.get('category') or 'unknown'))} | "
            f"{format_number(app.get('estimated_share_wh'), ' Wh', 3)} | "
            f"{format_number(app.get('average_estimated_w'), ' W')} | "
            f"{format_number(app.get('peak_estimated_w'), ' W')} | "
            f"{format_number(app.get('estimated_dynamic_wh'), ' Wh', 3)} | "
            f"{format_number(app.get('average_estimated_cpu_w'), ' W')} | "
            f"{format_number(app.get('average_estimated_gpu_w'), ' W')} | "
            f"{format_number(app.get('average_energy_impact'))} | "
            f"{format_number(app.get('average_cpu_ms_per_s'), ' ms/s')} | "
            f"{format_number(app.get('average_gpu_ms_per_s'), ' ms/s')} | "
            f"{format_number(app.get('average_intr_wakeups_per_s'))} / {format_number(app.get('average_idle_wakeups_per_s'))} | {disk} | {network} |"
        )
    return lines


def _new_window_entry(key: str, app: Mapping[str, Any]) -> dict[str, Any]:
    """Create one mutable per-app accumulator for a benchmark window."""

    return {
        "key": key,
        "display_name": app.get("display_name") or app.get("raw_name") or "Unknown",
        "category": app.get("category") or "unknown",
        "estimated_share_wh": 0.0,
        "estimated_dynamic_wh": 0.0,
        "estimated_cpu_wh": 0.0,
        "estimated_gpu_wh": 0.0,
        "estimated_residual_wh": 0.0,
        "watt_seconds": 0.0,
        "cpu_watt_seconds": 0.0,
        "gpu_watt_seconds": 0.0,
        "seconds": 0.0,
        "peak_estimated_w": 0.0,
        "energy_impact_total": 0.0,
        "energy_impact_count": 0,
        "cpu_ms_per_s_total": 0.0,
        "cpu_ms_per_s_count": 0,
        "gpu_ms_per_s_total": 0.0,
        "gpu_ms_per_s_count": 0,
        "intr_wakeups_per_s_total": 0.0,
        "intr_wakeups_per_s_count": 0,
        "idle_wakeups_per_s_total": 0.0,
        "idle_wakeups_per_s_count": 0,
        "disk_read_bytes": 0.0,
        "disk_write_bytes": 0.0,
        "network_rx_bytes": 0.0,
        "network_tx_bytes": 0.0,
    }


def _accumulate_sample(aggregates: dict[str, dict[str, Any]], sample: Mapping[str, Any]) -> None:
    """Accumulate one app-power sample into a benchmark-window aggregate."""

    interval = finite_float(sample.get("interval_seconds")) or 0.0
    if interval <= 0:
        return
    for app in sample.get("apps") or []:
        if not isinstance(app, dict):
            continue
        key = str(app.get("key") or app.get("display_name") or app.get("raw_name") or "unknown")
        entry = aggregates.setdefault(key, _new_window_entry(key, app))
        entry["display_name"] = app.get("display_name") or entry["display_name"]
        entry["category"] = app.get("category") or entry["category"]

        estimated_w = finite_float(app.get("estimated_share_w"))
        dynamic_w = finite_float(app.get("estimated_dynamic_w"))
        cpu_w = finite_float(app.get("estimated_cpu_w"))
        gpu_w = finite_float(app.get("estimated_gpu_w"))
        residual_w = finite_float(app.get("estimated_residual_w"))
        estimated_wh = finite_float(app.get("estimated_wh_interval"))
        dynamic_wh = finite_float(app.get("estimated_dynamic_wh_interval"))
        cpu_wh = finite_float(app.get("estimated_cpu_wh_interval"))
        gpu_wh = finite_float(app.get("estimated_gpu_wh_interval"))
        residual_wh = finite_float(app.get("estimated_residual_wh_interval"))
        impact = finite_float(app.get("energy_impact_per_s"))
        if impact is None:
            impact = finite_float(app.get("energy_impact"))
        cpu_activity = finite_float(app.get("cpu_ms_per_s"))
        gpu_activity = finite_float(app.get("gpu_ms_per_s"))
        intr_wakeups = finite_float(app.get("intr_wakeups_per_s"))
        idle_wakeups = finite_float(app.get("idle_wakeups_per_s"))
        disk_read_bps = finite_float(app.get("disk_read_bytes_per_s"))
        disk_write_bps = finite_float(app.get("disk_write_bytes_per_s"))
        network_rx_bps = finite_float(app.get("network_rx_bytes_per_s"))
        network_tx_bps = finite_float(app.get("network_tx_bytes_per_s"))

        if estimated_wh is not None:
            entry["estimated_share_wh"] += estimated_wh
        elif estimated_w is not None:
            entry["estimated_share_wh"] += estimated_w * interval / 3600.0
        if dynamic_wh is not None:
            entry["estimated_dynamic_wh"] += dynamic_wh
        elif dynamic_w is not None:
            entry["estimated_dynamic_wh"] += dynamic_w * interval / 3600.0
        if cpu_wh is not None:
            entry["estimated_cpu_wh"] += cpu_wh
        elif cpu_w is not None:
            entry["estimated_cpu_wh"] += cpu_w * interval / 3600.0
        if gpu_wh is not None:
            entry["estimated_gpu_wh"] += gpu_wh
        elif gpu_w is not None:
            entry["estimated_gpu_wh"] += gpu_w * interval / 3600.0
        if residual_wh is not None:
            entry["estimated_residual_wh"] += residual_wh
        elif residual_w is not None:
            entry["estimated_residual_wh"] += residual_w * interval / 3600.0

        if estimated_w is not None:
            entry["watt_seconds"] += estimated_w * interval
            entry["seconds"] += interval
            entry["peak_estimated_w"] = max(entry["peak_estimated_w"], estimated_w)
        if cpu_w is not None:
            entry["cpu_watt_seconds"] += cpu_w * interval
        if gpu_w is not None:
            entry["gpu_watt_seconds"] += gpu_w * interval
        if impact is not None:
            entry["energy_impact_total"] += impact
            entry["energy_impact_count"] += 1
        if cpu_activity is not None:
            entry["cpu_ms_per_s_total"] += cpu_activity
            entry["cpu_ms_per_s_count"] += 1
        if gpu_activity is not None:
            entry["gpu_ms_per_s_total"] += gpu_activity
            entry["gpu_ms_per_s_count"] += 1
        if intr_wakeups is not None:
            entry["intr_wakeups_per_s_total"] += intr_wakeups
            entry["intr_wakeups_per_s_count"] += 1
        if idle_wakeups is not None:
            entry["idle_wakeups_per_s_total"] += idle_wakeups
            entry["idle_wakeups_per_s_count"] += 1
        entry["disk_read_bytes"] += (disk_read_bps or 0.0) * interval
        entry["disk_write_bytes"] += (disk_write_bps or 0.0) * interval
        entry["network_rx_bytes"] += (network_rx_bps or 0.0) * interval
        entry["network_tx_bytes"] += (network_tx_bps or 0.0) * interval


def _finalize_window(aggregates: Mapping[str, Mapping[str, Any]]) -> list[dict[str, Any]]:
    """Convert mutable window accumulators into sorted report rows."""

    rows: list[dict[str, Any]] = []
    for original in aggregates.values():
        entry = dict(original)
        seconds = finite_float(entry.pop("seconds", 0.0)) or 0.0
        impact_count = int(finite_float(entry.pop("energy_impact_count", 0)) or 0)
        entry["average_estimated_w"] = (finite_float(entry.pop("watt_seconds", 0.0)) or 0.0) / seconds if seconds > 0 else None
        entry["average_estimated_cpu_w"] = (finite_float(entry.pop("cpu_watt_seconds", 0.0)) or 0.0) / seconds if seconds > 0 else None
        entry["average_estimated_gpu_w"] = (finite_float(entry.pop("gpu_watt_seconds", 0.0)) or 0.0) / seconds if seconds > 0 else None
        impact_total = finite_float(entry.pop("energy_impact_total", 0.0)) or 0.0
        entry["average_energy_impact"] = impact_total / impact_count if impact_count > 0 else None

        for output_key, total_key, count_key in (
            ("average_cpu_ms_per_s", "cpu_ms_per_s_total", "cpu_ms_per_s_count"),
            ("average_gpu_ms_per_s", "gpu_ms_per_s_total", "gpu_ms_per_s_count"),
            ("average_intr_wakeups_per_s", "intr_wakeups_per_s_total", "intr_wakeups_per_s_count"),
            ("average_idle_wakeups_per_s", "idle_wakeups_per_s_total", "idle_wakeups_per_s_count"),
        ):
            metric_total = finite_float(entry.pop(total_key, 0.0)) or 0.0
            metric_count = int(finite_float(entry.pop(count_key, 0)) or 0)
            entry[output_key] = metric_total / metric_count if metric_count > 0 else None
        rows.append(entry)
    rows.sort(key=lambda item: (item.get("estimated_share_wh") or 0.0, item.get("peak_estimated_w") or 0.0), reverse=True)
    return rows


def aggregate_test_window(samples: Iterable[Mapping[str, Any]], start: datetime, end: datetime) -> list[dict[str, Any]]:
    """Aggregate app estimates for one benchmark window."""

    aggregates: dict[str, dict[str, Any]] = {}
    for sample in samples:
        timestamp = parse_timestamp(sample.get("timestamp"))
        if timestamp is None or timestamp < start or timestamp > end:
            continue
        _accumulate_sample(aggregates, sample)
    return _finalize_window(aggregates)


def aggregate_test_windows_stream(
    jsonl_path: Path,
    test_runs: Iterable[Mapping[str, Any]],
) -> list[tuple[str, list[dict[str, Any]]]]:
    """Aggregate all benchmark windows in one bounded streaming pass."""

    windows: list[dict[str, Any]] = []
    for run in test_runs:
        start = parse_timestamp(run.get("start_time"))
        end = parse_timestamp(run.get("end_time"))
        if start is None or end is None or end < start:
            continue
        windows.append(
            {
                "title": str(run.get("title") or run.get("test_id") or "Benchmark window"),
                "start": start,
                "end": end,
                "aggregates": {},
            }
        )

    if not windows or not jsonl_path.exists():
        return []

    for sample in iter_jsonl(jsonl_path):
        timestamp = parse_timestamp(sample.get("timestamp"))
        if timestamp is None:
            continue
        for window in windows:
            if window["start"] <= timestamp <= window["end"]:
                _accumulate_sample(window["aggregates"], sample)

    results: list[tuple[str, list[dict[str, Any]]]] = []
    for window in windows:
        aggregated = _finalize_window(window["aggregates"])
        if aggregated:
            results.append((window["title"], aggregated))
    return results


def html_table(apps: Iterable[Mapping[str, Any]], limit: int) -> str:
    """Build an HTML table for application summaries."""

    rows = []
    for app in list(apps)[:limit]:
        rows.append(
            "<tr>"
            f"<td>{html.escape(str(app.get('display_name') or app.get('raw_name') or 'Unknown'))}</td>"
            f"<td>{html.escape(category_title(str(app.get('category') or 'unknown')))}</td>"
            f"<td>{format_number(app.get('estimated_share_wh'), ' Wh', 3)}</td>"
            f"<td>{format_number(app.get('average_estimated_w'), ' W')}</td>"
            f"<td>{format_number(app.get('peak_estimated_w'), ' W')}</td>"
            f"<td>{format_number(app.get('estimated_dynamic_wh'), ' Wh', 3)}</td>"
            f"<td>{format_number(app.get('average_estimated_cpu_w'), ' W')}</td>"
            f"<td>{format_number(app.get('average_estimated_gpu_w'), ' W')}</td>"
            f"<td>{format_number(app.get('average_energy_impact'))}</td>"
            f"<td>{format_number(app.get('average_cpu_ms_per_s'), ' ms/s')}</td>"
            f"<td>{format_number(app.get('average_gpu_ms_per_s'), ' ms/s')}</td>"
            f"<td>{format_number(app.get('average_intr_wakeups_per_s'))} / {format_number(app.get('average_idle_wakeups_per_s'))}</td>"
            "</tr>"
        )
    return (
        "<table><thead><tr><th>Application</th><th>Category</th><th>Est. energy</th>"
        "<th>Avg share</th><th>Peak share</th><th>Dynamic energy</th><th>CPU est.</th><th>GPU est.</th><th>Energy Impact</th>"
        "<th>CPU activity</th><th>GPU activity</th><th>Wakeups I/P</th></tr></thead><tbody>"
        + "".join(rows)
        + "</tbody></table>"
    )


def generate_report(summary_path: Path, top: int) -> tuple[Path, Path]:
    """Generate Markdown and HTML reports for ``summary_path``."""

    summary = load_json(summary_path)
    apps = summary.get("apps") or []
    if not isinstance(apps, list):
        raise RuntimeError(f"invalid apps list in {summary_path}")
    apps = [app for app in apps if isinstance(app, dict)]
    apps.sort(key=lambda item: (finite_float(item.get("estimated_share_wh")) or 0.0), reverse=True)

    stem = summary_path.name.removesuffix("_apps_summary.json")
    jsonl_path = summary_path.with_name(f"{stem}_apps.jsonl")
    test_runs = load_jsonl(summary_path.parent / "test_runs.jsonl")

    markdown_path = summary_path.with_name(f"{stem}_apps_report.md")
    html_path = summary_path.with_name(f"{stem}_apps_report.html")

    total_app_wh = sum(finite_float(app.get("estimated_share_wh")) or 0.0 for app in apps)
    total_dynamic_wh = sum(finite_float(app.get("estimated_dynamic_wh")) or 0.0 for app in apps)
    user_apps = [app for app in apps if app.get("category") == "user_app"]
    system_apps = [app for app in apps if app.get("category") in ("system", "background_service", "unattributed")]
    benchmark_apps = [app for app in apps if app.get("category") == "benchmark"]
    dynamic_apps = sorted(
        apps,
        key=lambda item: (finite_float(item.get("estimated_dynamic_wh")) or 0.0),
        reverse=True,
    )

    md = [
        f"# MacPowerLab Application Power Report v{VERSION}",
        "",
        f"Created: {datetime.now().isoformat(timespec='seconds')}",
        f"Source: `{summary_path}`",
        "",
        "## Method and accuracy",
        "",
        "- Apple `powermetrics --show-process-energy` supplies Energy Impact plus CPU, GPU, wakeup, disk, and network activity.",
        "- Coalition mode groups helper processes into application/resource coalitions where macOS exposes them.",
        "- MacPowerLab uses a component-aware model: CPU watts by CPU time, GPU watts by GPU time, and remaining dynamic power by Energy Impact/activity score.",
        "- Per-app watts and Wh are estimates, not direct electrical measurements. Use them for same-Mac comparisons and ranking.",
        "",
        "## Session overview",
        "",
        f"- App-power samples: {summary.get('sample_count', 0)}",
        f"- Observed time: {format_number((finite_float(summary.get('observed_seconds')) or 0.0) / 60.0, ' min')}",
        f"- Estimated system energy during app samples: {format_number(summary.get('estimated_system_wh'), ' Wh', 3)}",
        f"- Sum of per-app estimated energy: {format_number(total_app_wh, ' Wh', 3)}",
        f"- Sum of per-app estimated dynamic energy: {format_number(total_dynamic_wh, ' Wh', 3)}",
        f"- Sampling sources: `{summary.get('source_counts') or {}}`",
        f"- Confidence distribution: `{summary.get('confidence_counts') or {}}`",
        f"- Sampling errors/fallback events: {summary.get('error_count', 0)}",
        "",
        "## Top likely incremental power users",
        "",
        *markdown_table(dynamic_apps, top),
        "",
        "## Top applications and services by total share",
        "",
        *markdown_table(apps, top),
        "",
        "## User applications",
        "",
    ]
    if user_apps:
        md.extend(markdown_table(user_apps, top))
    else:
        md.append("No user applications were classified.")
    md.extend(["", "## System and background services", ""])
    if system_apps:
        md.extend(markdown_table(system_apps, top))
    else:
        md.append("No system/background services were classified.")
    if benchmark_apps:
        md += ["", "## Benchmark workloads", "", *markdown_table(benchmark_apps, top)]

    test_sections = aggregate_test_windows_stream(jsonl_path, test_runs)
    for title, aggregated in test_sections:
        md += ["", f"## App attribution during: {title}", "", *markdown_table(aggregated, top)]

    markdown_path.write_text("\n".join(md) + "\n", encoding="utf-8")

    css = """
body{font-family:-apple-system,BlinkMacSystemFont,Segoe UI,sans-serif;background:#0b1016;color:#e6edf3;margin:0;padding:28px}
h1,h2{color:#65e5ff} .card{background:#111820;border:1px solid #253545;border-radius:14px;padding:16px;margin:12px 0}
table{border-collapse:collapse;width:100%;background:#111820;margin:12px 0 28px}th,td{text-align:left;padding:9px;border-bottom:1px solid #263443}th{background:#182433}.muted{color:#9fb3c8}
"""
    sections_html = []
    for title, aggregated in test_sections:
        sections_html.append(f"<h2>App attribution during: {html.escape(title)}</h2>{html_table(aggregated, top)}")
    html_doc = f"""<!doctype html><html><head><meta charset='utf-8'><title>MacPowerLab Application Power Report</title><style>{css}</style></head><body>
<h1>MacPowerLab Application Power Report v{VERSION}</h1>
<p class='muted'>Source: {html.escape(str(summary_path))}</p>
<div class='card'><strong>Method:</strong> estimated app power shares use MacPowerLab total power, CPU/GPU component pools where available, and Apple Energy Impact/activity data. They are not direct per-app electrical measurements.</div>
<h2>Session overview</h2>
<div class='card'>Samples: {summary.get('sample_count',0)}<br>Observed: {format_number((finite_float(summary.get('observed_seconds')) or 0.0)/60.0,' min')}<br>Estimated system energy: {format_number(summary.get('estimated_system_wh'),' Wh',3)}<br>Per-app energy sum: {format_number(total_app_wh,' Wh',3)}<br>Sources: {html.escape(str(summary.get('source_counts') or {}))}<br>Confidence: {html.escape(str(summary.get('confidence_counts') or {}))}<br>Errors/fallbacks: {summary.get('error_count',0)}</div>
<h2>Top likely incremental power users</h2>{html_table(dynamic_apps, top)}
<h2>Top applications and services by total share</h2>{html_table(apps, top)}
<h2>User applications</h2>{html_table(user_apps, top) if user_apps else '<p>No user applications were classified.</p>'}
<h2>System and background services</h2>{html_table(system_apps, top) if system_apps else '<p>No system/background services were classified.</p>'}
{''.join(sections_html)}
</body></html>"""
    html_path.write_text(html_doc, encoding="utf-8")
    return markdown_path, html_path


def build_parser() -> argparse.ArgumentParser:
    """Build command-line input validation."""

    parser = argparse.ArgumentParser(description="Generate a MacPowerLab application power report.")
    parser.add_argument("--summary", type=Path, help="specific *_apps_summary.json file; default is latest")
    parser.add_argument("--top", type=int, default=20, help="maximum rows per table, 1-200")
    return parser


def main() -> int:
    """CLI entry point."""

    parser = build_parser()
    args = parser.parse_args()
    if args.top < 1 or args.top > 200:
        parser.error("--top must be between 1 and 200")
    logs_dir = Path(__file__).resolve().parent / "logs"
    summary_path = args.summary.expanduser().resolve() if args.summary else latest_summary(logs_dir)
    if summary_path is None or not summary_path.exists():
        parser.error("no mac_power_*_apps_summary.json file found")
    try:
        markdown_path, html_path = generate_report(summary_path, args.top)
    except RuntimeError as exc:
        parser.error(str(exc))
    print(f"App power Markdown report: {markdown_path}")
    print(f"App power HTML report: {html_path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
