#!/usr/bin/env python3
"""Idempotently create one conflict-free Partiful implementation card per issue."""
from __future__ import annotations
import argparse, json, os, re, subprocess, sys
from dataclasses import dataclass
from pathlib import Path
from typing import Callable, Iterable
try: from scripts.select_implementation_wave import select_wave
except ModuleNotFoundError: from select_implementation_wave import select_wave
ROOT=Path(__file__).resolve().parents[1]; REPOSITORY,MAP_ISSUE,BOARD="KalebCole/partiful",8,"partiful"; IMPLEMENTATION_LABEL="partiful:implementation"; WORKSPACE=f"worktree:{ROOT}"
GRAPHQL='''query($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name){issue(number:$number){subIssues(first:100){nodes{number title body state url labels(first:20){nodes{name}} assignees(first:10){nodes{login}} blockedBy(first:20){nodes{number state}}}}}}}'''
@dataclass(frozen=True)
class Issue:
 number:int; title:str; url:str; state:str; labels:tuple[str,...]; assignees:tuple[str,...]; blocked_by:tuple[tuple[int,str],...]; allowed_paths:tuple[str,...]=()
def parse_allowed_files(body:str)->tuple[str,...]:
 match=re.search(r"(?ims)^Allowed files:[ \t]*(.*?)(?=^[ \t]*\r?$|^\s*(?:##|Applicable |Native |Shared |Forbidden:)|\Z)",body)
 return tuple(re.findall(r'`([^`]+)`',match.group(1) if match else ""))
def _allowed(body:str)->tuple[str,...]: return parse_allowed_files(body)
def parse_issues(payload:dict)->list[Issue]:
 nodes=payload["data"]["repository"]["issue"]["subIssues"]["nodes"]
 return [Issue(n["number"],n["title"],n["url"],n["state"],tuple(x["name"] for x in n["labels"]["nodes"]),tuple(x["login"] for x in n["assignees"]["nodes"]),tuple((x["number"],x["state"]) for x in n["blockedBy"]["nodes"]),parse_allowed_files(n.get("body",""))) for n in nodes]
def _run(cmd:list[str])->str:
 env={"HOME": os.environ.get("HOME", str(Path.home()))}
 env["PATH"] = f'{env["HOME"]}/.hermes/hermes-agent/venv/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin'
 r=subprocess.run(cmd,cwd=ROOT,text=True,capture_output=True,env=env)
 if r.returncode: raise RuntimeError(r.stderr.strip() or r.stdout.strip())
 return r.stdout
def fetch_issues(run:Callable[[list[str]],str]=_run)->list[Issue]:
 owner,name=REPOSITORY.split("/"); return parse_issues(json.loads(run(["gh","api","graphql","-F",f"owner={owner}","-F",f"name={name}","-F",f"number={MAP_ISSUE}","-f",f"query={GRAPHQL}"])))
def select_frontier(issues:Iterable[Issue])->list[Issue]: return sorted([i for i in issues if i.state=="OPEN" and IMPLEMENTATION_LABEL in i.labels and not i.assignees and not any(s=="OPEN" for _,s in i.blocked_by)],key=lambda i:i.number)
def discover_live_cards(run:Callable[[list[str]],str]=_run)->list[dict]:
 raw=json.loads(run(["hermes","kanban","--board",BOARD,"list","--json"])); cards=raw if isinstance(raw,list) else raw.get("tasks",[])
 result=[]
 for c in cards:
  key=c.get("idempotency_key",c.get("idempotencyKey","")); status=str(c.get("status",c.get("state",""))).lower()
  match=re.fullmatch(r"partiful:implementation:(\d+)",key)
  if match and status not in {"done","closed","cancelled","completed"}:
   issue=int(match.group(1)); paths=list(parse_allowed_files(c.get("body",""))); card=c.get("id")
   if not paths: raise RuntimeError(f"card {card} for issue #{issue} has no allowed paths")
   result.append({"issue":issue,"paths":paths,"card":card})
 return result
def select_wave_for_issues(issues:Iterable[Issue],cards:Iterable[dict])->dict:
 issue_list=list(issues)
 for issue in issue_list:
  if not issue.allowed_paths: raise RuntimeError(f"issue #{issue.number} has no allowed paths")
 return select_wave([{"number":i.number,"paths":list(i.allowed_paths)} for i in issue_list],cards)
