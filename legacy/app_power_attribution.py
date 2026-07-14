#!/usr/bin/env python3
"""Per-application power attribution for MacPowerLab.

This module provides a production-oriented, dependency-free attribution pipeline
for macOS:

1. ``PowermetricsTaskSampler`` collects Apple's per-process or per-coalition
   ``energy_impact`` metric plus CPU, GPU, wakeup, disk, and network activity.
2. ``PsFallbackSampler`` provides a low-confidence fallback when powermetrics is
   unavailable or sudo authorization has expired.
3. ``AppPowerAttributionEngine`` combines process activity with MacPowerLab's
   measured/estimated total system power. CPU component watts are allocated by
   CPU time, GPU component watts by GPU time, and remaining dynamic power by
   Energy Impact/activity score.
4. ``AppPowerWorker`` performs sampling in a bounded background thread.
5. ``AppPowerSessionLogger`` writes streaming JSONL and incremental CSV/JSON
   summaries suitable for live reports and benchmark correlation.

Accuracy model
--------------
Apple's ``energy_impact`` is a rough platform-specific proxy that incorporates
CPU, GPU, disk, networking, and wakeups. macOS does not expose direct electrical
watts per process. MacPowerLab therefore reports:

* ``estimated_share_w``: the app's component-aware share of current system power.
* ``estimated_dynamic_w``: estimated app power above a quiet system baseline.
* ``estimated_cpu_w`` / ``estimated_gpu_w``: component allocations when macOS
  exposes usable CPU/GPU power telemetry.
* ``energy_share_percent``: the share of Apple Energy Impact/activity score.
* ``confidence``: high, medium, or low based on the telemetry sources available.

These values are estimates intended for comparing apps and workloads on the same
Mac, not direct per-process electrical measurements or cross-device comparisons.
"""

from __future__ import annotations

import argparse
import csv
import json
import math
import os
import plistlib
import re
import shutil
import subprocess
import threading
import time
from collections import defaultdict, deque
from dataclasses import asdict, dataclass, field
from datetime import datetime
from pathlib import Path
from typing import Any, Iterable, Mapping, MutableMapping, Sequence

VERSION = "0.9.0"
APP_POWER_SCHEMA = "macpowerlab.app_power.v1"
DEFAULT_TOP_SLOTS = 5
DEFAULT_MAX_ACTIVITIES = 160


class AppPowerError(RuntimeError):
    """Base exception for app-power attribution failures."""


class AppPowerSamplingError(AppPowerError):
    """Raised when a process-activity sampler cannot obtain a valid sample."""


class AppPowerValidationError(AppPowerError):
    """Raised when an app-power configuration value is invalid."""


def _finite_float(value: Any) -> float | None:
    """Return a finite float or ``None`` for unsupported/invalid values."""

    try:
        result = float(value)
    except (TypeError, ValueError, OverflowError):
        return None
    return result if math.isfinite(result) else None


def _finite_int(value: Any) -> int | None:
    """Return an integer or ``None`` without accepting NaN/Infinity."""

    numeric = _finite_float(value)
    if numeric is None:
        return None
    try:
        return int(numeric)
    except (TypeError, ValueError, OverflowError):
        return None


def _first_number(mapping: Mapping[str, Any], keys: Sequence[str]) -> float | None:
    """Return the first finite numeric value found under ``keys``."""

    for key in keys:
        if key in mapping:
            value = _finite_float(mapping.get(key))
            if value is not None:
                return value
    return None


def _safe_string(value: Any, limit: int = 512) -> str:
    """Convert ``value`` to a bounded string suitable for logs and the UI."""

    if value is None:
        return ""
    text = str(value).replace("\x00", "").strip()
    return text[: max(1, int(limit))]


def _now_iso() -> str:
    """Return a local ISO-8601 timestamp with second precision."""

    return datetime.now().isoformat(timespec="seconds")


def _clamp(value: float, minimum: float, maximum: float) -> float:
    """Clamp ``value`` to an inclusive numeric range."""

    return max(minimum, min(maximum, value))


def _percentile(values: Sequence[float], percentile: float) -> float | None:
    """Return a linearly interpolated percentile for a finite numeric sequence."""

    cleaned = sorted(v for v in values if math.isfinite(v))
    if not cleaned:
        return None
    if len(cleaned) == 1:
        return cleaned[0]
    position = _clamp(percentile, 0.0, 1.0) * (len(cleaned) - 1)
    lower = int(math.floor(position))
    upper = int(math.ceil(position))
    if lower == upper:
        return cleaned[lower]
    fraction = position - lower
    return cleaned[lower] + (cleaned[upper] - cleaned[lower]) * fraction


def validate_int(name: str, value: int, minimum: int, maximum: int) -> int:
    """Validate and return an integer constrained to ``[minimum, maximum]``."""

    if isinstance(value, bool) or not isinstance(value, int):
        raise AppPowerValidationError(f"{name} must be an integer")
    if value < minimum or value > maximum:
        raise AppPowerValidationError(f"{name} must be between {minimum} and {maximum}")
    return value


def validate_float(name: str, value: float, minimum: float, maximum: float) -> float:
    """Validate and return a finite float constrained to a closed range."""

    numeric = _finite_float(value)
    if numeric is None:
        raise AppPowerValidationError(f"{name} must be a finite number")
    if numeric < minimum or numeric > maximum:
        raise AppPowerValidationError(f"{name} must be between {minimum} and {maximum}")
    return numeric


@dataclass
class AppActivity:
    """Raw activity for a process or application coalition in one sample.

    Attributes:
        key: Stable aggregation key for this activity source.
        raw_name: Original powermetrics/ps name.
        display_name: Human-readable application or service name.
        category: ``user_app``, ``system``, ``background_service``,
            ``benchmark``, ``measurement``, or ``unattributed``.
        pid: Process ID when task-level data is used; coalitions may not have one.
        coalition_id: Coalition identifier when provided by powermetrics.
        responsible_pid: macOS responsible PID for task-level helper grouping.
        energy_impact: Apple's composite Energy Impact metric for the sample.
        energy_impact_per_s: Normalized Energy Impact when exposed.
        cpu_ms_per_s: CPU milliseconds consumed per sample-second.
        gpu_ms_per_s: GPU milliseconds consumed per sample-second.
        userland_ratio: Fraction/percentage of CPU time spent in user space.
        intr_wakeups_per_s: Interrupt wakeups per sample-second.
        idle_wakeups_per_s: Package-idle wakeups per sample-second.
        disk_read_bytes_per_s: Disk bytes read per sample-second.
        disk_write_bytes_per_s: Disk bytes written per sample-second.
        network_rx_bytes_per_s: Network bytes received per sample-second.
        network_tx_bytes_per_s: Network bytes sent per sample-second.
        score: Positive attribution score used when calculating power shares.
        source: ``powermetrics-coalition``, ``powermetrics-task``, or
            ``ps-fallback``.
        extra_metrics: Bounded scalar metrics retained for future parser work.
    """

    key: str
    raw_name: str
    display_name: str
    category: str
    pid: int | None = None
    coalition_id: int | None = None
    responsible_pid: int | None = None
    energy_impact: float | None = None
    energy_impact_per_s: float | None = None
    cpu_ms_per_s: float | None = None
    gpu_ms_per_s: float | None = None
    userland_ratio: float | None = None
    intr_wakeups_per_s: float | None = None
    idle_wakeups_per_s: float | None = None
    disk_read_bytes_per_s: float | None = None
    disk_write_bytes_per_s: float | None = None
    network_rx_bytes_per_s: float | None = None
    network_tx_bytes_per_s: float | None = None
    score: float = 0.0
    source: str = "unknown"
    extra_metrics: dict[str, Any] = field(default_factory=dict)

    def to_dict(self) -> dict[str, Any]:
        """Return a JSON-serializable representation."""

        return asdict(self)


@dataclass
class ActivitySnapshot:
    """Immutable-style result produced by an app activity sampler."""

    sample_id: int
    timestamp: str
    monotonic_time: float
    sample_window_s: float
    source: str
    status: str
    activities: list[AppActivity] = field(default_factory=list)
    error: str | None = None
    raw_record_count: int = 0

    @classmethod
    def starting(cls) -> "ActivitySnapshot":
        """Create an initial snapshot before the worker has sampled."""

        return cls(
            sample_id=0,
            timestamp=_now_iso(),
            monotonic_time=time.monotonic(),
            sample_window_s=0.0,
            source="none",
            status="starting",
            activities=[],
        )


@dataclass
class AttributedApp:
    """Application activity enriched with estimated power and energy share."""

    key: str
    raw_name: str
    display_name: str
    category: str
    pid: int | None
    coalition_id: int | None
    responsible_pid: int | None
    source: str
    energy_impact: float | None
    energy_impact_per_s: float | None
    cpu_ms_per_s: float | None
    gpu_ms_per_s: float | None
    intr_wakeups_per_s: float | None
    idle_wakeups_per_s: float | None
    disk_read_bytes_per_s: float | None
    disk_write_bytes_per_s: float | None
    network_rx_bytes_per_s: float | None
    network_tx_bytes_per_s: float | None
    activity_score: float
    energy_share_percent: float
    estimated_share_w: float | None
    estimated_dynamic_w: float | None
    estimated_cpu_w: float | None
    estimated_gpu_w: float | None
    estimated_residual_w: float | None

    def to_dict(self) -> dict[str, Any]:
        """Return a JSON-serializable representation."""

        return asdict(self)


