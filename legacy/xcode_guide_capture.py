#!/usr/bin/env python3
"""
MacPowerLab Xcode Guide Research Capture v0.8.6

Uses https://github.com/mikeroyal/Xcode-Guide as a curated link map for Xcode,
Swift, Objective-C, Core ML, Metal, Apple Silicon, Instruments, and Apple
Developer documentation.

v0.8.6 fixes:
- Falls back from Python TLS to unverified public-doc TLS, then macOS curl -k -L
  when Python's certificate store fails during public documentation capture.
"""

import argparse
import json
import re
import ssl
import subprocess
import urllib.parse
import urllib.request
from datetime import datetime
from pathlib import Path

VERSION = "0.9.0"

DEFAULT_REPO = "https://github.com/mikeroyal/Xcode-Guide"
RAW_README = "https://raw.githubusercontent.com/mikeroyal/Xcode-Guide/main/README.md"
API_REPO = "https://api.github.com/repos/mikeroyal/Xcode-Guide"
API_CONTENTS = "https://api.github.com/repos/mikeroyal/Xcode-Guide/contents"

KEYWORDS = [
    "battery", "power", "charging", "thermal", "performance", "energy",
    "xcode", "instruments", "metal", "metal performance shaders", "gpu",
    "gpu counters", "memory footprint", "apple silicon", "cpu", "core ml",
    "machine learning", "swift", "objective-c", "appkit", "swiftui",
    "virtualization", "hypervisor", "driver", "kernel", "kext",
    "iokit", "powermetrics", "debugging", "profiling", "optimization",
]

PRIORITY_PATTERNS = [
    "developer.apple.com/documentation/metal",
    "developer.apple.com/documentation/xcode",
    "developer.apple.com/documentation/os/workgroups",
    "developer.apple.com/documentation/kernel",
    "developer.apple.com/documentation/iokit",
    "developer.apple.com/documentation/appkit",
    "developer.apple.com/documentation/foundation",
    "developer.apple.com/documentation/coreml",
    "developer.apple.com/documentation/accelerate",
    "help.apple.com/instruments",
    "developer.apple.com/documentation/macos-release-notes",
]

def http_get(url, timeout=30):
    headers_req = {
        "User-Agent": "MacPowerLab/0.7.3 XcodeGuideCapture",
        "Accept": "text/html,text/plain,application/json,*/*",
    }
    req = urllib.request.Request(url, headers=headers_req)

    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            body = r.read()
            headers = dict(r.headers.items())
            headers["X-MacPowerLab-Capture-Method"] = "python-default-tls"
            return r.geturl(), headers, body
    except Exception as first_error:
        try:
            ctx = ssl._create_unverified_context()
            with urllib.request.urlopen(req, timeout=timeout, context=ctx) as r:
                body = r.read()
                headers = dict(r.headers.items())
                headers["X-MacPowerLab-Capture-Method"] = "python-unverified-tls"
                headers["X-MacPowerLab-First-Error"] = str(first_error)
                return r.geturl(), headers, body
        except Exception as second_error:
            try:
                p = subprocess.run(
                    ["curl", "-k", "-L", "--max-time", str(timeout), "-A", "MacPowerLab/0.7.3 XcodeGuideCapture", "-sS", "-D", "-", url],
                    stdout=subprocess.PIPE,
                    stderr=subprocess.PIPE,
                    timeout=timeout + 10,
                )
                if p.returncode != 0:
                    raise RuntimeError(p.stderr.decode("utf-8", errors="replace"))
                raw = p.stdout
                parts = raw.split(b"\r\n\r\n")
                body = parts[-1] if parts else raw
                header_blob = parts[-2].decode("utf-8", errors="replace") if len(parts) >= 2 else ""
                headers = {
                    "Content-Type": "unknown",
                    "X-MacPowerLab-Capture-Method": "curl-insecure-fallback",
                    "X-MacPowerLab-First-Error": str(first_error),
                    "X-MacPowerLab-Second-Error": str(second_error),
                    "X-MacPowerLab-Raw-Headers": header_blob[:4000],
                }
                return url, headers, body
            except Exception as third_error:
                raise RuntimeError(
                    f"default TLS failed: {first_error}; "
                    f"unverified TLS failed: {second_error}; "
                    f"curl fallback failed: {third_error}"
                )