def build_implement_body(issue:Issue)->str:
 return f'''Implement GitHub issue #{issue.number}: {issue.url}

Read `docs/agentic-engineering.md` and the GitHub issue (canonical requirements, allowed files, blockers, and commands). Use no credentials and make no live mutations. No child review/revision/integrator cards.

## Implementer phase
Feature/Bug/Refactor mode only as declared in the issue. Use strict feature/bug TDD, domain vocabulary and public seams. Record strict RED then GREEN command/output proof, focused/full verification, exact changed paths, PR URL and exact 40-character head SHA. Open/update PR, post and perform PR+handoff readback, then native `request-review` to `partiful-code-reviewer` on this same card with PR URL and SHA.

Allowed files: {", ".join(f'`{path}`' for path in issue.allowed_paths)}.

## Reviewer phase
After native request-review, checkout the exact PR head at the exact 40-character SHA (exact 40-character PR SHA) and prove detached HEAD equality. Run all nine categories: specification, correctness, domain_model, test_quality, edge_cases, security_privacy, maintainability, domain_adherence, evidence_rigor. Post machine-parseable structured verdict `## Implementation review`, `Verdict: APPROVE|REQUEST_CHANGES`, `Commit: <40-sha>`, `Category-<name>: PASS|FAIL` (for example `Category-specification: PASS`), `RED:`, and `GREEN:`. Count structural review events/structured reviews. On request changes, native-return this same card to `partiful-implementer` with evidence-block; after attempt 3 hard-block (max 3 reviews). On approval invoke `scripts/deterministic_merge_gate.py`. Escalate only contradictory requirements, safety choices, or genuinely unresolved behavior; architectural boundaries do not escalate.'''
def build_review_body(issue:Issue)->str: return build_implement_body(issue)
def request_native_review(card:str,pr_url:str,sha:str,run:Callable[[list[str]],str]=_run)->None:
 if not re.fullmatch(r"[0-9a-f]{40}",sha):raise ValueError("exact 40-character SHA required")
 run(["hermes","kanban","--board",BOARD,"request-review",card,"--reviewer","partiful-code-reviewer","--summary",f"PR: {pr_url}; exact SHA: {sha}","--metadata",json.dumps({"pr_url":pr_url,"sha":sha},sort_keys=True)])
def request_changes_on_same_card(card:str,evidence_block:str,run:Callable[[list[str]],str]=_run)->None:
 if not evidence_block.strip():raise ValueError("evidence block required")
 run(["hermes","kanban","--board",BOARD,"request-changes",card,evidence_block])
def _task_id(raw:str)->str:
 obj=json.loads(raw); value=obj.get("task_id") or obj.get("id")
 if not value: raise RuntimeError("Kanban create returned no task id")
 return str(value)
def create_card(issue:Issue,run:Callable[[list[str]],str]=_run)->str:
 return _task_id(run(["hermes","kanban","--board",BOARD,"create",f"implement: #{issue.number} {issue.title}","--body",build_implement_body(issue),"--assignee","partiful-implementer","--workspace",WORKSPACE,"--branch",f"partiful/issue-{issue.number}","--tenant","partiful-wayfinder","--priority","80","--idempotency-key",f"partiful:implementation:{issue.number}","--max-runtime","2h","--max-retries","3","--goal","--goal-max-turns","30","--skill","test-driven-development","--json"]))
def main(argv:list[str]|None=None)->int:
 p=argparse.ArgumentParser(description=__doc__);p.add_argument("--issue",type=int);p.add_argument("--dry-run",action="store_true");p.add_argument("--quiet",action="store_true");a=p.parse_args(argv)
 try:
  ready=select_frontier(fetch_issues()); ready=[x for x in ready if a.issue is None or x.number==a.issue]; wave=select_wave_for_issues(ready,discover_live_cards())
  if a.dry_run:
   if not a.quiet: print(json.dumps(wave,sort_keys=True))
   return 0
  if ready: _run([sys.executable,str(ROOT/"scripts/verify_implementation_worker_profiles.py")])
  existing={x["issue"] for x in discover_live_cards()}; created=[{"issue":x["number"],"card":create_card(next(i for i in ready if i.number==x["number"]))} for x in wave["selected"] if x["number"] not in existing]
  if not a.quiet: print(json.dumps({"created":created,"held":wave["held"]},sort_keys=True))
  return 0
 except (RuntimeError,ValueError,KeyError,json.JSONDecodeError) as e: print(f"FAIL: {e}",file=sys.stderr);return 1
if __name__=="__main__": raise SystemExit(main())
