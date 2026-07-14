#!/usr/bin/env python3
import json
from pathlib import Path
def fmt(v,s='',d=2):
    if v is None: return 'n/a'
    try: return f'{float(v):.{d}f}{s}'
    except Exception: return str(v)
def main():
    p=Path('logs/history.jsonl')
    if not p.exists(): print('No history database found yet. Generate a report first.'); return
    rows=[]
    for line in p.read_text().splitlines():
        try: rows.append(json.loads(line))
        except Exception: pass
    print('MacPowerLab History\n===================\n')
    print('Date | Peak draw | Max temp | Health | Cycles | Cell Δ | Charger')
    print('--- | --- | --- | --- | --- | --- | ---')
    for r in rows[-30:]:
        print(' | '.join([r.get('created_at','n/a'),fmt(r.get('peak_battery_draw_w'),' W'),fmt(r.get('max_battery_temp_c'),' °C'),fmt(r.get('battery_health_percent'),'%'),fmt(r.get('cycle_count'),'',0),fmt(r.get('cell_delta_max_mv'),' mV'),r.get('charger_verdict','n/a')]))
if __name__=='__main__': main()
