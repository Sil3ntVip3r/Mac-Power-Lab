#!/usr/bin/env python3
import csv
from datetime import datetime
from pathlib import Path

VERSION = "0.9.0"

def latest_dir(pattern):
    dirs=sorted(Path("logs").glob(pattern), key=lambda p:p.stat().st_mtime, reverse=True)
    return dirs[0] if dirs else None
def latest_file(pattern):
    files=sorted(Path("logs").glob(pattern), key=lambda p:p.stat().st_mtime, reverse=True)
    return files[0] if files else None
def read_text(path):
    if not path or not Path(path).exists(): return ""
    return Path(path).read_text(encoding="utf-8",errors="replace")
def csv_headers(path):
    if not path or not Path(path).exists(): return []
    with open(path,newline="",encoding="utf-8",errors="replace") as f:
        r=csv.reader(f)
        return next(r,[])
def main():
    out=Path("logs")/f"beta_compatibility_report_{datetime.now().strftime('%Y%m%d_%H%M%S')}.md"
    docs=latest_dir("apple_docs_capture_*") or latest_dir("apple_docs_import_*")
    guide=latest_dir("xcode_guide_capture_*")
    xcode=latest_dir("xcode_environment_scan_*")
    csv_path=latest_file("mac_power_*.csv")
    lines=[f"# MacPowerLab Beta Compatibility Report v{VERSION}", "", f"Created: {datetime.now().isoformat(timespec='seconds')}", ""]
    lines += ["## Inputs", f"- Apple docs capture/import: `{docs or 'not found'}`", f"- Xcode Guide capture: `{guide or 'not found'}`", f"- Xcode environment scan: `{xcode or 'not found'}`", f"- Latest power CSV: `{csv_path or 'not found'}`", ""]
    lines += ["## Capture health checks"]
    if not csv_path:
        lines.append("- WARNING: No mac_power_*.csv found. Start the Power Monitor before benchmark runs.")
        lines.append("- Recommended command: `./run_complete_beta_capture.sh`")
    if guide:
        manifest = read_text(Path(guide)/"manifest.json")
        if "CERTIFICATE_VERIFY_FAILED" in manifest:
            lines.append("- WARNING: Xcode Guide capture hit certificate verification errors. v0.8.6 uses curl/insecure public-doc fallback.")
        if '"discovered_link_count": 0' in manifest:
            lines.append("- WARNING: Xcode Guide capture found 0 links. Rerun with v0.8.6.")
    if xcode:
        status = read_text(Path(xcode)/"xcode_status_summary.txt")
        if status:
            lines.append("")
            lines.append("```text")
            lines.append(status[:3000])
            lines.append("```")
    lines.append("")

    if csv_path:
        headers=csv_headers(csv_path)
        lines += ["## Sensor columns detected in latest CSV"]
        for h in ["primary_total_load_w","primary_total_load_source","actual_battery_draw_w","bms_system_power_w","telemetry_system_effective_total_load_w","pd_ipd_input_power_w","stress_cpu_percent","p_cluster_active_percent","e_cluster_active_percent","battery_charge_acceptance_w"]:
            lines.append(f"- {h}: {'YES' if h in headers else 'NO'}")
        lines.append("")
    if guide:
        guide_rel=Path(guide)/"POWER_PERFORMANCE_METAL_RELEVANT_LINES.txt"
        guide_text=read_text(guide_rel)
        lines += ["## Xcode Guide power/performance/Metal matches", ""]
        if guide_text.strip():
            lines.append("```text")
            lines.append(guide_text[:20000])
            lines.append("```")
        else:
            lines.append("No keyword matches found yet.")
        lines.append("")

    if docs:
        rel=Path(docs)/"POWER_RELEVANT_LINES.txt"
        text=read_text(rel)
        lines += ["## Apple docs power/performance/Metal/Xcode matches", ""]
        if text.strip():
            lines.append("```text")
            lines.append(text[:20000])
            lines.append("```")
        else:
            lines.append("No keyword matches found yet.")
        lines.append("")
    if xcode:
        lines += ["## Xcode/toolchain snapshot", ""]
        for name in ["xcodebuild_version.txt","xcrun_show_sdk_version.txt","xcrun_show_sdk_build_version.txt","swiftc_version.txt","clang_version.txt","metal_version.txt"]:
            p=Path(xcode)/name
            if p.exists():
                lines.append(f"### {name}")
                lines.append("```text")
                lines.append(read_text(p)[:4000])
                lines.append("```")
        lines.append("")
    lines += ["## MacPowerLab interpretation", "- Use this report to compare macOS beta builds against MacPowerLab sensor availability and benchmark results.", "- If Apple docs mention battery/power/thermal/performance changes, map them to benchmark phases.", "- If Xcode/Metal changes are present, verify GPU stress compilation and cluster/process attribution after the update.", ""]
    out.write_text("\n".join(lines),encoding="utf-8")
    print(f"Beta compatibility report written: {out}")
if __name__=="__main__":
    main()
