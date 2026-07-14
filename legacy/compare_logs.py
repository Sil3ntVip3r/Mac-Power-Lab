#!/usr/bin/env python3
import csv, sys
from pathlib import Path
def fnum(v):
    try:
        if v in ('','n/a',None): return None
        return float(v)
    except Exception: return None
def avg(a):
    a=[x for x in a if x is not None]; return sum(a)/len(a) if a else None
def maxv(a):
    a=[x for x in a if x is not None]; return max(a) if a else None
def fmt(v,s='',d=2): return 'n/a' if v is None else f'{v:.{d}f}{s}'
def read(p):
    rows=list(csv.DictReader(Path(p).open()))
    if not rows: return None
    dis=[]; ch=[]
    for r in rows:
        n=fnum(r.get('net_battery_watts'))
        if n is not None and n<0: dis.append(abs(n))
        if n is not None and n>0: ch.append(n)
    last=rows[-1]
    return [Path(p).name,fmt(maxv(dis),' W'),fmt(avg(dis),' W'),fmt(maxv(ch),' W'),fmt(maxv([fnum(r.get('battery_temp_c')) for r in rows]),' °C'),fmt(fnum(last.get('battery_health_percent')),'%'),fmt(fnum(last.get('cycle_count')),'',0),fmt(maxv([fnum(r.get('cell_voltage_delta_mv')) for r in rows]),' mV'),fmt(maxv([fnum(r.get('bms_system_power_w')) for r in rows]),' W'),fmt(maxv([fnum(r.get('soc_power_w')) for r in rows]),' W'),fmt(fnum(last.get('battery_wh_net')),' Wh')]
def main():
    paths=sys.argv[1:] or [str(p) for p in sorted(Path('logs').glob('mac_power_*.csv'),key=lambda p:p.stat().st_mtime,reverse=True)[:5]]
    print('MacPowerLab Log Comparison\n==========================\n')
    print('File | Peak draw | Avg draw | Peak charge | Max temp | Health | Cycles | Cell Δ | BMS peak | SoC peak | Wh net')
    print('--- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---')
    for p in paths:
        r=read(p)
        if r: print(' | '.join(r))
if __name__=='__main__': main()