def safe_name(url):
    parsed = urllib.parse.urlparse(url)
    name = (parsed.netloc + parsed.path).strip("/").replace("/", "__")
    name = re.sub(r"[^A-Za-z0-9_.-]+", "_", name)
    return name[:180] or "root"

def write_text(path, text):
    path.write_text(text, encoding="utf-8", errors="replace")

def extract_links(text):
    links = set()
    for m in re.finditer(r'https?://[^\s"<>\\)]+', text):
        links.add(m.group(0).rstrip(".,;)"))
    for m in re.finditer(r'\]\(([^)]+)\)', text):
        href = m.group(1)
        if href.startswith("http"):
            links.add(href.rstrip(".,;)"))
    for m in re.finditer(r'href=["\']([^"\']+)["\']', text):
        href = m.group(1)
        if href.startswith("http"):
            links.add(href.rstrip(".,;)"))
    return sorted(links)

def relevant_lines(text):
    out = []
    for i, line in enumerate(text.splitlines(), 1):
        low = line.lower()
        if any(k in low for k in KEYWORDS):
            out.append(f"L{i}: {line}")
    return "\n".join(out) + ("\n" if out else "")

def priority_score(url):
    u = url.lower()
    score = 0
    for i, pat in enumerate(PRIORITY_PATTERNS):
        if pat in u:
            score += 100 - i
    for k in KEYWORDS:
        if k.replace(" ", "_") in u or k.replace(" ", "-") in u or k in u:
            score += 5
    if "developer.apple.com" in u:
        score += 25
    if "help.apple.com/instruments" in u:
        score += 25
    return score

def capture_url(url, out_dir):
    rec = {"url": url, "ok": False}
    try:
        final_url, headers, body = http_get(url)
        name = safe_name(url)
        content_type = headers.get("Content-Type", "")
        suffix = ".json" if "json" in content_type.lower() else ".txt"
        path = out_dir / f"{name}{suffix}"
        path.write_bytes(body)
        decoded = body.decode("utf-8", errors="replace")
        decoded_path = out_dir / f"{name}.decoded.txt"
        write_text(decoded_path, decoded)
        write_text(out_dir / f"{name}.headers.json", json.dumps({"final_url": final_url, "headers": headers}, indent=2))
        rel = relevant_lines(decoded)
        if rel.strip():
            write_text(out_dir / f"{name}.relevant.txt", rel)
        rec.update({"ok": True, "final_url": final_url, "content_type": content_type, "capture_method": headers.get("X-MacPowerLab-Capture-Method"), "file": str(path), "decoded_file": str(decoded_path)})
    except Exception as e:
        rec["error"] = str(e)
    return rec

