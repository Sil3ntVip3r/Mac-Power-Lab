#!/usr/bin/env python3
import json, subprocess
from datetime import datetime
from pathlib import Path

VERSION = "0.9.0"
COMMANDS = [
    ("xcodebuild_version.txt", ["xcodebuild","-version"]),
    ("xcode_select_path.txt", ["xcode-select","-p"]),
    ("xcrun_show_sdk_version.txt", ["xcrun","--sdk","macosx","--show-sdk-version"]),
    ("xcrun_show_sdk_build_version.txt", ["xcrun","--sdk","macosx","--show-sdk-build-version"]),
    ("xcrun_show_sdk_path.txt", ["xcrun","--sdk","macosx","--show-sdk-path"]),
    ("swiftc_version.txt", ["xcrun","swiftc","--version"]),
    ("clang_version.txt", ["xcrun","clang","--version"]),
    ("metal_version.txt", ["xcrun","metal","-v"]),
    ("metallib_help.txt", ["xcrun","metallib","-help"]),
    ("xctrace_templates.txt", ["xcrun","xctrace","list","templates"]),
    ("xctrace_devices.txt", ["xcrun","xctrace","list","devices"]),
    ("xctrace_path.txt", ["xcrun","-find","xctrace"]),
    ("metal_path.txt", ["xcrun","-find","metal"]),
    ("powermetrics_help.txt", ["powermetrics","--help"]),
    ("system_profiler_developer_tools.json", ["system_profiler","SPDeveloperToolsDataType","-json"]),
    ("system_profiler_displays.json", ["system_profiler","SPDisplaysDataType","-json"]),
    ("system_profiler_power.json", ["system_profiler","SPPowerDataType","-json"]),
    ("system_profiler_thunderbolt.json", ["system_profiler","SPThunderboltDataType","-json"]),
    ("system_profiler_usb.json", ["system_profiler","SPUSBDataType","-json"]),
]
def run(cmd, out, timeout=45):
    rec={"command":cmd,"output":str(out),"ok":False,"error":None}
    try:
        with out.open("w",encoding="utf-8",errors="replace") as f:
            p=subprocess.run(cmd, stdout=f, stderr=subprocess.STDOUT, text=True, timeout=timeout)
        rec["returncode"]=p.returncode
        rec["ok"]=p.returncode==0
    except Exception as e:
        out.write_text(str(e)+"\n",encoding="utf-8")
        rec["error"]=str(e)
    return rec
def main():
    root=Path(__file__).resolve().parent
    out=root/"logs"/f"xcode_environment_scan_{datetime.now().strftime('%Y%m%d_%H%M%S')}"
    out.mkdir(parents=True,exist_ok=True)
    print(f"MacPowerLab Xcode/toolchain scan v{VERSION}")
    print(f"Output: {out}")
    records=[]
    for name,cmd in COMMANDS:
        print("Capturing:", name)
        records.append(run(cmd,out/name))
    (out/"manifest.json").write_text(json.dumps({"created_at":datetime.now().isoformat(timespec="seconds"),"tool_version":VERSION,"records":records},indent=2),encoding="utf-8")
    print()
    print("Xcode/toolchain scan complete.")
    print("Pack everything with: ./pack_logs.sh")
if __name__=="__main__":
    main()
