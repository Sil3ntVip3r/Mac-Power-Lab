#!/usr/bin/env python3
import argparse, json, shutil
from datetime import datetime
from pathlib import Path

VERSION = "0.9.0"
KEYWORDS=["battery","power","charging","thermal","performance","energy","low power","high power","metal","gpu","cpu","memory","xcode","instruments","activity monitor","background","process","powermetrics","iokit","usb-c","thunderbolt","sleep","wake","display"]

def relevant_lines(text):
    out=[]
    for i,line in enumerate(text.splitlines(),1):
        low=line.lower()
        if any(k in low for k in KEYWORDS):
            out.append(f"L{i}: {line}")
    return "\n".join(out)+"\n" if out else ""

def main():
    ap=argparse.ArgumentParser(description="Import copied Apple release notes/docs into MacPowerLab logs.")
    ap.add_argument("files", nargs="+", help="Text/markdown/html files copied from Safari/Apple Developer docs.")
    args=ap.parse_args()
    root=Path(__file__).resolve().parent
    stamp=datetime.now().strftime("%Y%m%d_%H%M%S")
    out=root/"logs"/f"apple_docs_import_{stamp}"
    out.mkdir(parents=True, exist_ok=True)
    manifest={"created_at":datetime.now().isoformat(timespec="seconds"),"tool_version":VERSION,"files":[]}
    combined=[]
    relevant=[]
    for f in args.files:
        src=Path(f).expanduser()
        if not src.exists():
            print(f"Missing: {src}")
            continue
        dst=out/src.name
        shutil.copy2(src,dst)
        text=src.read_text(encoding="utf-8",errors="replace")
        combined.append(f"\n\n===== {src.name} =====\n{text}")
        rel=relevant_lines(text)
        if rel.strip():
            relevant.append(f"\n\n===== {src.name} =====\n{rel}")
        manifest["files"].append({"source":str(src),"copied_to":str(dst),"bytes":src.stat().st_size})
    (out/"ALL_IMPORTED_TEXT_COMBINED.txt").write_text("".join(combined),encoding="utf-8")
    (out/"POWER_RELEVANT_LINES.txt").write_text("".join(relevant) if relevant else "No keyword matches found.\n",encoding="utf-8")
    (out/"manifest.json").write_text(json.dumps(manifest,indent=2),encoding="utf-8")
    print(f"Imported docs into: {out}")
    print(f"Relevant lines: {out/'POWER_RELEVANT_LINES.txt'}")
    print("Pack everything with: ./pack_logs.sh")

if __name__=="__main__":
    main()