@dataclass
class AttributionResult:
    """Result of allocating total system power to observed applications."""

    sample_id: int
    timestamp: str
    sample_monotonic_time: float
    sample_window_s: float
    sample_age_s: float
    status: str
    source: str
    confidence: str
    total_system_power_w: float | None
    total_power_source: str
    baseline_power_w: float | None
    dynamic_power_w: float | None
    cpu_component_pool_w: float | None
    gpu_component_pool_w: float | None
    residual_dynamic_pool_w: float | None
    attribution_method: str
    attributed_power_w: float | None
    unattributed_power_w: float | None
    apps: list[AttributedApp]
    error: str | None = None

    def top_for_display(self, limit: int) -> list[AttributedApp]:
        """Return top apps, excluding measurement overhead when possible."""

        limit = max(1, int(limit))
        preferred = [app for app in self.apps if app.category != "measurement"]
        pool = preferred if preferred else self.apps
        return pool[:limit]

    def to_row_fields(self, top_slots: int = DEFAULT_TOP_SLOTS) -> dict[str, Any]:
        """Flatten attribution into bounded fields suitable for the main CSV."""

        top_slots = validate_int("top_slots", int(top_slots), 1, 12)
        top = self.top_for_display(top_slots)
        fields: dict[str, Any] = {
            "app_power_status": self.status,
            "app_power_source": self.source,
            "app_power_confidence": self.confidence,
            "app_power_error": self.error,
            "app_power_sample_id": self.sample_id,
            "app_power_sample_age_s": self.sample_age_s,
            "app_power_total_system_w": self.total_system_power_w,
            "app_power_total_source": self.total_power_source,
            "app_power_baseline_w": self.baseline_power_w,
            "app_power_dynamic_w": self.dynamic_power_w,
            "app_power_cpu_pool_w": self.cpu_component_pool_w,
            "app_power_gpu_pool_w": self.gpu_component_pool_w,
            "app_power_residual_pool_w": self.residual_dynamic_pool_w,
            "app_power_method": self.attribution_method,
            "app_power_attributed_w": self.attributed_power_w,
            "app_power_unattributed_w": self.unattributed_power_w,
            "app_power_top_summary": " | ".join(
                f"{app.display_name} {app.estimated_share_w:.1f}W"
                for app in top
                if app.estimated_share_w is not None
            ),
            "app_power_top_json": json.dumps([app.to_dict() for app in top], ensure_ascii=False, separators=(",", ":")),
        }
        for index in range(1, top_slots + 1):
            app = top[index - 1] if index <= len(top) else None
            prefix = f"app_top_{index}"
            fields[f"{prefix}_name"] = app.display_name if app else None
            fields[f"{prefix}_category"] = app.category if app else None
            fields[f"{prefix}_estimated_w"] = app.estimated_share_w if app else None
            fields[f"{prefix}_dynamic_w"] = app.estimated_dynamic_w if app else None
            fields[f"{prefix}_cpu_w"] = app.estimated_cpu_w if app else None
            fields[f"{prefix}_gpu_w"] = app.estimated_gpu_w if app else None
            fields[f"{prefix}_residual_w"] = app.estimated_residual_w if app else None
            fields[f"{prefix}_share_percent"] = app.energy_share_percent if app else None
            fields[f"{prefix}_energy_impact"] = app.energy_impact if app else None
            fields[f"{prefix}_cpu_ms_per_s"] = app.cpu_ms_per_s if app else None
            fields[f"{prefix}_gpu_ms_per_s"] = app.gpu_ms_per_s if app else None
        return fields


class AppClassifier:
    """Classify application/process names into stable report categories.

    Apple uses the ``com.apple`` namespace for both foreground applications and
    operating-system services. Explicit user-facing identifiers are checked
    before the broad Apple-service rule so Safari, Xcode, Mail, and similar apps
    are not incorrectly hidden in the system-services section.
    """

    BENCHMARK_TOKENS = ("cpu_stress", "gpu_stress", "memory_stress", "macpowerlab")
    MEASUREMENT_TOKENS = ("powermetrics", "mac_power_watch", "app_power_attribution")
    USER_APP_BUNDLE_IDS = {
        "com.apple.Safari",
        "com.apple.finder",
        "com.apple.Terminal",
        "com.apple.dt.Xcode",
        "com.apple.mail",
        "com.apple.Photos",
        "com.apple.MobileSMS",
        "com.apple.Music",
        "com.apple.TV",
        "com.apple.Preview",
        "com.apple.iCal",
        "com.apple.Notes",
        "com.apple.Maps",
        "com.apple.AppStore",
        "com.apple.systempreferences",
        "com.apple.FaceTime",
        "com.apple.podcasts",
        "com.apple.iBooksX",
    }
    USER_APP_NAMES = {
        "Safari",
        "Finder",
        "Terminal",
        "Xcode",
        "Mail",
        "Photos",
        "Messages",
        "Music",
        "TV",
        "Preview",
        "Calendar",
        "Notes",
        "Maps",
        "App Store",
        "System Settings",
        "FaceTime",
        "Podcasts",
        "Books",
    }

    @classmethod
    def classify(cls, raw_name: str, display_name: str) -> str:
        """Return a normalized category for a process or coalition name."""

        combined = f"{raw_name} {display_name}".lower()
        if any(token in combined for token in cls.MEASUREMENT_TOKENS):
            return "measurement"
        if any(token in combined for token in cls.BENCHMARK_TOKENS):
            return "benchmark"
        if raw_name in ("kernel_coalition", "ALL_TASKS", "DEAD_TASKS"):
            return "unattributed" if raw_name != "kernel_coalition" else "system"
        if raw_name in cls.USER_APP_BUNDLE_IDS or display_name in cls.USER_APP_NAMES:
            return "user_app"
        if raw_name.startswith("com.apple.") or display_name in ("WindowServer", "kernel_task", "Kernel"):
            return "system"
        if display_name.endswith("d") and " " not in display_name:
            return "background_service"
        if raw_name.startswith("com.") or raw_name.startswith("org.") or raw_name.startswith("io."):
            return "user_app"
        if display_name and display_name[0].isupper():
            return "user_app"
        return "background_service"


class BundleNameResolver:
    """Resolve bundle identifiers and process paths to friendly application names.

    Resolution is cached and bounded. Spotlight lookup is optional because it can
    add latency on systems that are indexing or have a damaged metadata store.
    """

    KNOWN_NAMES = {
        "com.apple.WindowServer": "WindowServer",
        "kernel_coalition": "Kernel",
        "com.apple.Safari": "Safari",
        "com.google.Chrome": "Google Chrome",
        "com.microsoft.edgemac": "Microsoft Edge",
        "com.microsoft.VSCode": "Visual Studio Code",
        "com.openai.chat": "ChatGPT",
        "com.hnc.Discord": "Discord",
        "com.spotify.client": "Spotify",
        "com.valvesoftware.steam": "Steam",
        "com.apple.finder": "Finder",
        "com.apple.Terminal": "Terminal",
        "com.apple.dt.Xcode": "Xcode",
        "com.apple.mail": "Mail",
        "com.apple.Photos": "Photos",
        "com.apple.MobileSMS": "Messages",
        "com.apple.Music": "Music",
        "com.apple.TV": "TV",
        "com.apple.Preview": "Preview",
        "com.apple.iCal": "Calendar",
        "com.apple.Notes": "Notes",
        "com.apple.Maps": "Maps",
        "com.apple.AppStore": "App Store",
        "com.apple.systempreferences": "System Settings",
        "com.googlecode.iterm2": "iTerm2",
    }

    def __init__(self, enable_spotlight: bool = True, max_cache_entries: int = 512) -> None:
        """Initialize the bounded name resolver.

        Args:
            enable_spotlight: Allow a short ``mdfind`` lookup for unknown bundle
                identifiers. Disable this for the lowest possible overhead.
            max_cache_entries: Maximum resolved-name entries retained in memory.

        Raises:
            AppPowerValidationError: If ``max_cache_entries`` is outside the
                supported range.
        """

        self.enable_spotlight = bool(enable_spotlight)
        self.max_cache_entries = validate_int("max_cache_entries", int(max_cache_entries), 32, 4096)
        self._cache: dict[str, str] = {}
        self._lock = threading.Lock()

    @staticmethod
    def _app_from_path(text: str) -> str | None:
        match = re.search(r"/([^/]+)\.app/(?:Contents/)?", text)
        return match.group(1) if match else None

    @staticmethod
    def _fallback_bundle_name(identifier: str) -> str:
        parts = [part for part in re.split(r"[._-]+", identifier) if part]
        ignored = {"com", "org", "net", "io", "co", "app", "apple"}
        useful = [part for part in parts if part.lower() not in ignored]
        if not useful:
            return identifier
        tail = useful[-2:] if len(useful) > 1 else useful
        return " ".join(part[:1].upper() + part[1:] for part in tail)

    def _spotlight_lookup(self, identifier: str) -> str | None:
        if not self.enable_spotlight or shutil.which("mdfind") is None:
            return None
        if not re.fullmatch(r"[A-Za-z0-9._-]{3,220}", identifier):
            return None
        query = f'kMDItemCFBundleIdentifier == "{identifier}"c'
        try:
            output = subprocess.check_output(
                ["/usr/bin/mdfind", query],
                text=True,
                stderr=subprocess.DEVNULL,
                timeout=1.5,
            )
        except (subprocess.SubprocessError, OSError):
            return None
        for line in output.splitlines():
            path = line.strip()
            if path.endswith(".app"):
                return Path(path).stem
        return None

    def resolve(self, raw_name: str, command_line: str | None = None) -> str:
        """Resolve a raw coalition/process name to a display name."""

        raw_name = _safe_string(raw_name, 240) or "Unknown"
        command_line = _safe_string(command_line, 1024)
        cache_key = f"{raw_name}\0{command_line}"

        with self._lock:
            cached = self._cache.get(cache_key)
        if cached:
            return cached

        result = self.KNOWN_NAMES.get(raw_name)
        if result is None and command_line:
            result = self._app_from_path(command_line)
        if result is None:
            result = self._app_from_path(raw_name)
        if result is None and re.fullmatch(r"(?:[A-Za-z0-9-]+\.)+[A-Za-z0-9_-]+", raw_name):
            result = self._spotlight_lookup(raw_name) or self._fallback_bundle_name(raw_name)
        if result is None:
            result = Path(raw_name).name or raw_name

        with self._lock:
            if len(self._cache) >= self.max_cache_entries:
                self._cache.pop(next(iter(self._cache)))
            self._cache[cache_key] = result
        return result


