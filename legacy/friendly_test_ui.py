#!/usr/bin/env python3
"""
MacPowerLab Friendly Test UI v0.8.6

Clean stress-test runner with:
- progress bar
- elapsed and remaining timers
- automatic start
- caffeinate-based sleep prevention
- active-test lock to prevent overlapping stress tests
- captured command output
- structured per-test JSON record
- global logs/test_runs.jsonl index
"""

import argparse
import json
import os
import re
import signal
import shutil
import subprocess
import sys
import time
from datetime import datetime
from pathlib import Path

VERSION = "0.9.0"


def fmt_time(seconds):
    seconds = max(0, int(seconds))
    h, rem = divmod(seconds, 3600)
    m, s = divmod(rem, 60)
    return f"{h:d}:{m:02d}:{s:02d}" if h else f"{m:02d}:{s:02d}"


def bar(percent, width):
    percent = max(0.0, min(1.0, percent))
    filled = int(width * percent)
    return "█" * filled + "░" * (width - filled)


def clear():
    sys.stdout.write("\033[2J\033[H")
    sys.stdout.flush()


def hide_cursor():
    sys.stdout.write("\033[?25l")
    sys.stdout.flush()


def show_cursor():
    sys.stdout.write("\033[?25h")
    sys.stdout.flush()


def color(text, code):
    return f"\033[{code}m{text}\033[0m"


def slugify(text):
    text = re.sub(r"[^A-Za-z0-9._-]+", "_", text.strip())
    text = text.strip("_")
    return text[:60] or "test"


def parse_meta(items):
    out = {}
    for item in items or []:
        if "=" in item:
            k, v = item.split("=", 1)
            out[k.strip()] = v.strip()
    return out


def write_phase(root, phase):
    if not phase:
        return
    try:
        (root / "current_phase.txt").write_text(phase + "\n", encoding="utf-8")
    except Exception:
        pass


def pid_alive(pid):
    try:
        os.kill(int(pid), 0)
        return True
    except Exception:
        return False


class TestLock:
    def __init__(self, root, force=False):
        self.root = root
        self.path = root / "logs" / ".active_test.lock"
        self.force = force
        self.acquired = False

    def acquire(self, record):
        self.path.parent.mkdir(exist_ok=True)
        if self.path.exists():
            try:
                old = json.loads(self.path.read_text(encoding="utf-8"))
            except Exception:
                old = {}
            old_pid = old.get("pid")
            if old_pid and pid_alive(old_pid) and not self.force:
                print()
                print(color("Another MacPowerLab test appears to be running.", "1;91"))
                print(f"Active test: {old.get('title', 'unknown')}")
                print(f"Started:     {old.get('start_time', 'unknown')}")
                print(f"PID:         {old_pid}")
                print()
                print("Stop that test first, or use --force-lock if you are sure it is stale.")
                raise SystemExit(3)
            else:
                try:
                    self.path.unlink()
                except Exception:
                    pass

        lock = dict(record)
        lock["pid"] = os.getpid()
        lock["lock_created_at"] = datetime.now().isoformat(timespec="seconds")
        self.path.write_text(json.dumps(lock, indent=2, ensure_ascii=False), encoding="utf-8")
        self.acquired = True

    def release(self):
        if self.acquired and self.path.exists():
            try:
                current = json.loads(self.path.read_text(encoding="utf-8"))
                if current.get("pid") == os.getpid():
                    self.path.unlink()
            except Exception:
                try:
                    self.path.unlink()
                except Exception:
                    pass
        self.acquired = False


def find_nearest_power_log(logs_dir, start_ts, end_ts):
    csvs = [p for p in logs_dir.glob("mac_power_*.csv") if "_events" not in p.name]
    if not csvs:
        return None
    candidates = []
    for p in csvs:
        m = p.stat().st_mtime
        if start_ts - 60 <= m <= end_ts + 180:
            score = 1000 - abs(m - end_ts)
        else:
            score = -abs(m - end_ts)
        candidates.append((score, p.stat().st_mtime, p))
    candidates.sort(reverse=True)
    return candidates[0][2] if candidates else None



