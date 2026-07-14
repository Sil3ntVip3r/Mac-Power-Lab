#!/usr/bin/env python3
import re
from datetime import datetime
from pathlib import Path

VERSION = "0.9.0"

def latest_probe():
    files = sorted(Path("logs").glob("powermetrics_sampler_probe_*.txt"), key=lambda p: p.stat().st_mtime, reverse=True)
    files = [p for p in files if not p.name.endswith("_summary.txt")]
    return files[0] if files else None

def first(pattern, text):
    m = re.search(pattern, text, re.I | re.M)
    return m.group(1).strip() if m else None

def all_values(pattern, text):
    return [m.group(1).strip() for m in re.finditer(pattern, text, re.I | re.M)]

def main():
    path = latest_probe()
    if not path:
        print("No powermetrics_sampler_probe_*.txt found.")
        raise SystemExit(1)

    text = path.read_text(encoding="utf-8", errors="replace")
    stamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    out = Path("logs") / f"powermetrics_probe_analysis_{stamp}.md"

    supported_samplers = []
    sampler_block = re.search(r"The following samplers are supported by --samplers:(.*?)(?:and the following sampler groups|$)", text, re.S)
    if sampler_block:
        for line in sampler_block.group(1).splitlines():
            m = re.match(r"\s+([a-zA-Z0-9_]+)\s+", line)
            if m:
                supported_samplers.append(m.group(1))

    thermal_levels = all_values(r"Current pressure level:\s*([A-Za-z0-9 _./-]+)", text)
    e_freqs = all_values(r"E-Cluster HW active frequency:\s*([0-9.]+)\s*MHz", text)
    p0_freqs = all_values(r"P0-Cluster HW active frequency:\s*([0-9.]+)\s*MHz", text)
    p1_freqs = all_values(r"P1-Cluster HW active frequency:\s*([0-9.]+)\s*MHz", text)
    gpu_freqs = all_values(r"GPU HW active frequency:\s*([0-9.]+)\s*MHz", text)

    cpu_power = all_values(r"CPU Power:\s*([0-9.]+\s*(?:mW|W))", text)
    gpu_power = all_values(r"GPU Power:\s*([0-9.]+\s*(?:mW|W))", text)

    smc_unsupported = "unrecognized sampler: smc" in text
    thermlog_timeout = "pmset -g thermlog" in text and "Timeout" in text

    lines = [
        f"# MacPowerLab powermetrics probe analysis v{VERSION}",
        "",
        f"Created: {datetime.now().isoformat(timespec='seconds')}",
        f"Source: `{path}`",
        "",
        "## Result",
        "",
        f"- Thermal sampler listed in help: {'YES' if 'thermal' in supported_samplers else 'NO'}",
        f"- Thermal sampler returned pressure level: {'YES' if thermal_levels else 'NO'}",
        f"- Latest thermal pressure level: `{thermal_levels[-1] if thermal_levels else 'n/a'}`",
        f"- CPU cluster frequency/residency available: {'YES' if (e_freqs or p0_freqs or p1_freqs) else 'NO'}",
        f"- GPU frequency available: {'YES' if gpu_freqs else 'NO'}",
        f"- CPU power samples found: {len(cpu_power)}",
        f"- GPU power samples found: {len(gpu_power)}",
        f"- SMC sampler supported: {'NO' if smc_unsupported else 'unknown/possibly'}",
        f"- pmset thermlog one-shot: {'NO, it timed out/live-streamed' if thermlog_timeout else 'not tested or completed'}",
        "",
        "## Best monitor strategy",
        "",
        "- Use `powermetrics --samplers cpu_power,gpu_power,thermal` as the primary sampler.",
        "- Do not use the old `smc` sampler on this Mac; it reports `unrecognized sampler`.",
        "- Use `Current pressure level` for macOS thermal pressure.",
        "- Also keep MacPowerLab battery temperature/trend because macOS can say `Nominal` while battery temperature is still rising.",
        "",
        "## Captured values",
        "",
        f"- E-cluster MHz samples: {', '.join(e_freqs[:8]) or 'n/a'}",
        f"- P0-cluster MHz samples: {', '.join(p0_freqs[:8]) or 'n/a'}",
        f"- P1-cluster MHz samples: {', '.join(p1_freqs[:8]) or 'n/a'}",
        f"- GPU MHz samples: {', '.join(gpu_freqs[:8]) or 'n/a'}",
        f"- Thermal levels: {', '.join(thermal_levels) or 'n/a'}",
        "",
    ]

    out.write_text("\n".join(lines), encoding="utf-8")
    print(f"Probe analysis written: {out}")

if __name__ == "__main__":
    main()