class PowermetricsTaskSampler:
    """Collect per-app activity using Apple's machine-readable powermetrics output."""

    def __init__(
        self,
        sample_ms: int = 1000,
        timeout_s: float = 12.0,
        max_activities: int = DEFAULT_MAX_ACTIVITIES,
        resolver: BundleNameResolver | None = None,
    ) -> None:
        """Initialize a bounded machine-readable ``powermetrics`` sampler.

        Args:
            sample_ms: Duration of each process-activity sample in milliseconds.
            timeout_s: Absolute subprocess timeout for one sampling attempt.
            max_activities: Maximum parsed apps/coalitions retained per sample.
            resolver: Optional shared application-name resolver.

        Raises:
            AppPowerValidationError: If a numeric setting is outside its safe
                production range.
        """

        self.sample_ms = validate_int("sample_ms", int(sample_ms), 250, 60_000)
        self.timeout_s = validate_float("timeout_s", float(timeout_s), 2.0, 120.0)
        self.max_activities = validate_int("max_activities", int(max_activities), 10, 1000)
        self.resolver = resolver or BundleNameResolver()

    @staticmethod
    def _sudo_prefix() -> list[str]:
        return [] if os.geteuid() == 0 else ["/usr/bin/sudo", "-n"]

    def _commands(self) -> list[list[str]]:
        base = self._sudo_prefix() + [
            "/usr/bin/powermetrics",
            "-f",
            "plist",
            "-n",
            "1",
            "-i",
            str(self.sample_ms),
            "--samplers",
            "tasks",
        ]
        # ``--show-process-energy`` implicitly enables process CPU, GPU, disk,
        # network, and wakeup metrics on current macOS builds. AMP/IPC details
        # are intentionally not requested here because they substantially grow
        # each plist and are already collected by the main CPU sampler where
        # supported.
        return [
            base
            + [
                "--show-process-coalition",
                "--show-responsible-pid",
                "--show-process-energy",
                "--show-process-samp-norm",
                "--handle-invalid-values",
            ],
            base
            + [
                "--show-process-coalition",
                "--show-responsible-pid",
                "--show-process-energy",
                "--show-process-samp-norm",
            ],
            base + ["--show-responsible-pid", "--show-process-energy", "--show-process-samp-norm"],
        ]

    @staticmethod
    def _decode_plists(data: bytes) -> list[dict[str, Any]]:
        """Decode NUL-separated XML/binary plist samples."""

        objects: list[dict[str, Any]] = []

        # A single plist may be XML or binary. Parse the complete payload first
        # so internal NUL bytes in a binary plist are never treated as record
        # separators. powermetrics normally emits NUL-separated XML plists; that
        # path is handled only after the whole-payload attempt fails.
        try:
            whole = plistlib.loads(data)
        except Exception:
            whole = None
        if isinstance(whole, dict):
            return [whole]

        chunks = [chunk.strip() for chunk in data.split(b"\x00") if chunk.strip()]
        if not chunks:
            chunks = [data]
        for chunk in chunks:
            try:
                obj = plistlib.loads(chunk)
            except Exception:
                continue
            if isinstance(obj, dict):
                objects.append(obj)
        if not objects:
            raise AppPowerSamplingError("powermetrics returned no decodable plist sample")
        return objects

    @staticmethod
    def _scalar_extras(record: Mapping[str, Any], limit: int = 32) -> dict[str, Any]:
        """Retain a bounded set of scalar metrics for future compatibility."""

        result: dict[str, Any] = {}
        for key, value in record.items():
            if len(result) >= limit:
                break
            if isinstance(value, (str, int, float, bool)) or value is None:
                result[str(key)] = value
        return result

    @staticmethod
    def _composite_score(
        cpu_ms_per_s: float | None,
        gpu_ms_per_s: float | None,
        idle_wakeups_per_s: float | None,
        intr_wakeups_per_s: float | None,
        disk_read_bps: float | None,
        disk_write_bps: float | None,
        network_rx_bps: float | None,
        network_tx_bps: float | None,
    ) -> float:
        """Compute a bounded fallback activity score when Energy Impact is absent."""

        cpu = max(0.0, cpu_ms_per_s or 0.0)
        gpu = max(0.0, gpu_ms_per_s or 0.0) * 2.0
        idle = max(0.0, idle_wakeups_per_s or 0.0) * 0.75
        intr = max(0.0, intr_wakeups_per_s or 0.0) * 0.08
        disk_mib = max(0.0, (disk_read_bps or 0.0) + (disk_write_bps or 0.0)) / (1024.0 * 1024.0)
        net_mib = max(0.0, (network_rx_bps or 0.0) + (network_tx_bps or 0.0)) / (1024.0 * 1024.0)
        return cpu + gpu + idle + intr + disk_mib * 3.0 + net_mib

    def _activity_from_record(
        self,
        record: Mapping[str, Any],
        source: str,
        sample_window_s: float,
        resolve_name: bool,
    ) -> AppActivity | None:
        sample_process_name = _safe_string(record.get("name") or record.get("process_name") or record.get("command"), 240)
        raw_name = _safe_string(record.get("_responsible_name") or record.get("responsible_name"), 240) or sample_process_name
        if not raw_name:
            return None

        # ``ALL_TASKS`` is an aggregate row in text/plist variants. Including it
        # alongside individual tasks or coalitions would double-count activity
        # and distort every application's estimated share. ``DEAD_TASKS`` is
        # retained because it represents otherwise-unattributed transient work.
        if raw_name.upper() in {"ALL_TASKS", "ALL_PROCESSES"}:
            return None

        pid = _finite_int(record.get("pid") if "pid" in record else record.get("id"))
        responsible_pid = _finite_int(
            record.get("responsible_pid")
            if "responsible_pid" in record
            else record.get("responsiblePid", record.get("responsible_id"))
        )
        coalition_id = _finite_int(record.get("coalition_id") if "coalition_id" in record else record.get("id"))
        command_line = _safe_string(record.get("command") or record.get("args"), 1024)
        display_name = self.resolver.resolve(raw_name, command_line) if resolve_name else self.resolver.KNOWN_NAMES.get(raw_name, raw_name)
        category = AppClassifier.classify(raw_name, display_name)

        energy_impact = _first_number(record, ("energy_impact", "energyImpact"))
        energy_impact_per_s = _first_number(record, ("energy_impact_per_s", "energyImpactPerS"))
        cpu_ms_per_s = _first_number(
            record,
            (
                "cputime_sample_ms_per_s",
                "cputime_ms_per_s",
                "cpu_time_ms_per_s",
                "cpu_ms_per_s",
            ),
        )
        gpu_ms_per_s = _first_number(record, ("gputime_ms_per_s", "gpu_time_ms_per_s"))
        if gpu_ms_per_s is None:
            gpu_ms = _first_number(record, ("gputime_ms", "gpu_time_ms"))
            gpu_ms_per_s = gpu_ms / sample_window_s if gpu_ms is not None and sample_window_s > 0 else None

        userland_ratio = _first_number(record, ("cputime_userland_ratio", "userland_ratio"))
        intr_wakeups_per_s = _first_number(record, ("intr_wakeups_per_s", "interrupt_wakeups_per_s"))
        if intr_wakeups_per_s is None:
            raw = _first_number(record, ("intr_wakeups", "interrupt_wakeups"))
            intr_wakeups_per_s = raw / sample_window_s if raw is not None and sample_window_s > 0 else None
        idle_wakeups_per_s = _first_number(record, ("idle_wakeups_per_s", "package_idle_wakeups_per_s"))
        if idle_wakeups_per_s is None:
            raw = _first_number(record, ("idle_wakeups", "package_idle_wakeups"))
            idle_wakeups_per_s = raw / sample_window_s if raw is not None and sample_window_s > 0 else None

        disk_read_bps = _first_number(record, ("diskio_bytesread_per_s", "disk_read_bytes_per_s"))
        if disk_read_bps is None:
            raw = _first_number(record, ("diskio_bytesread", "disk_read_bytes"))
            disk_read_bps = raw / sample_window_s if raw is not None and sample_window_s > 0 else None
        disk_write_bps = _first_number(record, ("diskio_byteswritten_per_s", "disk_write_bytes_per_s"))
        if disk_write_bps is None:
            raw = _first_number(record, ("diskio_byteswritten", "disk_write_bytes"))
            disk_write_bps = raw / sample_window_s if raw is not None and sample_window_s > 0 else None

        network_rx_bps = _first_number(record, ("bytes_received_per_s", "network_rx_bytes_per_s"))
        if network_rx_bps is None:
            raw = _first_number(record, ("bytes_received", "network_rx_bytes"))
            network_rx_bps = raw / sample_window_s if raw is not None and sample_window_s > 0 else None
        network_tx_bps = _first_number(record, ("bytes_sent_per_s", "network_tx_bytes_per_s"))
        if network_tx_bps is None:
            raw = _first_number(record, ("bytes_sent", "network_tx_bytes"))
            network_tx_bps = raw / sample_window_s if raw is not None and sample_window_s > 0 else None

        if energy_impact_per_s is not None and energy_impact_per_s > 0:
            score = energy_impact_per_s
        elif energy_impact is not None and energy_impact > 0:
            score = energy_impact
        else:
            score = self._composite_score(
                cpu_ms_per_s,
                gpu_ms_per_s,
                idle_wakeups_per_s,
                intr_wakeups_per_s,
                disk_read_bps,
                disk_write_bps,
                network_rx_bps,
                network_tx_bps,
            )
        score = max(0.0, score or 0.0)

        if source == "powermetrics-coalition":
            key = f"coalition:{raw_name}"
        elif responsible_pid is not None and responsible_pid > 0:
            key = f"responsible:{responsible_pid}"
        else:
            key = f"task:{raw_name}:{pid if pid is not None else 'unknown'}"

        extras = self._scalar_extras(record)
        if sample_process_name and sample_process_name != raw_name:
            extras["sample_process_name"] = sample_process_name

        return AppActivity(
            key=key,
            raw_name=raw_name,
            display_name=display_name,
            category=category,
            pid=pid if source != "powermetrics-coalition" else None,
            coalition_id=coalition_id if source == "powermetrics-coalition" else None,
            responsible_pid=responsible_pid if source != "powermetrics-coalition" else None,
            energy_impact=energy_impact,
            energy_impact_per_s=energy_impact_per_s,
            cpu_ms_per_s=cpu_ms_per_s,
            gpu_ms_per_s=gpu_ms_per_s,
            userland_ratio=userland_ratio,
            intr_wakeups_per_s=intr_wakeups_per_s,
            idle_wakeups_per_s=idle_wakeups_per_s,
            disk_read_bytes_per_s=disk_read_bps,
            disk_write_bytes_per_s=disk_write_bps,
            network_rx_bytes_per_s=network_rx_bps,
            network_tx_bytes_per_s=network_tx_bps,
            score=score,
            source=source,
            extra_metrics=extras,
        )

    @staticmethod
    def _merge(activities: Iterable[AppActivity]) -> list[AppActivity]:
        """Merge duplicate keys while preserving additive resource metrics."""

        merged: dict[str, AppActivity] = {}
        for activity in activities:
            existing = merged.get(activity.key)
            if existing is None:
                merged[activity.key] = activity
                continue
            for field_name in (
                "energy_impact",
                "energy_impact_per_s",
                "cpu_ms_per_s",
                "gpu_ms_per_s",
                "intr_wakeups_per_s",
                "idle_wakeups_per_s",
                "disk_read_bytes_per_s",
                "disk_write_bytes_per_s",
                "network_rx_bytes_per_s",
                "network_tx_bytes_per_s",
                "score",
            ):
                current = getattr(existing, field_name)
                incoming = getattr(activity, field_name)
                if current is None:
                    setattr(existing, field_name, incoming)
                elif incoming is not None:
                    setattr(existing, field_name, current + incoming)
        return list(merged.values())

    def sample(self, sample_id: int) -> ActivitySnapshot:
        """Run one bounded powermetrics sample and parse app/coalition activity."""

        if shutil.which("powermetrics") is None:
            raise AppPowerSamplingError("/usr/bin/powermetrics is not available")

        errors: list[str] = []
        for command in self._commands():
            try:
                output = subprocess.check_output(
                    command,
                    stderr=subprocess.STDOUT,
                    timeout=max(self.timeout_s, self.sample_ms / 1000.0 + 4.0),
                )
                objects = self._decode_plists(output)
                sample = objects[-1]
                elapsed_ns = _finite_float(sample.get("elapsed_ns"))
                sample_window_s = elapsed_ns / 1_000_000_000.0 if elapsed_ns and elapsed_ns > 0 else self.sample_ms / 1000.0

                records: list[Mapping[str, Any]]
                source: str
                if isinstance(sample.get("coalitions"), list) and sample.get("coalitions"):
                    records = [item for item in sample["coalitions"] if isinstance(item, dict)]
                    source = "powermetrics-coalition"
                elif isinstance(sample.get("tasks"), list) and sample.get("tasks"):
                    records = [item for item in sample["tasks"] if isinstance(item, dict)]
                    source = "powermetrics-task"
                else:
                    raise AppPowerSamplingError("powermetrics plist did not contain coalitions or tasks")

                responsible_names: dict[int, str] = {}
                if source == "powermetrics-task":
                    for record in records:
                        record_pid = _finite_int(record.get("pid") if "pid" in record else record.get("id"))
                        record_name = _safe_string(record.get("name") or record.get("process_name") or record.get("command"), 240)
                        if record_pid is not None and record_name:
                            responsible_names[record_pid] = record_name

                preliminary: list[AppActivity] = []
                for record in records:
                    normalized_record: Mapping[str, Any] = record
                    if source == "powermetrics-task":
                        responsible_pid = _finite_int(
                            record.get("responsible_pid")
                            if "responsible_pid" in record
                            else record.get("responsiblePid", record.get("responsible_id"))
                        )
                        responsible_name = responsible_names.get(responsible_pid) if responsible_pid is not None else None
                        if responsible_name:
                            normalized = dict(record)
                            normalized["_responsible_name"] = responsible_name
                            normalized_record = normalized
                    activity = self._activity_from_record(normalized_record, source, sample_window_s, resolve_name=False)
                    if activity is not None and activity.score > 0:
                        preliminary.append(activity)
                preliminary = self._merge(preliminary)
                preliminary.sort(key=lambda item: item.score, reverse=True)
                preliminary = preliminary[: self.max_activities]

                # Resolve friendly names only after sorting to avoid hundreds of
                # Spotlight lookups on the first sample.
                for index, activity in enumerate(preliminary):
                    if index < 40:
                        activity.display_name = self.resolver.resolve(activity.raw_name)
                        activity.category = AppClassifier.classify(activity.raw_name, activity.display_name)

                return ActivitySnapshot(
                    sample_id=sample_id,
                    timestamp=_now_iso(),
                    monotonic_time=time.monotonic(),
                    sample_window_s=sample_window_s,
                    source=source,
                    status="ok",
                    activities=preliminary,
                    raw_record_count=len(records),
                )
            except subprocess.CalledProcessError as exc:
                output = exc.output.decode("utf-8", errors="replace") if isinstance(exc.output, bytes) else str(exc.output)
                errors.append(_safe_string(output, 600))
            except (subprocess.TimeoutExpired, OSError, plistlib.InvalidFileException, AppPowerSamplingError) as exc:
                errors.append(_safe_string(exc, 600))

        message = "; ".join(error for error in errors if error) or "powermetrics app sampler failed"
        if "password" in message.lower() or "sudo" in message.lower():
            message = f"sudo authorization required or expired: {message}"
        raise AppPowerSamplingError(message)