def process_activity(root_pid):
    """
    Sum CPU/memory for the active process tree. This gives useful live feedback
    for CPU/GPU tests that do not print progress until they finish.
    """
    try:
        out = subprocess.check_output(
            ["ps", "-axo", "pid,ppid,pcpu,pmem,rss,comm"],
            text=True,
            stderr=subprocess.DEVNULL,
        )
    except Exception:
        return None

    rows = []
    for line in out.splitlines()[1:]:
        parts = line.strip().split(None, 5)
        if len(parts) < 6:
            continue
        try:
            pid = int(parts[0])
            ppid = int(parts[1])
            pcpu = float(parts[2])
            pmem = float(parts[3])
            rss_kb = float(parts[4])
            comm = parts[5]
            rows.append((pid, ppid, pcpu, pmem, rss_kb, comm))
        except Exception:
            continue

    children = {root_pid}
    changed = True
    while changed:
        changed = False
        for pid, ppid, *_rest in rows:
            if ppid in children and pid not in children:
                children.add(pid)
                changed = True

    selected = [r for r in rows if r[0] in children]
    if not selected:
        return None

    cpu = sum(r[2] for r in selected)
    mem = sum(r[3] for r in selected)
    rss_mb = sum(r[4] for r in selected) / 1024.0
    names = []
    for r in selected:
        name = Path(r[5]).name
        if name not in names:
            names.append(name)

    return {
        "process_count": len(selected),
        "cpu_percent_sum": cpu,
        "mem_percent_sum": mem,
        "rss_mb_sum": rss_mb,
        "process_names": names[:6],
    }


def draw(args, proc, start, log_path, json_path, status, using_caffeinate, final=False):
    width = shutil.get_terminal_size((100, 24)).columns
    content_width = max(34, min(width - 8, 92))
    elapsed = time.time() - start
    duration = args.duration or 0
    pct = elapsed / duration if duration > 0 else 0
    pct_display = min(100.0, pct * 100.0) if duration > 0 else 0
    remaining = max(0, duration - elapsed) if duration > 0 else 0
    overtime = max(0, elapsed - duration) if duration > 0 else 0

    spinner = ["⠋","⠙","⠹","⠸","⠼","⠴","⠦","⠧","⠇","⠏"][int(elapsed * 4) % 10]
    if final:
        spinner = "✓" if proc.returncode == 0 else "✗"

    status_col = "92" if proc.poll() is None else ("92" if proc.returncode == 0 else "91")
    clear()
    print(color(f" MacPowerLab Test UI v{VERSION} ", "1;37;44"))
    print()
    print(f"{color(spinner, '1;92')}  {color(args.title, '1;96')}")
    print(f"   Phase:       {color(args.phase or 'unmarked', '95')}")
    if duration > 0 and overtime > 0 and proc.poll() is None:
        status = "finishing workload / command buffers"
        status_col = "93"
    print(f"   Status:      {color(status, status_col)}")
    print(f"   Sleep lock:  {color('ON via caffeinate', '92') if using_caffeinate else color('OFF / caffeinate unavailable', '93')}")
    print()

    if duration > 0:
        print(f"   Progress:    [{color(bar(pct, min(50, content_width)), '92')}] {pct_display:5.1f}%")
        print(f"   Elapsed:     {fmt_time(elapsed)}")
        print(f"   Remaining:   {fmt_time(remaining)}")
        if overtime > 0:
            print(f"   Overtime:    {color(fmt_time(overtime) + ' finishing child workload', '93')}")
    else:
        print(f"   Elapsed:     {fmt_time(elapsed)}")
        print("   Progress:    running")

    print()
    print(f"   Test log:    {log_path}")
    print(f"   Test JSON:   {json_path}")
    print(f"   Command:     {' '.join(args.command)}")
    activity = process_activity(proc.pid) if proc and proc.poll() is None else None
    if activity:
        print(f"   Workload:    {activity['process_count']} proc / process CPU {activity['cpu_percent_sum']:.1f}% / RAM {activity['rss_mb_sum']:.0f} MB")
        print(f"   Processes:   {', '.join(activity['process_names'])}")
        if activity['cpu_percent_sum'] > 200:
            print(f"   CPU proof:   child workload is active even if powermetrics CPU watts are 0")
    print()
    print(color("   Active-test lock is enabled. Ctrl+C stops this test safely.", "90"))

    try:
        if log_path.exists():
            age = max(0, time.time() - log_path.stat().st_mtime)
            print(f"   Output age:  {age:.1f}s since child process wrote output")
            lines = log_path.read_text(encoding="utf-8", errors="replace").splitlines()[-5:]
            if lines:
                print()
                print(color("   Recent test output:", "90"))
                for line in lines:
                    print("   " + line[:content_width])
    except Exception:
        pass


