#!/usr/bin/env python3
"""
MacPowerLab Apple Developer Docs Capture v0.8.6

Captures as much as possible from Apple Developer documentation using:
1. Raw HTTP
2. Apple DocC JSON endpoint guesses
3. Optional Safari-rendered capture through AppleScript
4. Keyword extraction for battery/power/performance/Metal/Xcode terms

v0.8.6 fixes:
- Public docs capture falls back through unverified TLS and curl -k -L when the
  local Python certificate store fails.
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

DEFAULT_URLS = [
    "https://developer.apple.com/documentation/macos-release-notes/macos-27-release-notes",
    "https://developer.apple.com/documentation/xcode",
    "https://developer.apple.com/documentation/metal",
    "https://developer.apple.com/documentation/metalkit",
    "https://developer.apple.com/documentation/accelerate",
    "https://developer.apple.com/documentation/foundation",
    "https://developer.apple.com/documentation/appkit",
    "https://developer.apple.com/documentation/kernel",
    "https://developer.apple.com/documentation/iokit",
    "https://developer.apple.com/documentation/system",
    "https://developer.apple.com/documentation/os/oslog",
    "https://developer.apple.com/documentation/metal/optimizing_your_metal_app",
    "https://developer.apple.com/documentation/metal/gpu_counters_and_counter_sample_buffers",
    "https://developer.apple.com/documentation/metal/metal_sample_code_library",
    "https://developer.apple.com/documentation/metalperformanceshaders",
    "https://developer.apple.com/documentation/coreml",
    "https://help.apple.com/instruments/mac/current/",
]

KEYWORDS = [
    "battery", "batteries", "power", "charging", "charger", "thermal", "temperature",
    "performance", "energy", "low power", "high power", "activity monitor",
    "instruments", "xcode", "metal", "gpu", "cpu", "memory", "process",
    "background", "daemon", "spotlight", "indexing", "powermetrics", "iokit",
    "ioreg", "thunderbolt", "usb-c", "usbc", "sleep", "wake", "display",
    "windowserver", "sensor", "telemetry", "developer beta", "release notes",
]

def http_get(url, timeout=30):
    headers_req = {
        "User-Agent": "MacPowerLab/0.7.3 AppleDocsCapture",
        "Accept": "text/html,application/json,text/plain,*/*",
    }
    req = urllib.request.Request(url, headers=headers_req)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            body = r.read()
            headers = dict(r.headers.items())
            headers["X-MacPowerLab-Capture-Method"] = "python-default-tls"
            return 200, r.geturl(), headers, body
    except Exception as first_error:
        try:
            ctx = ssl._create_unverified_context()
            with urllib.request.urlopen(req, timeout=timeout, context=ctx) as r:
                body = r.read()
                headers = dict(r.headers.items())
                headers["X-MacPowerLab-Capture-Method"] = "python-unverified-tls"
                headers["X-MacPowerLab-First-Error"] = str(first_error)
                return 200, r.geturl(), headers, body
        except Exception as second_error:
            try:
                p = subprocess.run(
                    ["curl", "-k", "-L", "--max-time", str(timeout), "-A", "MacPowerLab/0.7.3 AppleDocsCapture", "-sS", "-D", "-", url],
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
                return 200, url, headers, body
            except Exception as third_error:
                raise RuntimeError(
                    f"default TLS failed: {first_error}; "
                    f"unverified TLS failed: {second_error}; "
                    f"curl fallback failed: {third_error}"
                )

def safe_name(url: str) -> str:
    parsed = urllib.parse.urlparse(url)
    path = (parsed.netloc + parsed.path).strip("/").replace("/", "__")
    path = re.sub(r"[^A-Za-z0-9_.-]+", "_", path)
    return path[:180] or "root"

def write_text(path: Path, text: str):
    path.write_text(text, encoding="utf-8", errors="replace")

def text_from_json(obj, prefix=""):
    lines = []
    def walk(x, key_path=""):
        if isinstance(x, dict):
            for k in ("title", "abstract", "discussion", "roleHeading", "name", "identifier", "path"):
                if k in x and isinstance(x[k], str):
                    lines.append(f"{key_path}.{k}: {x[k]}")
            for k, v in x.items():
                walk(v, f"{key_path}.{k}" if key_path else k)
        elif isinstance(x, list):
            for i, v in enumerate(x):
                walk(v, f"{key_path}[{i}]")
        elif isinstance(x, str):
            s = x.strip()
            if s:
                lines.append(f"{key_path}: {s}")
    walk(obj, prefix)
    seen = set()
    out = []
    for line in lines:
        if line not in seen:
            seen.add(line)
            out.append(line)
    return "\n".join(out) + "\n"

def docc_endpoint_candidates(url: str):
    parsed = urllib.parse.urlparse(url)
    path = parsed.path
    doc_path = path[len("/documentation/"):] if path.startswith("/documentation/") else path.strip("/")
    candidates = []
    if doc_path:
        candidates.append(f"https://developer.apple.com/tutorials/data/documentation/{doc_path}.json")
        candidates.append(f"https://developer.apple.com/tutorials/data/documentation/{doc_path}")
        parts = doc_path.split("/")
        if parts:
            candidates.append(f"https://developer.apple.com/tutorials/data/documentation/{parts[0]}.json")
    return list(dict.fromkeys(candidates))

def useful_discovered_link(url: str) -> bool:
    u = url.lower()
    if any(x in u for x in ["/assets/", ".css", ".js", ".png", ".jpg", ".jpeg", ".svg", ".woff", "/wss/fonts"]):
        return False
    if "developer.apple.com/documentation" in u:
        return True
    if "help.apple.com/instruments" in u:
        return True
    if "developer.apple.com/metal" in u or "developer.apple.com/xcode" in u or "developer.apple.com/machine-learning" in u:
        return True
    return False


def extract_links_from_text(text: str):
    found = set()
    for m in re.finditer(r'https://developer\.apple\.com/[^\s"<>\\)]+', text):
        u = m.group(0).rstrip(".,;")
        if useful_discovered_link(u):
            found.add(u)
    for m in re.finditer(r'href=["\']([^"\']+)["\']', text):
        href = m.group(1)
        if href.startswith("/"):
            u = "https://developer.apple.com" + href
            if useful_discovered_link(u):
                found.add(u)
        elif href.startswith("https://developer.apple.com"):
            u = href.rstrip(".,;")
            if useful_discovered_link(u):
                found.add(u)
    return sorted(found)

def relevant_lines(text: str):
    lines = text.splitlines()
    out = []
    for i, line in enumerate(lines, 1):
        low = line.lower()
        if any(k in low for k in KEYWORDS):
            out.append(f"L{i}: {line}")
    return "\n".join(out) + ("\n" if out else "")

def run_osascript(script: str, timeout=90):
    return subprocess.run(["osascript", "-e", script], text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout)

def safari_capture(url: str, out_dir: Path, wait_seconds: int):
    base = safe_name(url)
    result = {"url": url, "strategy": "safari", "ok": False, "error": None}
    escaped = url.replace("\\", "\\\\").replace('"', '\\"')
    text_script = (
        'tell application "Safari"\n'
        'activate\n'
        f'set docRef to make new document with properties {{URL:"{escaped}"}}\n'
        f'delay {wait_seconds}\n'
        'try\n'
        'set pageText to do JavaScript "document.body.innerText" in docRef\n'
        'on error errMsg\n'
        'set pageText to "SAFARI_CAPTURE_ERROR: " & errMsg\n'
        'end try\n'
        'return pageText\n'
        'end tell\n'
    )
    html_script = (
        'tell application "Safari"\n'
        'try\n'
        'set docRef to front document\n'
        'set pageHTML to do JavaScript "document.documentElement.outerHTML" in docRef\n'
        'on error errMsg\n'
        'set pageHTML to "SAFARI_CAPTURE_ERROR: " & errMsg\n'
        'end try\n'
        'return pageHTML\n'
        'end tell\n'
    )
    try:
        txt = run_osascript(text_script, timeout=max(60, wait_seconds + 60))
        html = run_osascript(html_script, timeout=60)
        text_path = out_dir / f"{base}.safari_text.txt"
        html_path = out_dir / f"{base}.safari_html.html"
        write_text(text_path, txt.stdout + ("\n\nSTDERR:\n" + txt.stderr if txt.stderr else ""))
        write_text(html_path, html.stdout + ("\n\nSTDERR:\n" + html.stderr if html.stderr else ""))
        result.update({"ok": txt.returncode == 0 and html.returncode == 0, "error": (txt.stderr or html.stderr or None), "text_file": str(text_path), "html_file": str(html_path)})
    except Exception as e:
        result["error"] = str(e)
    return result

def capture_url(url: str, out_dir: Path, include_json=True):
    base = safe_name(url)
    records = []
    rec = {"url": url, "strategy": "raw_http", "ok": False}
    try:
        status, final_url, headers, body = http_get(url)
        content_type = headers.get("Content-Type", "")
        ext = ".json" if "json" in content_type.lower() else ".html"
        raw_path = out_dir / f"{base}.raw{ext}"
        hdr_path = out_dir / f"{base}.headers.json"
        raw_path.write_bytes(body)
        write_text(hdr_path, json.dumps({"status": status, "final_url": final_url, "headers": headers}, indent=2))
        rec.update({"ok": True, "status": status, "final_url": final_url, "file": str(raw_path), "headers_file": str(hdr_path), "capture_method": headers.get("X-MacPowerLab-Capture-Method")})
        text = body.decode("utf-8", errors="replace")
        write_text(out_dir / f"{base}.raw_text.txt", text)
        rel = relevant_lines(text)
        if rel.strip():
            write_text(out_dir / f"{base}.relevant.txt", rel)
    except Exception as e:
        rec["error"] = str(e)
    records.append(rec)

    if include_json:
        for jurl in docc_endpoint_candidates(url):
            jbase = safe_name(jurl)
            jrec = {"url": jurl, "strategy": "docc_json_guess", "source_url": url, "ok": False}
            try:
                status, final_url, headers, body = http_get(jurl)
                raw_path = out_dir / f"{jbase}.json"
                hdr_path = out_dir / f"{jbase}.headers.json"
                raw_path.write_bytes(body)
                write_text(hdr_path, json.dumps({"status": status, "final_url": final_url, "headers": headers}, indent=2))
                jrec.update({"ok": True, "status": status, "final_url": final_url, "file": str(raw_path), "headers_file": str(hdr_path), "capture_method": headers.get("X-MacPowerLab-Capture-Method")})
                try:
                    obj = json.loads(body.decode("utf-8", errors="replace"))
                    extracted = text_from_json(obj)
                    extracted_path = out_dir / f"{jbase}.extracted.txt"
                    write_text(extracted_path, extracted)
                    rel = relevant_lines(extracted)
                    if rel.strip():
                        write_text(out_dir / f"{jbase}.relevant.txt", rel)
                except Exception as e:
                    jrec["json_extract_error"] = str(e)
            except Exception as e:
                jrec["error"] = str(e)
            records.append(jrec)
    return records

def build_summary(out_dir: Path, manifest: dict):
    all_text = []
    relevant = []
    links = set()
    for p in sorted(out_dir.glob("*")):
        if p.suffix.lower() in (".txt", ".html", ".json"):
            try:
                txt = p.read_text(encoding="utf-8", errors="replace")
            except Exception:
                continue
            links.update(extract_links_from_text(txt))
            if p.name.endswith((".txt", ".html")):
                all_text.append(f"\n\n===== {p.name} =====\n{txt[:200000]}")
                rel = relevant_lines(txt)
                if rel.strip():
                    relevant.append(f"\n\n===== {p.name} =====\n{rel}")
    write_text(out_dir / "ALL_CAPTURED_TEXT_COMBINED.txt", "".join(all_text))
    write_text(out_dir / "POWER_RELEVANT_LINES.txt", "".join(relevant) if relevant else "No keyword matches found.\n")
    write_text(out_dir / "DISCOVERED_APPLE_DEVELOPER_LINKS.txt", "\n".join(sorted(links)) + ("\n" if links else ""))
    report = [f"MacPowerLab Apple Docs Capture v{VERSION}", f"Created: {manifest['created_at']}", "", "Seed URLs:", *[f"- {u}" for u in manifest["seed_urls"]], "", "Capture results:"]
    for rec in manifest["records"]:
        report.append(f"- {rec.get('strategy')}: {rec.get('url')} :: {'OK' if rec.get('ok') else 'FAILED'} :: {rec.get('capture_method','')}")
        if rec.get("error"):
            report.append(f"  error: {rec.get('error')}")
    report += ["", "Important files:", "- POWER_RELEVANT_LINES.txt", "- ALL_CAPTURED_TEXT_COMBINED.txt", "- DISCOVERED_APPLE_DEVELOPER_LINKS.txt", "- manifest.json", "", "If Safari capture fails, enable Safari > Develop > Allow JavaScript from Apple Events, then rerun with --safari."]
    write_text(out_dir / "README_CAPTURE_SUMMARY.txt", "\n".join(report) + "\n")

def main():
    parser = argparse.ArgumentParser(description=f"Apple Developer Docs capture for MacPowerLab v{VERSION}")
    parser.add_argument("--url", action="append", help="Add a seed URL. Can be used multiple times.")
    parser.add_argument("--urls-file", help="Text file of URLs, one per line.")
    parser.add_argument("--safari", action="store_true", help="Also capture rendered page text/html through Safari AppleScript.")
    parser.add_argument("--wait", type=int, default=8, help="Seconds to wait for Safari page render. Default: 8.")
    parser.add_argument("--crawl-discovered", action="store_true", help="Capture discovered developer.apple.com links from first-pass pages. Conservative depth 1.")
    parser.add_argument("--max-discovered", type=int, default=25, help="Max discovered links to capture. Default: 25.")
    args = parser.parse_args()

    root = Path(__file__).resolve().parent
    stamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    out_dir = root / "logs" / f"apple_docs_capture_{stamp}"
    out_dir.mkdir(parents=True, exist_ok=True)

    urls = list(DEFAULT_URLS)
    if args.url:
        urls.extend(args.url)
    if args.urls_file:
        for line in Path(args.urls_file).read_text(encoding="utf-8", errors="replace").splitlines():
            line = line.strip()
            if line and not line.startswith("#"):
                urls.append(line)
    urls = list(dict.fromkeys(urls))

    print(f"MacPowerLab Apple Docs Capture v{VERSION}")
    print(f"Output: {out_dir}")
    print(f"Seed URLs: {len(urls)}")
    print()

    records = []
    for url in urls:
        print(f"Capturing: {url}")
        records.extend(capture_url(url, out_dir, include_json=True))
        if args.safari:
            print(f"  Safari render capture: {url}")
            records.append(safari_capture(url, out_dir, args.wait))

    if args.crawl_discovered:
        print("Finding discovered links for conservative depth-1 capture...")
        links = set()
        for p in out_dir.glob("*"):
            if p.suffix.lower() in (".txt", ".html", ".json"):
                try:
                    links.update(extract_links_from_text(p.read_text(encoding="utf-8", errors="replace")))
                except Exception:
                    pass
        discovered = [u for u in sorted(links) if u not in urls]
        for url in discovered[: args.max_discovered]:
            print(f"Discovered capture: {url}")
            records.extend(capture_url(url, out_dir, include_json=True))

    manifest = {"created_at": datetime.now().isoformat(timespec="seconds"), "tool_version": VERSION, "seed_urls": urls, "safari_enabled": args.safari, "records": records}
    write_text(out_dir / "manifest.json", json.dumps(manifest, indent=2))
    build_summary(out_dir, manifest)

    print()
    print("Apple docs capture complete.")
    print(f"Folder: {out_dir}")
    print("Most useful files:")
    print(f"  {out_dir / 'POWER_RELEVANT_LINES.txt'}")
    print(f"  {out_dir / 'ALL_CAPTURED_TEXT_COMBINED.txt'}")
    print(f"  {out_dir / 'README_CAPTURE_SUMMARY.txt'}")
    print()
    print("Pack everything with:")
    print("  ./pack_logs.sh")

if __name__ == "__main__":
    main()