class PsFallbackSampler:
    """Low-overhead, low-confidence fallback based on ``ps`` CPU/RSS usage."""

    def __init__(self, max_activities: int = DEFAULT_MAX_ACTIVITIES, resolver: BundleNameResolver | None = None) -> None:
        """Initialize the low-confidence process-table fallback sampler.

        Args:
            max_activities: Maximum grouped process/application records retained.
            resolver: Optional shared application-name resolver.

        Raises:
            AppPowerValidationError: If ``max_activities`` is invalid.
        """

        self.max_activities = validate_int("max_activities", int(max_activities), 10, 1000)
        self.resolver = resolver or BundleNameResolver(enable_spotlight=False)

    @staticmethod
    def _app_name_from_args(arguments: str, command: str) -> tuple[str, str]:
        app_match = re.search(r"/([^/]+)\.app/(?:Contents/)?", arguments)
        if app_match:
            name = app_match.group(1)
            return f"app:{name}", name
        base = Path(command).name or command or "Unknown"
        return f"process:{base}", base

    def sample(self, sample_id: int, primary_error: str | None = None) -> ActivitySnapshot:
        """Collect one process table and group it by application executable."""

        try:
            output = subprocess.check_output(
                ["/bin/ps", "-axo", "pid=,ppid=,pcpu=,pmem=,rss=,comm=,args="],
                text=True,
                stderr=subprocess.STDOUT,
                timeout=4.0,
            )
        except (subprocess.SubprocessError, OSError) as exc:
            raise AppPowerSamplingError(f"ps fallback failed: {exc}") from exc

        grouped: dict[str, MutableMapping[str, Any]] = {}
        for line in output.splitlines():
            parts = line.strip().split(None, 6)
            if len(parts) < 7:
                continue
            pid = _finite_int(parts[0])
            cpu_percent = _finite_float(parts[2]) or 0.0
            memory_percent = _finite_float(parts[3]) or 0.0
            rss_kb = _finite_float(parts[4]) or 0.0
            command = parts[5]
            arguments = parts[6]
            key, raw_name = self._app_name_from_args(arguments, command)
            item = grouped.setdefault(
                key,
                {
                    "raw_name": raw_name,
                    "pid": pid,
                    "cpu_percent": 0.0,
                    "memory_percent": 0.0,
                    "rss_kb": 0.0,
                    "process_count": 0,
                    "arguments": arguments,
                },
            )
            item["cpu_percent"] += max(0.0, cpu_percent)
            item["memory_percent"] += max(0.0, memory_percent)
            item["rss_kb"] += max(0.0, rss_kb)
            item["process_count"] += 1

        activities: list[AppActivity] = []
        for key, item in grouped.items():
            raw_name = _safe_string(item["raw_name"], 240)
            display_name = self.resolver.resolve(raw_name, _safe_string(item["arguments"], 1024))
            cpu_percent = _finite_float(item["cpu_percent"]) or 0.0
            rss_mb = (_finite_float(item["rss_kb"]) or 0.0) / 1024.0
            score = max(0.0, cpu_percent) + min(20.0, rss_mb / 256.0)
            if score <= 0:
                continue
            activities.append(
                AppActivity(
                    key=key,
                    raw_name=raw_name,
                    display_name=display_name,
                    category=AppClassifier.classify(raw_name, display_name),
                    pid=_finite_int(item["pid"]),
                    cpu_ms_per_s=cpu_percent * 10.0,
                    score=score,
                    source="ps-fallback",
                    extra_metrics={
                        "cpu_percent": cpu_percent,
                        "memory_percent": item["memory_percent"],
                        "rss_mb": rss_mb,
                        "process_count": item["process_count"],
                    },
                )
            )

        activities.sort(key=lambda item: item.score, reverse=True)
        return ActivitySnapshot(
            sample_id=sample_id,
            timestamp=_now_iso(),
            monotonic_time=time.monotonic(),
            sample_window_s=1.0,
            source="ps-fallback",
            status="degraded",
            activities=activities[: self.max_activities],
            error=_safe_string(primary_error, 800) or None,
            raw_record_count=len(grouped),
        )


