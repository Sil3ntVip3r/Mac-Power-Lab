#!/usr/bin/env python3
"""
MacPowerLab log packer v0.8.6

Creates a max-compressed tar archive of MacPowerLab logs for sharing.

Default:
  ./pack_logs.sh

Output:
  exports/mac_power_logs_all_YYYYMMDD_HHMMSS.tar.xz

Compression:
  Python lzma/xz preset 9 + extreme. If xz is unavailable, use:
  ./pack_logs_gz.sh
"""

import argparse
import gzip
import io
import json
import lzma
import tarfile
import time
from datetime import datetime
from pathlib import Path

VERSION = "0.9.0"


def human_size(n):
    n = float(n)
    for unit in ["B", "KB", "MB", "GB", "TB"]:
        if n < 1024 or unit == "TB":
            return f"{n:.1f} {unit}"
        n /= 1024


def latest_run_files(logs_dir):
    csvs = sorted(
        [p for p in logs_dir.glob("mac_power_*.csv") if "_events" not in p.name],
        key=lambda p: p.stat().st_mtime,
        reverse=True,
    )
    if not csvs:
        return []
    csv = csvs[0]
    stem = csv.with_suffix("").name
    files = []
    for name in [csv.name, f"{stem}_events.jsonl", f"{stem}_debug.json", f"{stem}_report.html"]:
        p = logs_dir / name
        if p.exists():
            files.append(p)
    for extra in ["history.jsonl", "wall_meter_readings.jsonl"]:
        p = logs_dir / extra
        if p.exists():
            files.append(p)
    return files


def collect_files(root, mode):
    logs_dir = root / "logs"
    if not logs_dir.exists():
        return []
    if mode == "latest":
        files = latest_run_files(logs_dir)
    else:
        files = []
        for p in logs_dir.rglob("*"):
            if not p.is_file():
                continue
            name = p.name.lower()
            if name.endswith((".tar", ".tar.gz", ".tgz", ".tar.xz", ".zip", ".xz", ".gz")):
                continue
            files.append(p)
    for extra in ["current_test_metadata.json", "current_phase.txt", "README.txt"]:
        p = root / extra
        if p.exists() and p.is_file():
            files.append(p)
    return sorted(set(files))


def add_manifest(tar, root, files, archive_name, mode, compression):
    total = sum(p.stat().st_size for p in files)
    manifest = {
        "created_at": datetime.now().isoformat(timespec="seconds"),
        "created_by": f"MacPowerLab log packer v{VERSION}",
        "archive_name": archive_name,
        "mode": mode,
        "compression": compression,
        "file_count": len(files),
        "uncompressed_size_bytes": total,
        "uncompressed_size_human": human_size(total),
        "files": [
            {
                "path": str(p.relative_to(root)),
                "size_bytes": p.stat().st_size,
                "size_human": human_size(p.stat().st_size),
                "modified": datetime.fromtimestamp(p.stat().st_mtime).isoformat(timespec="seconds"),
            }
            for p in files
        ],
        "notes": [
            "Contains MacPowerLab logs/reports/debug files.",
            "Debug JSON may include raw AppleSmartBattery telemetry.",
        ],
    }
    data = json.dumps(manifest, indent=2, ensure_ascii=False).encode("utf-8")
    info = tarfile.TarInfo("MANIFEST_macpowerlab_logs.json")
    info.size = len(data)
    info.mtime = time.time()
    tar.addfile(info, io.BytesIO(data))


def make_xz(out_path, root, files, mode):
    preset = 9 | lzma.PRESET_EXTREME
    with lzma.open(out_path, "wb", preset=preset) as xz:
        with tarfile.open(fileobj=xz, mode="w", format=tarfile.PAX_FORMAT) as tar:
            add_manifest(tar, root, files, out_path.name, mode, "xz preset=9 extreme")
            for p in files:
                tar.add(p, arcname=str(p.relative_to(root)), recursive=False)
    return "xz preset=9 extreme"


def make_gz(out_path, root, files, mode):
    with gzip.open(out_path, "wb", compresslevel=9) as gz:
        with tarfile.open(fileobj=gz, mode="w", format=tarfile.PAX_FORMAT) as tar:
            add_manifest(tar, root, files, out_path.name, mode, "gzip level=9")
            for p in files:
                tar.add(p, arcname=str(p.relative_to(root)), recursive=False)
    return "gzip level=9"


def main():
    parser = argparse.ArgumentParser(description=f"MacPowerLab log packer v{VERSION}")
    parser.add_argument("--mode", choices=["all", "latest"], default="all", help="Pack all logs or latest run only.")
    parser.add_argument("--format", choices=["xz", "gz"], default="xz", help="Archive format.")
    parser.add_argument("--output-dir", default="exports", help="Output folder.")
    parser.add_argument("--name", default=None, help="Custom archive filename without extension.")
    args = parser.parse_args()

    root = Path(__file__).resolve().parent
    out_dir = root / args.output_dir
    out_dir.mkdir(parents=True, exist_ok=True)
    files = collect_files(root, args.mode)

    if not files:
        print("No log files found to pack.")
        print(f"Expected logs folder: {root / 'logs'}")
        raise SystemExit(1)

    stamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    base = args.name or f"mac_power_logs_{args.mode}_{stamp}"

    if args.format == "xz":
        out_path = out_dir / f"{base}.tar.xz"
        compression = make_xz(out_path, root, files, args.mode)
    else:
        out_path = out_dir / f"{base}.tar.gz"
        compression = make_gz(out_path, root, files, args.mode)

    raw = sum(p.stat().st_size for p in files)
    packed = out_path.stat().st_size
    ratio = packed / raw * 100 if raw else 0

    print("MacPowerLab log archive created")
    print("--------------------------------")
    print(f"Archive:       {out_path}")
    print(f"Mode:          {args.mode}")
    print(f"Compression:   {compression}")
    print(f"Files packed:  {len(files)}")
    print(f"Raw size:      {human_size(raw)}")
    print(f"Archive size:  {human_size(packed)}")
    print(f"Size ratio:    {ratio:.1f}%")
    print()
    print("Send/upload this file:")
    print(out_path)


if __name__ == "__main__":
    main()
