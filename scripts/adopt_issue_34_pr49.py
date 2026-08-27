#!/usr/bin/env python3
"""Adopt existing PR #49 as stable issue #34 review card; never runs automatically."""
from __future__ import annotations
import argparse,json,subprocess
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]; SHA="27119290015d4d29e0e6f128788128c2b06a4e50"
def adopt(run=None)->str:
 command=["hermes","kanban","--board","partiful","create","implement: #34 application kernel","--assignee","partiful-implementer","--workspace",f"worktree:{ROOT}","--branch","partiful/issue-34-initial-implement","--tenant","partiful-wayfinder","--idempotency-key","partiful:implementation:34","--initial-status","running","--body",f"Adopt existing PR #49 directly in review phase. Exact SHA: {SHA}. Do not rerun implementation or create children. Review using docs/agentic-engineering.md and native same-card lifecycle.","--json"]
 if run is None:
  def run(c:list[str])->str:
   result=subprocess.run(c,cwd=ROOT,text=True,capture_output=True)
   if result.returncode:raise RuntimeError(result.stderr.strip() or result.stdout.strip())
   return result.stdout
 payload=json.loads(run(command)); card=payload.get("task_id") or payload.get("id")
 if not card:raise RuntimeError("Kanban create returned no task id")
 card=str(card)
 state_value=json.loads(run(["hermes","kanban","--board","partiful","show",card,"--json"]))
 status=str(state_value.get("task",state_value).get("status",""))
 if status=="review":return card
 if status!="running":raise RuntimeError(f"adopted card {card} has unexpected status {status!r}")
 run(["hermes","kanban","--board","partiful","request-review",card,"--reviewer","partiful-code-reviewer","--summary",f"Adopt PR #49 at exact SHA {SHA}","--metadata",json.dumps({"issue":34,"pr":49,"sha":SHA},sort_keys=True)])
 return card
def main(argv=None)->int:
 p=argparse.ArgumentParser();p.add_argument("--apply",action="store_true");p.add_argument("--quiet",action="store_true");a=p.parse_args(argv)
 if not a.apply:
  if not a.quiet:print(json.dumps({"issue":34,"pr":49,"sha":SHA,"action":"dry-run"}))
  return 0
 try:
  card=adopt()
  if not a.quiet:print(json.dumps({"card":card,"issue":34,"pr":49,"sha":SHA},sort_keys=True))
  return 0
 except (RuntimeError,KeyError,json.JSONDecodeError) as error:print(f"FAIL: {error}");return 1
if __name__=="__main__":raise SystemExit(main())