class AppPowerWorker:
    """Threaded sampler that atomically publishes the latest app activity snapshot."""

    def __init__(
        self,
        interval_s: float = 5.0,
        sample_ms: int = 1000,
        max_activities: int = DEFAULT_MAX_ACTIVITIES,
        resolve_bundles: bool = True,
    ) -> None:
        """Initialize the single background app-activity worker.

        Args:
            interval_s: Delay between app-activity samples.
            sample_ms: Duration of each ``powermetrics`` sample.
            max_activities: Maximum app/coalition records retained.
            resolve_bundles: Enable bounded Spotlight name resolution.

        Raises:
            AppPowerValidationError: If any setting is outside its safe range.
        """

        self.interval_s = validate_float("interval_s", float(interval_s), 2.0, 300.0)
        self.sample_ms = validate_int("sample_ms", int(sample_ms), 250, 60_000)
        self.max_activities = validate_int("max_activities", int(max_activities), 10, 1000)
        resolver = BundleNameResolver(enable_spotlight=resolve_bundles)
        self.primary_sampler = PowermetricsTaskSampler(
            sample_ms=self.sample_ms,
            max_activities=self.max_activities,
            resolver=resolver,
        )
        self.fallback_sampler = PsFallbackSampler(max_activities=self.max_activities, resolver=resolver)
        self._snapshot = ActivitySnapshot.starting()
        self._snapshot_lock = threading.Lock()
        self._stop_event = threading.Event()
        self._thread = threading.Thread(target=self._run, name="MacPowerLabAppPower", daemon=True)
        self._sample_id = 0

    def start(self) -> None:
        """Start the background sampler once."""

        if not self._thread.is_alive():
            self._thread.start()

    def stop(self) -> None:
        """Request background shutdown."""

        self._stop_event.set()

    def join(self, timeout: float = 3.0) -> None:
        """Wait briefly for the worker to terminate."""

        if self._thread.is_alive():
            self._thread.join(timeout=max(0.0, float(timeout)))

    def snapshot(self) -> ActivitySnapshot:
        """Return the latest atomically published snapshot."""

        with self._snapshot_lock:
            return self._snapshot

    def _publish(self, snapshot: ActivitySnapshot) -> None:
        with self._snapshot_lock:
            self._snapshot = snapshot

    def _run(self) -> None:
        while not self._stop_event.is_set():
            started = time.monotonic()
            self._sample_id += 1
            try:
                snapshot = self.primary_sampler.sample(self._sample_id)
            except AppPowerSamplingError as primary_error:
                try:
                    snapshot = self.fallback_sampler.sample(self._sample_id, str(primary_error))
                except AppPowerSamplingError as fallback_error:
                    snapshot = ActivitySnapshot(
                        sample_id=self._sample_id,
                        timestamp=_now_iso(),
                        monotonic_time=time.monotonic(),
                        sample_window_s=0.0,
                        source="none",
                        status="unavailable",
                        activities=[],
                        error=f"{primary_error}; {fallback_error}",
                    )
            self._publish(snapshot)
            elapsed = time.monotonic() - started
            self._stop_event.wait(max(0.1, self.interval_s - elapsed))


class AppPowerAttributionEngine:
    """Allocate measured total system power across apps using activity shares."""

    HIGH_TRUST_TOTAL_SOURCES = {"battery discharge watts", "BMS SystemPower"}
    MEDIUM_TRUST_TOTAL_TOKENS = ("PowerTelemetry", "adapter draw", "system load")

    def __init__(
        self,
        top_slots: int = DEFAULT_TOP_SLOTS,
        min_score: float = 0.001,
        quiet_history_size: int = 240,
    ) -> None:
        """Initialize the component-aware attribution model.

        Args:
            top_slots: Number of top applications exported to the live CSV/UI.
            min_score: Minimum positive app activity score eligible for allocation.
            quiet_history_size: Bounded number of quiet-load samples retained per
                power-source/telemetry-source pair for baseline calibration.

        Raises:
            AppPowerValidationError: If a model setting is invalid.
        """

        self.top_slots = validate_int("top_slots", int(top_slots), 1, 12)
        self.min_score = validate_float("min_score", float(min_score), 0.0, 1_000_000.0)
        self.quiet_history_size = validate_int("quiet_history_size", int(quiet_history_size), 20, 5000)
        self._quiet_power_by_source: dict[str, deque[float]] = defaultdict(
            lambda: deque(maxlen=self.quiet_history_size)
        )
        self._last_result: AttributionResult | None = None

    @staticmethod
    def _total_power(row: Mapping[str, Any]) -> tuple[float | None, str]:
        candidates = (
            (row.get("primary_total_load_w"), _safe_string(row.get("primary_total_load_source"), 160)),
            (row.get("actual_battery_draw_w"), "battery discharge watts"),
            (row.get("bms_system_power_w"), "BMS SystemPower"),
            (row.get("telemetry_system_effective_total_load_w"), "PowerTelemetry SystemEffectiveTotalLoad"),
            (row.get("soc_power_w"), "powermetrics SoC/component estimate"),
        )
        for value, source in candidates:
            numeric = _finite_float(value)
            if numeric is not None and numeric >= 0:
                return numeric, source or "unknown total power source"
        return None, "no total power source"

    @staticmethod
    def _is_quiet_sample(row: Mapping[str, Any]) -> bool:
        phase = f"{row.get('phase') or ''} {row.get('auto_phase') or ''}".lower()
        if any(token in phase for token in ("stress", "load", "max", "benchmark", "soak")):
            return False
        stress_cpu = _finite_float(row.get("stress_cpu_percent")) or 0.0
        return stress_cpu < 40.0

    def _baseline(
        self,
        row: Mapping[str, Any],
        total_power_w: float | None,
        total_source: str,
    ) -> tuple[float | None, bool]:
        """Return ``(baseline_watts, established)`` for the current power source.

        A zero fallback keeps dynamic-power arithmetic stable while the initial
        baseline is being learned. The separate ``established`` flag prevents
        that fallback from being reported as high-confidence calibration.
        """

        if total_power_w is None:
            return None, False

        key = f"{row.get('power_source') or 'unknown'}::{total_source}"
        if self._is_quiet_sample(row):
            self._quiet_power_by_source[key].append(total_power_w)

        explicit = _finite_float(row.get("baseline_primary_total_load_w"))
        explicit_source = _safe_string(row.get("baseline_power_source"), 80)
        current_source = _safe_string(row.get("power_source"), 80)
        if explicit is not None and (not explicit_source or explicit_source == current_source):
            return _clamp(explicit, 0.0, total_power_w), True

        history = list(self._quiet_power_by_source.get(key, ()))
        baseline = _percentile(history, 0.20) if len(history) >= 8 else None
        if baseline is not None:
            return _clamp(baseline, 0.0, total_power_w), True
        return 0.0, False

    @classmethod
    def _confidence(cls, snapshot: ActivitySnapshot, total_source: str, baseline_available: bool) -> str:
        if snapshot.source == "powermetrics-coalition" and total_source in cls.HIGH_TRUST_TOTAL_SOURCES and baseline_available:
            return "high"
        if snapshot.source.startswith("powermetrics") and (
            total_source in cls.HIGH_TRUST_TOTAL_SOURCES
            or any(token in total_source for token in cls.MEDIUM_TRUST_TOTAL_TOKENS)
        ):
            return "medium"
        return "low"

    @staticmethod
    def _component_pools(
        row: Mapping[str, Any],
        dynamic_power_w: float | None,
    ) -> tuple[float | None, float | None, float | None, str]:
        """Return bounded CPU, GPU, and residual dynamic-power pools.

        Component watts are used only when they are finite, non-negative, and
        fit inside the measured dynamic-power envelope. Any unavailable or
        untrusted component power remains in the residual pool and is allocated
        using Energy Impact. This preserves the total-power invariant while
        using more detailed telemetry when macOS exposes it.
        """

        if dynamic_power_w is None:
            return None, None, None, "activity-share only; total power unavailable"

        remaining = max(0.0, dynamic_power_w)
        cpu_raw = _finite_float(row.get("cpu_power_w"))
        gpu_raw = _finite_float(row.get("gpu_power_w"))
        cpu_baseline = _finite_float(row.get("baseline_cpu_power_w"))
        gpu_baseline = _finite_float(row.get("baseline_gpu_power_w"))

        cpu_dynamic = max(0.0, (cpu_raw or 0.0) - (cpu_baseline or 0.0))
        gpu_dynamic = max(0.0, (gpu_raw or 0.0) - (gpu_baseline or 0.0))
        cpu_pool = min(cpu_dynamic, remaining)
        remaining -= cpu_pool
        gpu_pool = min(gpu_dynamic, remaining)
        remaining -= gpu_pool

        has_cpu = cpu_raw is not None and cpu_dynamic > 0
        has_gpu = gpu_raw is not None and gpu_dynamic > 0
        if has_cpu or has_gpu:
            baseline_note = "baseline-adjusted " if cpu_baseline is not None or gpu_baseline is not None else ""
            method = f"component-aware: {baseline_note}CPU/GPU time plus Energy Impact residual"
        else:
            method = "Energy Impact proportional allocation"
        return cpu_pool, gpu_pool, remaining, method

    def attribute(self, row: Mapping[str, Any], snapshot: ActivitySnapshot) -> AttributionResult:
        """Attribute current system power to activities in ``snapshot``.

        The activity snapshot may remain constant for several monitor refreshes,
        but total system power can change during that interval. Allocation is
        therefore recalculated on every monitor row. ``AppPowerSessionLogger``
        still de-duplicates by ``sample_id`` so disk I/O remains bounded.
        """

        total_power_w, total_source = self._total_power(row)
        baseline_power_w, baseline_established = self._baseline(row, total_power_w, total_source)
        dynamic_power_w = (
            max(0.0, total_power_w - (baseline_power_w or 0.0))
            if total_power_w is not None
            else None
        )

        eligible = [activity for activity in snapshot.activities if activity.score >= self.min_score]
        total_score = sum(max(0.0, activity.score) for activity in eligible)
        total_cpu_score = sum(max(0.0, activity.cpu_ms_per_s or 0.0) for activity in eligible)
        total_gpu_score = sum(max(0.0, activity.gpu_ms_per_s or 0.0) for activity in eligible)
        cpu_pool_w, gpu_pool_w, residual_pool_w, attribution_method = self._component_pools(row, dynamic_power_w)
        apps: list[AttributedApp] = []

        if total_score > 0:
            for activity in eligible:
                energy_share = max(0.0, activity.score) / total_score
                cpu_share = (
                    max(0.0, activity.cpu_ms_per_s or 0.0) / total_cpu_score
                    if total_cpu_score > 0
                    else energy_share
                )
                gpu_share = (
                    max(0.0, activity.gpu_ms_per_s or 0.0) / total_gpu_score
                    if total_gpu_score > 0
                    else energy_share
                )

                cpu_w = cpu_pool_w * cpu_share if cpu_pool_w is not None else None
                gpu_w = gpu_pool_w * gpu_share if gpu_pool_w is not None else None
                residual_w = residual_pool_w * energy_share if residual_pool_w is not None else None
                dynamic_w = (
                    (cpu_w or 0.0) + (gpu_w or 0.0) + (residual_w or 0.0)
                    if dynamic_power_w is not None
                    else None
                )
                baseline_share_w = baseline_power_w * energy_share if baseline_power_w is not None else None
                share_w = (
                    (baseline_share_w or 0.0) + (dynamic_w or 0.0)
                    if total_power_w is not None
                    else None
                )

                apps.append(
                    AttributedApp(
                        key=activity.key,
                        raw_name=activity.raw_name,
                        display_name=activity.display_name,
                        category=activity.category,
                        pid=activity.pid,
                        coalition_id=activity.coalition_id,
                        responsible_pid=activity.responsible_pid,
                        source=activity.source,
                        energy_impact=activity.energy_impact,
                        energy_impact_per_s=activity.energy_impact_per_s,
                        cpu_ms_per_s=activity.cpu_ms_per_s,
                        gpu_ms_per_s=activity.gpu_ms_per_s,
                        intr_wakeups_per_s=activity.intr_wakeups_per_s,
                        idle_wakeups_per_s=activity.idle_wakeups_per_s,
                        disk_read_bytes_per_s=activity.disk_read_bytes_per_s,
                        disk_write_bytes_per_s=activity.disk_write_bytes_per_s,
                        network_rx_bytes_per_s=activity.network_rx_bytes_per_s,
                        network_tx_bytes_per_s=activity.network_tx_bytes_per_s,
                        activity_score=activity.score,
                        energy_share_percent=energy_share * 100.0,
                        estimated_share_w=share_w,
                        estimated_dynamic_w=dynamic_w,
                        estimated_cpu_w=cpu_w,
                        estimated_gpu_w=gpu_w,
                        estimated_residual_w=residual_w,
                    )
                )

        apps.sort(
            key=lambda item: (
                item.estimated_share_w if item.estimated_share_w is not None else item.activity_score,
                item.activity_score,
            ),
            reverse=True,
        )

        attributed_power_w = (
            sum(app.estimated_share_w or 0.0 for app in apps)
            if total_power_w is not None
            else None
        )
        unattributed_power_w = (
            max(0.0, total_power_w - (attributed_power_w or 0.0))
            if total_power_w is not None
            else None
        )
        sample_age_s = max(0.0, time.monotonic() - snapshot.monotonic_time)
        result = AttributionResult(
            sample_id=snapshot.sample_id,
            timestamp=snapshot.timestamp,
            sample_monotonic_time=snapshot.monotonic_time,
            sample_window_s=snapshot.sample_window_s,
            sample_age_s=sample_age_s,
            status=snapshot.status,
            source=snapshot.source,
            confidence=self._confidence(snapshot, total_source, baseline_established),
            total_system_power_w=total_power_w,
            total_power_source=total_source,
            baseline_power_w=baseline_power_w,
            dynamic_power_w=dynamic_power_w,
            cpu_component_pool_w=cpu_pool_w,
            gpu_component_pool_w=gpu_pool_w,
            residual_dynamic_pool_w=residual_pool_w,
            attribution_method=attribution_method,
            attributed_power_w=attributed_power_w,
            unattributed_power_w=unattributed_power_w,
            apps=apps,
            error=snapshot.error,
        )
        self._last_result = result
        return result