def append_jsonl(path, record):
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("a", encoding="utf-8") as f:
        f.write(json.dumps(record, ensure_ascii=False) + "\n")


def main():
    parser = argparse.ArgumentParser(description=f"MacPowerLab Friendly Test UI v{VERSION}")
    parser.add_argument("--title", default="MacPowerLab Test")
    parser.add_argument("--duration", type=float, default=0)
    parser.add_argument("--phase", default="")
    parser.add_argument("--log", default=None)
    parser.add_argument("--meta", action="append", default=[], help="Extra metadata key=value")
    parser.add_argument("--no-caffeinate", action="store_true", help="Do not prevent sleep during test.")
    parser.add_argument("--force-lock", action="store_true", help="Override stale active-test lock.")
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()

    if args.command and args.command[0] == "--":
        args.command = args.command[1:]
    if not args.command:
        print("No command provided.")
        raise SystemExit(2)

    root = Path(__file__).resolve().parent
    logs_dir = root / "logs"
    logs_dir.mkdir(exist_ok=True)
    test_runs_dir = logs_dir / "test_runs"
    test_runs_dir.mkdir(exist_ok=True)

    stamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    test_id = f"{stamp}_{slugify(args.title)}"

    log_path = Path(args.log) if args.log else logs_dir / f"friendly_{test_id}.log"
    if not log_path.is_absolute():
        log_path = root / log_path
    log_path.parent.mkdir(parents=True, exist_ok=True)

    json_path = test_runs_dir / f"{test_id}.json"
    jsonl_path = logs_dir / "test_runs.jsonl"
    meta = parse_meta(args.meta)

    start_wall = datetime.now().isoformat(timespec="seconds")
    base_record = {
        "schema": "macpowerlab.test_run.v2",
        "test_id": test_id,
        "tool_version": VERSION,
        "title": args.title,
        "phase": args.phase,
        "requested_duration_seconds": args.duration,
        "command": args.command,
        "metadata": meta,
        "start_time": start_wall,
    }

    lock = TestLock(root, force=args.force_lock)
    lock.acquire(base_record)

    write_phase(root, args.phase)
    start_ts = time.time()

    caffeinate_path = shutil.which("caffeinate")
    using_caffeinate = bool(caffeinate_path and not args.no_caffeinate)
    actual_command = [caffeinate_path, "-dimsu"] + args.command if using_caffeinate else args.command

    record = dict(base_record)
    record.update({
        "end_time": None,
        "elapsed_seconds": None,
        "exit_code": None,
        "status": "running",
        "log_path": str(log_path.relative_to(root)),
        "json_path": str(json_path.relative_to(root)),
        "nearest_power_log": None,
        "sleep_prevention": "caffeinate -dimsu" if using_caffeinate else "off",
        "actual_command": actual_command,
    })
    json_path.write_text(json.dumps(record, indent=2, ensure_ascii=False), encoding="utf-8")

    proc = None
    stopped_by_user = False
    try:
        with log_path.open("w", encoding="utf-8", errors="replace") as log:
            log.write(f"MacPowerLab Friendly Test UI v{VERSION}\n")
            log.write(f"Test ID: {test_id}\n")
            log.write(f"Started: {start_wall}\n")
            log.write(f"Title: {args.title}\n")
            log.write(f"Phase: {args.phase}\n")
            log.write(f"Metadata: {json.dumps(meta, ensure_ascii=False)}\n")
            log.write(f"Command: {' '.join(args.command)}\n")
            log.write(f"Actual command: {' '.join(actual_command)}\n")
            log.write(f"Sleep prevention: {'caffeinate -dimsu' if using_caffeinate else 'off'}\n")
            log.write("=" * 72 + "\n")
            log.flush()

            env = os.environ.copy()
            env["MACPOWERLAB_PRETTY_UI"] = "1"
            env["MACPOWERLAB_AUTO_START"] = "1"
            env["MACPOWERLAB_TEST_ID"] = test_id

            proc = subprocess.Popen(
                actual_command,
                cwd=str(root),
                stdout=log,
                stderr=subprocess.STDOUT,
                stdin=subprocess.DEVNULL,
                text=True,
                env=env,
                preexec_fn=os.setsid if hasattr(os, "setsid") else None,
            )

            hide_cursor()
            try:
                while proc.poll() is None:
                    draw(args, proc, start_ts, log_path, json_path, "running", using_caffeinate)
                    time.sleep(0.5)

                if proc.returncode == 0:
                    status = "complete"
                elif proc.returncode in (-15, -2, 130):
                    status = f"stopped exit {proc.returncode}"
                else:
                    status = f"failed exit {proc.returncode}"
                draw(args, proc, start_ts, log_path, json_path, status, using_caffeinate, final=True)
                time.sleep(0.8)

            except KeyboardInterrupt:
                stopped_by_user = True
                draw(args, proc, start_ts, log_path, json_path, "stopping...", using_caffeinate)
                try:
                    if hasattr(os, "killpg"):
                        os.killpg(os.getpgid(proc.pid), signal.SIGTERM)
                    else:
                        proc.terminate()
                    proc.wait(timeout=8)
                except Exception:
                    try:
                        proc.kill()
                    except Exception:
                        pass
                proc.returncode = proc.returncode if proc.returncode is not None else 130
                log.write("\nStopped by user.\n")
            finally:
                show_cursor()

        end_ts = time.time()
        elapsed = end_ts - start_ts
        nearest = find_nearest_power_log(logs_dir, start_ts, end_ts)
        end_wall = datetime.now().isoformat(timespec="seconds")

        record.update({
            "end_time": end_wall,
            "elapsed_seconds": round(elapsed, 3),
            "exit_code": proc.returncode if proc else None,
            "status": "complete" if proc and proc.returncode == 0 else ("stopped" if stopped_by_user or (proc and proc.returncode in (130, -15, -2)) else "failed"),
            "output_log_size_bytes": log_path.stat().st_size if log_path.exists() else None,
            "nearest_power_log": str(nearest.relative_to(root)) if nearest else None,
        })

        json_path.write_text(json.dumps(record, indent=2, ensure_ascii=False), encoding="utf-8")
        append_jsonl(jsonl_path, record)

        with log_path.open("a", encoding="utf-8", errors="replace") as log:
            log.write(f"\nFinished: {end_wall}\n")
            log.write(f"Elapsed seconds: {elapsed:.3f}\n")
            log.write(f"Exit code: {record['exit_code']}\n")
            if nearest:
                log.write(f"Nearest power log: {nearest.relative_to(root)}\n")

        print()
        print(f"Test log:  {log_path}")
        print(f"Test JSON: {json_path}")
        if nearest:
            print(f"Power log: {nearest}")

        raise SystemExit(proc.returncode if proc else 1)

    finally:
        show_cursor()
        lock.release()


if __name__ == "__main__":
    main()
