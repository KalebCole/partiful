#!/usr/bin/env python3
"""Run decision, implementation, and evidence pumps deterministically."""
from __future__ import annotations
import argparse, os, subprocess, sys
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
def build_commands(root:Path=ROOT)->list[list[str]]:
 names=("verify_implementation_contracts.py","wayfinder_frontier_pump.py","implementation_frontier_pump.py","evidence_frontier_pump.py")
 return [[sys.executable,str(root/"scripts"/name),"--quiet"] for name in names]
def main(argv:list[str]|None=None)->int:
 p=argparse.ArgumentParser(description=__doc__+" Merge is triggered only by same-card reviewer approval.");p.add_argument("--quiet",action="store_true");a=p.parse_args(argv)
 env={"PATH":os.environ.get("PATH","") ,"HOME":os.environ.get("HOME","")}
 for command in build_commands():
  result=subprocess.run(command,cwd=ROOT,text=True,capture_output=True,env=env)
  if result.returncode:
   print(result.stderr.strip() or result.stdout.strip(),file=sys.stderr);return result.returncode
  if not a.quiet and result.stdout:print(result.stdout,end="")
 return 0
if __name__=="__main__":raise SystemExit(main())