class AppPowerSessionLogger:
    """Stream app-power samples and maintain incremental per-app summaries."""

    SUMMARY_FIELDS = (
        "key",
        "display_name",
        "raw_name",
        "category",
        "source",
        "sample_count",
        "observed_seconds",
        "estimated_share_wh",
        "estimated_dynamic_wh",
        "estimated_cpu_wh",
        "estimated_gpu_wh",
        "estimated_residual_wh",
        "average_estimated_w",
        "peak_estimated_w",
        "average_dynamic_w",
        "peak_dynamic_w",
        "average_estimated_cpu_w",
        "peak_estimated_cpu_w",
        "average_estimated_gpu_w",
        "peak_estimated_gpu_w",
        "average_energy_impact",
        "peak_energy_impact",
        "average_cpu_ms_per_s",
        "peak_cpu_ms_per_s",
        "average_gpu_ms_per_s",
        "peak_gpu_ms_per_s",
        "average_intr_wakeups_per_s",
        "peak_intr_wakeups_per_s",
        "average_idle_wakeups_per_s",
        "peak_idle_wakeups_per_s",
        "disk_read_bytes",
        "disk_write_bytes",
        "network_rx_bytes",
        "network_tx_bytes",
    )

    def __init__(
        self,
        base_dir: str | os.PathLike[str],
        run_stem: str,
        enabled: bool = True,
        flush_every: int = 3,
    ) -> None:
        """Initialize streaming app-power persistence for one monitor run.

        Args:
            base_dir: Directory for JSONL and incremental summary files.
            run_stem: File-name stem shared with the main power-monitor run.
            enabled: Disable all disk writes when ``False``.
            flush_every: Number of unique activity samples between atomic summary
                rewrites.

        Raises:
            AppPowerValidationError: If ``flush_every`` is outside its safe range.
            OSError: If an enabled output stream cannot be created.
        """

        self.enabled = bool(enabled)
        self.base_dir = Path(base_dir)
        self.run_stem = _safe_string(run_stem, 180) or f"mac_power_{datetime.now().strftime('%Y%m%d_%H%M%S')}"
        self.flush_every = validate_int("flush_every", int(flush_every), 1, 120)
        self.jsonl_path = self.base_dir / f"{self.run_stem}_apps.jsonl"
        self.summary_json_path = self.base_dir / f"{self.run_stem}_apps_summary.json"
        self.summary_csv_path = self.base_dir / f"{self.run_stem}_apps_summary.csv"
        self._jsonl_handle = None
        self._last_sample_id: int | None = None
        self._last_sample_monotonic: float | None = None
        self._sample_count = 0
        self._observed_seconds = 0.0
        self._estimated_system_wh = 0.0
        self._created_at = _now_iso()
        self._aggregates: dict[str, dict[str, Any]] = {}
        self._confidence_counts: dict[str, int] = defaultdict(int)
        self._source_counts: dict[str, int] = defaultdict(int)
        self._error_count = 0

        if self.enabled:
            self.base_dir.mkdir(parents=True, exist_ok=True)
            self._jsonl_handle = self.jsonl_path.open("a", encoding="utf-8")

    @staticmethod
    def _average(total: float, count: int) -> float | None:
        return total / count if count > 0 else None

    def _interval_seconds(self, result: AttributionResult) -> float:
        if self._last_sample_monotonic is None:
            return _clamp(result.sample_window_s or 1.0, 0.1, 60.0)
        elapsed = result.sample_monotonic_time - self._last_sample_monotonic
        return _clamp(elapsed, 0.1, 60.0)

    def _update_aggregate(self, app: AttributedApp, interval_s: float) -> None:
        aggregate = self._aggregates.setdefault(
            app.key,
            {
                "key": app.key,
                "display_name": app.display_name,
                "raw_name": app.raw_name,
                "category": app.category,
                "source": app.source,
                "sample_count": 0,
                "observed_seconds": 0.0,
                "estimated_share_wh": 0.0,
                "estimated_dynamic_wh": 0.0,
                "estimated_cpu_wh": 0.0,
                "estimated_gpu_wh": 0.0,
                "estimated_residual_wh": 0.0,
                "estimated_watt_seconds": 0.0,
                "dynamic_watt_seconds": 0.0,
                "peak_estimated_w": 0.0,
                "peak_dynamic_w": 0.0,
                "estimated_cpu_watt_seconds": 0.0,
                "peak_estimated_cpu_w": 0.0,
                "estimated_gpu_watt_seconds": 0.0,
                "peak_estimated_gpu_w": 0.0,
                "energy_impact_total": 0.0,
                "energy_impact_count": 0,
                "peak_energy_impact": 0.0,
                "cpu_ms_per_s_total": 0.0,
                "cpu_ms_per_s_count": 0,
                "peak_cpu_ms_per_s": 0.0,
                "gpu_ms_per_s_total": 0.0,
                "gpu_ms_per_s_count": 0,
                "peak_gpu_ms_per_s": 0.0,
                "intr_wakeups_per_s_total": 0.0,
                "intr_wakeups_per_s_count": 0,
                "peak_intr_wakeups_per_s": 0.0,
                "idle_wakeups_per_s_total": 0.0,
                "idle_wakeups_per_s_count": 0,
                "peak_idle_wakeups_per_s": 0.0,
                "disk_read_bytes": 0.0,
                "disk_write_bytes": 0.0,
                "network_rx_bytes": 0.0,
                "network_tx_bytes": 0.0,
            },
        )
        aggregate["display_name"] = app.display_name
        aggregate["raw_name"] = app.raw_name
        aggregate["category"] = app.category
        aggregate["source"] = app.source
        aggregate["sample_count"] += 1
        aggregate["observed_seconds"] += interval_s

        if app.estimated_share_w is not None:
            aggregate["estimated_share_wh"] += app.estimated_share_w * interval_s / 3600.0
            aggregate["estimated_watt_seconds"] += app.estimated_share_w * interval_s
            aggregate["peak_estimated_w"] = max(aggregate["peak_estimated_w"], app.estimated_share_w)
        if app.estimated_dynamic_w is not None:
            aggregate["estimated_dynamic_wh"] += app.estimated_dynamic_w * interval_s / 3600.0
            aggregate["dynamic_watt_seconds"] += app.estimated_dynamic_w * interval_s
            aggregate["peak_dynamic_w"] = max(aggregate["peak_dynamic_w"], app.estimated_dynamic_w)

        if app.estimated_cpu_w is not None:
            aggregate["estimated_cpu_wh"] += app.estimated_cpu_w * interval_s / 3600.0
            aggregate["estimated_cpu_watt_seconds"] += app.estimated_cpu_w * interval_s
            aggregate["peak_estimated_cpu_w"] = max(aggregate["peak_estimated_cpu_w"], app.estimated_cpu_w)
        if app.estimated_gpu_w is not None:
            aggregate["estimated_gpu_wh"] += app.estimated_gpu_w * interval_s / 3600.0
            aggregate["estimated_gpu_watt_seconds"] += app.estimated_gpu_w * interval_s
            aggregate["peak_estimated_gpu_w"] = max(aggregate["peak_estimated_gpu_w"], app.estimated_gpu_w)
        if app.estimated_residual_w is not None:
            aggregate["estimated_residual_wh"] += app.estimated_residual_w * interval_s / 3600.0

        impact = app.energy_impact_per_s if app.energy_impact_per_s is not None else app.energy_impact
        if impact is not None:
            aggregate["energy_impact_total"] += impact
            aggregate["energy_impact_count"] += 1
            aggregate["peak_energy_impact"] = max(aggregate["peak_energy_impact"], impact)
        if app.cpu_ms_per_s is not None:
            aggregate["cpu_ms_per_s_total"] += app.cpu_ms_per_s
            aggregate["cpu_ms_per_s_count"] += 1
            aggregate["peak_cpu_ms_per_s"] = max(aggregate["peak_cpu_ms_per_s"], app.cpu_ms_per_s)
        if app.gpu_ms_per_s is not None:
            aggregate["gpu_ms_per_s_total"] += app.gpu_ms_per_s
            aggregate["gpu_ms_per_s_count"] += 1
            aggregate["peak_gpu_ms_per_s"] = max(aggregate["peak_gpu_ms_per_s"], app.gpu_ms_per_s)
        if app.intr_wakeups_per_s is not None:
            aggregate["intr_wakeups_per_s_total"] += app.intr_wakeups_per_s
            aggregate["intr_wakeups_per_s_count"] += 1
            aggregate["peak_intr_wakeups_per_s"] = max(aggregate["peak_intr_wakeups_per_s"], app.intr_wakeups_per_s)
        if app.idle_wakeups_per_s is not None:
            aggregate["idle_wakeups_per_s_total"] += app.idle_wakeups_per_s
            aggregate["idle_wakeups_per_s_count"] += 1
            aggregate["peak_idle_wakeups_per_s"] = max(aggregate["peak_idle_wakeups_per_s"], app.idle_wakeups_per_s)

        aggregate["disk_read_bytes"] += (app.disk_read_bytes_per_s or 0.0) * interval_s
        aggregate["disk_write_bytes"] += (app.disk_write_bytes_per_s or 0.0) * interval_s
        aggregate["network_rx_bytes"] += (app.network_rx_bytes_per_s or 0.0) * interval_s
        aggregate["network_tx_bytes"] += (app.network_tx_bytes_per_s or 0.0) * interval_s

    def _summary_rows(self) -> list[dict[str, Any]]:
        rows: list[dict[str, Any]] = []
        for aggregate in self._aggregates.values():
            seconds = aggregate["observed_seconds"]
            row = {
                "key": aggregate["key"],
                "display_name": aggregate["display_name"],
                "raw_name": aggregate["raw_name"],
                "category": aggregate["category"],
                "source": aggregate["source"],
                "sample_count": aggregate["sample_count"],
                "observed_seconds": seconds,
                "estimated_share_wh": aggregate["estimated_share_wh"],
                "estimated_dynamic_wh": aggregate["estimated_dynamic_wh"],
                "estimated_cpu_wh": aggregate["estimated_cpu_wh"],
                "estimated_gpu_wh": aggregate["estimated_gpu_wh"],
                "estimated_residual_wh": aggregate["estimated_residual_wh"],
                "average_estimated_w": aggregate["estimated_watt_seconds"] / seconds if seconds > 0 else None,
                "peak_estimated_w": aggregate["peak_estimated_w"],
                "average_dynamic_w": aggregate["dynamic_watt_seconds"] / seconds if seconds > 0 else None,
                "peak_dynamic_w": aggregate["peak_dynamic_w"],
                "average_estimated_cpu_w": aggregate["estimated_cpu_watt_seconds"] / seconds if seconds > 0 else None,
                "peak_estimated_cpu_w": aggregate["peak_estimated_cpu_w"],
                "average_estimated_gpu_w": aggregate["estimated_gpu_watt_seconds"] / seconds if seconds > 0 else None,
                "peak_estimated_gpu_w": aggregate["peak_estimated_gpu_w"],
                "average_energy_impact": self._average(aggregate["energy_impact_total"], aggregate["energy_impact_count"]),
                "peak_energy_impact": aggregate["peak_energy_impact"],
                "average_cpu_ms_per_s": self._average(aggregate["cpu_ms_per_s_total"], aggregate["cpu_ms_per_s_count"]),
                "peak_cpu_ms_per_s": aggregate["peak_cpu_ms_per_s"],
                "average_gpu_ms_per_s": self._average(aggregate["gpu_ms_per_s_total"], aggregate["gpu_ms_per_s_count"]),
                "peak_gpu_ms_per_s": aggregate["peak_gpu_ms_per_s"],
                "average_intr_wakeups_per_s": self._average(aggregate["intr_wakeups_per_s_total"], aggregate["intr_wakeups_per_s_count"]),
                "peak_intr_wakeups_per_s": aggregate["peak_intr_wakeups_per_s"],
                "average_idle_wakeups_per_s": self._average(aggregate["idle_wakeups_per_s_total"], aggregate["idle_wakeups_per_s_count"]),
                "peak_idle_wakeups_per_s": aggregate["peak_idle_wakeups_per_s"],
                "disk_read_bytes": aggregate["disk_read_bytes"],
                "disk_write_bytes": aggregate["disk_write_bytes"],
                "network_rx_bytes": aggregate["network_rx_bytes"],
                "network_tx_bytes": aggregate["network_tx_bytes"],
            }
            rows.append(row)
        rows.sort(key=lambda item: (item.get("estimated_share_wh") or 0.0, item.get("peak_estimated_w") or 0.0), reverse=True)
        return rows

    @staticmethod
    def _atomic_write_text(path: Path, text: str) -> None:
        temp = path.with_suffix(path.suffix + ".tmp")
        temp.write_text(text, encoding="utf-8")
        os.replace(temp, path)

    def _flush_summary(self) -> None:
        if not self.enabled:
            return
        rows = self._summary_rows()
        summary = {
            "schema": APP_POWER_SCHEMA,
            "tool_version": VERSION,
            "run_stem": self.run_stem,
            "created_at": self._created_at,
            "updated_at": _now_iso(),
            "sample_count": self._sample_count,
            "observed_seconds": self._observed_seconds,
            "estimated_system_wh": self._estimated_system_wh,
            "confidence_counts": dict(sorted(self._confidence_counts.items())),
            "source_counts": dict(sorted(self._source_counts.items())),
            "error_count": self._error_count,
            "method": {
                "activity_source": "Apple powermetrics Energy Impact / coalition data with ps fallback",
                "power_model": "component-aware allocation of MacPowerLab total system power",
                "dynamic_model": "CPU watts by CPU time, GPU watts by GPU time, residual by Energy Impact above quiet baseline",
                "direct_per_app_watts": False,
            },
            "paths": {
                "jsonl": str(self.jsonl_path),
                "summary_json": str(self.summary_json_path),
                "summary_csv": str(self.summary_csv_path),
            },
            "apps": rows,
        }
        self._atomic_write_text(self.summary_json_path, json.dumps(summary, indent=2, ensure_ascii=False))

        temp_csv = self.summary_csv_path.with_suffix(self.summary_csv_path.suffix + ".tmp")
        with temp_csv.open("w", newline="", encoding="utf-8") as handle:
            writer = csv.DictWriter(handle, fieldnames=self.SUMMARY_FIELDS)
            writer.writeheader()
            for row in rows:
                writer.writerow({field: row.get(field) for field in self.SUMMARY_FIELDS})
        os.replace(temp_csv, self.summary_csv_path)

    def write(self, result: AttributionResult, row: Mapping[str, Any]) -> None:
        """Write one unique app-power sample and update incremental summaries."""

        if not self.enabled or self._jsonl_handle is None:
            return
        if self._last_sample_id == result.sample_id:
            return

        interval_s = self._interval_seconds(result)
        self._last_sample_id = result.sample_id
        self._last_sample_monotonic = result.sample_monotonic_time
        self._sample_count += 1
        self._observed_seconds += interval_s
        self._confidence_counts[result.confidence or "unknown"] += 1
        self._source_counts[result.source or "unknown"] += 1
        if result.error:
            self._error_count += 1
        if result.total_system_power_w is not None:
            self._estimated_system_wh += result.total_system_power_w * interval_s / 3600.0

        app_records: list[dict[str, Any]] = []
        for app in result.apps:
            self._update_aggregate(app, interval_s)
            app_record = app.to_dict()
            app_record["estimated_wh_interval"] = (
                app.estimated_share_w * interval_s / 3600.0
                if app.estimated_share_w is not None
                else None
            )
            app_record["estimated_dynamic_wh_interval"] = (
                app.estimated_dynamic_w * interval_s / 3600.0
                if app.estimated_dynamic_w is not None
                else None
            )
            app_record["estimated_cpu_wh_interval"] = (
                app.estimated_cpu_w * interval_s / 3600.0
                if app.estimated_cpu_w is not None
                else None
            )
            app_record["estimated_gpu_wh_interval"] = (
                app.estimated_gpu_w * interval_s / 3600.0
                if app.estimated_gpu_w is not None
                else None
            )
            app_record["estimated_residual_wh_interval"] = (
                app.estimated_residual_w * interval_s / 3600.0
                if app.estimated_residual_w is not None
                else None
            )
            app_records.append(app_record)

        record = {
            "schema": APP_POWER_SCHEMA,
            "tool_version": VERSION,
            "timestamp": row.get("timestamp") or result.timestamp,
            "sample_id": result.sample_id,
            "interval_seconds": interval_s,
            "phase": row.get("phase"),
            "auto_phase": row.get("auto_phase"),
            "mode": row.get("mode"),
            "power_source": row.get("power_source"),
            "status": result.status,
            "source": result.source,
            "confidence": result.confidence,
            "error": result.error,
            "power_context": {
                "total_system_power_w": result.total_system_power_w,
                "total_power_source": result.total_power_source,
                "baseline_power_w": result.baseline_power_w,
                "dynamic_power_w": result.dynamic_power_w,
                "cpu_component_pool_w": result.cpu_component_pool_w,
                "gpu_component_pool_w": result.gpu_component_pool_w,
                "residual_dynamic_pool_w": result.residual_dynamic_pool_w,
                "attribution_method": result.attribution_method,
                "attributed_power_w": result.attributed_power_w,
                "unattributed_power_w": result.unattributed_power_w,
            },
            "apps": app_records,
        }
        self._jsonl_handle.write(json.dumps(record, ensure_ascii=False) + "\n")
        self._jsonl_handle.flush()

        if self._sample_count == 1 or self._sample_count % self.flush_every == 0:
            self._flush_summary()

    def close(self) -> None:
        """Flush summaries and close the JSONL stream."""

        if not self.enabled:
            return
        try:
            self._flush_summary()
        finally:
            if self._jsonl_handle is not None:
                self._jsonl_handle.close()
                self._jsonl_handle = None