def main():
    ap = argparse.ArgumentParser(description=f"Capture Xcode-Guide research map v{VERSION}")
    ap.add_argument("--max-links", type=int, default=80, help="Max priority links to capture. Default: 80.")
    ap.add_argument("--include-third-party", action="store_true", help="Also capture non-Apple third-party links.")
    args = ap.parse_args()

    root = Path(__file__).resolve().parent
    out = root / "logs" / f"xcode_guide_capture_{datetime.now().strftime('%Y%m%d_%H%M%S')}"
    out.mkdir(parents=True, exist_ok=True)

    print(f"MacPowerLab Xcode Guide Capture v{VERSION}")
    print(f"Output: {out}")
    print()

    seed_urls = [DEFAULT_REPO, RAW_README, API_REPO, API_CONTENTS]
    records = []
    combined_text = []

    for url in seed_urls:
        print(f"Capturing seed: {url}")
        rec = capture_url(url, out)
        records.append(rec)
        if rec.get("ok") and rec.get("decoded_file"):
            p = Path(rec["decoded_file"])
            try:
                combined_text.append((p.name, p.read_text(encoding="utf-8", errors="replace")))
            except Exception:
                pass

    all_seed_text = "\n\n".join(f"===== {name} =====\n{text}" for name, text in combined_text)
    write_text(out / "XCODE_GUIDE_SEED_TEXT.txt", all_seed_text)

    links = set()
    for _, text in combined_text:
        links.update(extract_links(text))

    apple_links = sorted([u for u in links if "developer.apple.com" in u or "help.apple.com/instruments" in u], key=lambda u: priority_score(u), reverse=True)
    third_party = sorted([u for u in links if u not in apple_links], key=lambda u: priority_score(u), reverse=True)

    priority_links = apple_links + (third_party if args.include_third_party else [])
    priority_links = priority_links[: args.max_links]

    write_text(out / "ALL_DISCOVERED_LINKS.txt", "\n".join(sorted(links)) + ("\n" if links else ""))
    write_text(out / "APPLE_DOCS_PRIORITY_LINKS.txt", "\n".join(apple_links) + ("\n" if apple_links else ""))
    write_text(out / "THIRD_PARTY_LINKS.txt", "\n".join(third_party) + ("\n" if third_party else ""))
    write_text(out / "APPLE_DOCS_SEED_URLS.txt", "\n".join(apple_links[:80]) + ("\n" if apple_links else ""))

    print(f"Discovered links: {len(links)}")
    print(f"Apple/doc priority links: {len(apple_links)}")
    print(f"Capturing priority links: {len(priority_links)}")
    print()

    for url in priority_links:
        print(f"Capturing priority: {url}")
        records.append(capture_url(url, out))

    relevant = []
    combined = []
    for p in sorted(out.glob("*.decoded.txt")) + sorted(out.glob("*.relevant.txt")):
        try:
            text = p.read_text(encoding="utf-8", errors="replace")
        except Exception:
            continue
        combined.append(f"\n\n===== {p.name} =====\n{text[:200000]}")
        rel = relevant_lines(text)
        if rel.strip():
            relevant.append(f"\n\n===== {p.name} =====\n{rel}")

    write_text(out / "ALL_CAPTURED_TEXT_COMBINED.txt", "".join(combined))
    write_text(out / "POWER_PERFORMANCE_METAL_RELEVANT_LINES.txt", "".join(relevant) if relevant else "No keyword matches found.\n")

    manifest = {
        "created_at": datetime.now().isoformat(timespec="seconds"),
        "tool_version": VERSION,
        "seed_urls": seed_urls,
        "discovered_link_count": len(links),
        "apple_priority_link_count": len(apple_links),
        "captured_priority_link_count": len(priority_links),
        "records": records,
    }
    write_text(out / "manifest.json", json.dumps(manifest, indent=2))

    summary = f"""MacPowerLab Xcode Guide Capture v{VERSION}

The Xcode Guide is being used as a curated link map for:
- Xcode
- Instruments
- Metal
- Metal Performance Shaders
- GPU Counters
- Memory footprint optimization
- Apple Silicon performance tuning
- Swift / Objective-C / AppKit / SwiftUI
- Core ML / Accelerate

Most useful files:
- APPLE_DOCS_PRIORITY_LINKS.txt
- APPLE_DOCS_SEED_URLS.txt
- POWER_PERFORMANCE_METAL_RELEVANT_LINES.txt
- ALL_CAPTURED_TEXT_COMBINED.txt

Next step:
  ./run_apple_docs_capture.sh --urls-file "{out / 'APPLE_DOCS_SEED_URLS.txt'}" --crawl-discovered --max-discovered 80
"""
    write_text(out / "README_XCODE_GUIDE_CAPTURE.txt", summary)

    print()
    print("Xcode Guide capture complete.")
    print(f"Folder: {out}")
    print(f"Apple docs seed URLs: {out / 'APPLE_DOCS_SEED_URLS.txt'}")
    print("Recommended next command:")
    print(f'  ./run_apple_docs_capture.sh --urls-file "{out / "APPLE_DOCS_SEED_URLS.txt"}" --crawl-discovered --max-discovered 80')
    print("Then pack everything with:")
    print("  ./pack_logs.sh")

if __name__ == "__main__":
    main()
