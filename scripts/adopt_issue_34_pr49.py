#!/usr/bin/env python3
"""Adopt existing PR #49 as stable issue #34 review card; never runs automatically."""
from __future__ import annotations
import argparse,json,subprocess
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]; SHA="27119290015d4d29e0e6f128788128c2b06a4e50"
def main(argv=None)->int:
 p=argparse.ArgumentParser();p.add_argument("--apply",action="store_true");p.add_argument("--quiet",action="store_true");a=p.parse_args(argv)
 if not a.apply:
  if not a.quiet:print(json.dumps({"issue":34,"pr":49,"sha":SHA,"action":"dry-run"}))
  return 0
 command=["hermes","kanban","--board","partiful","create","implement: #34 application kernel","--assignee","partiful-code-reviewer","--workspace",f"worktree:{ROOT}","--branch","partiful/issue-34","--tenant","partiful-wayfinder","--idempotency-key","partiful:implementation:34","--body",f"Adopt existing PR #49 directly in review phase. Exact SHA: {SHA}. Do not rerun implementation or create children. Review using docs/agentic-engineering.md and native same-card lifecycle.","--json"]
 r=subprocess.run(command,cwd=ROOT,text=True,capture_output=True)
 if r.returncode:print(f"FAIL: {r.stderr.strip() or r.stdout.strip()}");return 1
 if not a.quiet:print(r.stdout)
 return 0
if __name__=="__main__":raise SystemExit(main())