def empty_app_power_row_fields(top_slots: int = DEFAULT_TOP_SLOTS) -> dict[str, Any]:
    """Return a complete set of blank app-power CSV fields."""

    top_slots = validate_int("top_slots", int(top_slots), 1, 12)
    fields: dict[str, Any] = {
        "app_power_status": "off",
        "app_power_source": None,
        "app_power_confidence": None,
        "app_power_error": None,
        "app_power_sample_id": None,
        "app_power_sample_age_s": None,
        "app_power_total_system_w": None,
        "app_power_total_source": None,
        "app_power_baseline_w": None,
        "app_power_dynamic_w": None,
        "app_power_cpu_pool_w": None,
        "app_power_gpu_pool_w": None,
        "app_power_residual_pool_w": None,
        "app_power_method": None,
        "app_power_attributed_w": None,
        "app_power_unattributed_w": None,
        "app_power_top_summary": None,
        "app_power_top_json": None,
    }
    for index in range(1, top_slots + 1):
        prefix = f"app_top_{index}"
        fields.update(
            {
                f"{prefix}_name": None,
                f"{prefix}_category": None,
                f"{prefix}_estimated_w": None,
                f"{prefix}_dynamic_w": None,
                f"{prefix}_cpu_w": None,
                f"{prefix}_gpu_w": None,
                f"{prefix}_residual_w": None,
                f"{prefix}_share_percent": None,
                f"{prefix}_energy_impact": None,
                f"{prefix}_cpu_ms_per_s": None,
                f"{prefix}_gpu_ms_per_s": None,
            }
        )
    return fields


def _print_once(args: argparse.Namespace) -> int:
    resolver = BundleNameResolver(enable_spotlight=not args.no_bundle_resolution)
    sampler = PowermetricsTaskSampler(
        sample_ms=args.sample_ms,
        max_activities=args.max_activities,
        resolver=resolver,
    )
    fallback = PsFallbackSampler(max_activities=args.max_activities, resolver=resolver)
    try:
        snapshot = sampler.sample(1)
    except AppPowerSamplingError as exc:
        snapshot = fallback.sample(1, str(exc))

    engine = AppPowerAttributionEngine(top_slots=args.top)
    row = {
        "primary_total_load_w": args.total_watts,
        "primary_total_load_source": args.total_source,
        "power_source": args.power_source,
        "phase": "manual app-power sample",
        "auto_phase": "manual",
        "stress_cpu_percent": 0.0,
        "baseline_primary_total_load_w": args.baseline_watts,
        "baseline_power_source": args.power_source,
    }
    result = engine.attribute(row, snapshot)
    if args.json:
        print(
            json.dumps(
                {
                    "snapshot": {
                        "sample_id": snapshot.sample_id,
                        "timestamp": snapshot.timestamp,
                        "source": snapshot.source,
                        "status": snapshot.status,
                        "error": snapshot.error,
                    },
                    "attribution": {
                        "confidence": result.confidence,
                        "total_system_power_w": result.total_system_power_w,
                        "baseline_power_w": result.baseline_power_w,
                        "dynamic_power_w": result.dynamic_power_w,
                        "cpu_component_pool_w": result.cpu_component_pool_w,
                        "gpu_component_pool_w": result.gpu_component_pool_w,
                        "residual_dynamic_pool_w": result.residual_dynamic_pool_w,
                        "attribution_method": result.attribution_method,
                        "apps": [app.to_dict() for app in result.top_for_display(args.top)],
                    },
                },
                indent=2,
                ensure_ascii=False,
            )
        )
    else:
        print(f"MacPowerLab App Power Attribution v{VERSION}")
        print(f"Source: {snapshot.source} ({snapshot.status})")
        if snapshot.error:
            print(f"Fallback reason: {snapshot.error}")
        print(f"Total power: {result.total_system_power_w if result.total_system_power_w is not None else 'n/a'} W")
        print(f"Confidence: {result.confidence}")
        print()
        print("Estimated app power shares:")
        for app in result.top_for_display(args.top):
            watts = f"{app.estimated_share_w:.2f} W" if app.estimated_share_w is not None else "n/a"
            dynamic = f"{app.estimated_dynamic_w:.2f} W dyn" if app.estimated_dynamic_w is not None else "n/a dyn"
            impact = app.energy_impact_per_s if app.energy_impact_per_s is not None else app.energy_impact
            print(f"{watts:>10}  {dynamic:>12}  {app.energy_share_percent:6.2f}%  EI {impact or 0:8.2f}  {app.display_name}")
    return 0


def build_arg_parser() -> argparse.ArgumentParser:
    """Build the standalone app-power CLI parser."""

    parser = argparse.ArgumentParser(description="Sample per-app Energy Impact and estimate power shares.")
    parser.add_argument("--sample-ms", type=int, default=1000, help="powermetrics sample window, 250-60000 ms")
    parser.add_argument("--top", type=int, default=10, help="number of apps to print, 1-50")
    parser.add_argument("--max-activities", type=int, default=DEFAULT_MAX_ACTIVITIES, help="maximum raw activities retained, 10-1000")
    parser.add_argument("--total-watts", type=float, default=None, help="optional total system watts used for app-share estimates")
    parser.add_argument("--baseline-watts", type=float, default=0.0, help="optional quiet baseline watts used for dynamic estimates")
    parser.add_argument("--total-source", default="manual total watts", help="description of the total-watts source")
    parser.add_argument("--power-source", choices=("AC Power", "Battery Power", "Unknown"), default="Unknown")
    parser.add_argument("--no-bundle-resolution", action="store_true", help="disable Spotlight bundle-name lookup")
    parser.add_argument("--json", action="store_true", help="print JSON instead of a text table")
    return parser


def main() -> int:
    """Standalone CLI entry point used for diagnostics and one-shot sampling."""

    parser = build_arg_parser()
    args = parser.parse_args()
    try:
        args.sample_ms = validate_int("sample_ms", args.sample_ms, 250, 60_000)
        args.top = validate_int("top", args.top, 1, 50)
        args.max_activities = validate_int("max_activities", args.max_activities, 10, 1000)
        if args.total_watts is not None:
            args.total_watts = validate_float("total_watts", args.total_watts, 0.0, 2000.0)
        args.baseline_watts = validate_float("baseline_watts", args.baseline_watts, 0.0, 2000.0)
        return _print_once(args)
    except AppPowerError as exc:
        parser.error(str(exc))
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
