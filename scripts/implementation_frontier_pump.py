#!/usr/bin/env python3
"""Create one durable implementation card for every conflict-free GitHub issue."""
from __future__ import annotations
import argparse, json, subprocess, sys
from dataclasses import dataclass
from pathlib import Path
from typing import Callable, Iterable
try:
    from scripts.select_implementation_wave import select_wave
except ModuleNotFoundError:  # direct script invocation
    from select_implementation_wave import select_wave

ROOT = Path(__file__).resolve().parents[1]
REPOSITORY, MAP_ISSUE, BOARD = "KalebCole/partiful", 8, "partiful"
IMPLEMENTATION_LABEL = "partiful:implementation"
WORKSPACE = f"worktree:{ROOT}"
GRAPHQL = '''query($owner:String!,$name:String!,$number:Int!,$after:String){repository(owner:$owner,name:$name){issue(number:$number){subIssues(first:100,after:$after){nodes{number title body state url labels(first:20){nodes{name}} assignees(first:10){nodes{login}} blockedBy(first:20){nodes{number state}}} pageInfo{hasNextPage endCursor}}}}}'''

@dataclass(frozen=True)
class Issue:
    number:int; title:str; url:str; state:str; labels:tuple[str,...]; assignees:tuple[str,...]; blocked_by:tuple[tuple[int,str],...]; allowed_paths:tuple[str,...] = ()

def _allowed(body:str)->tuple[str,...]:
    line=next((line for line in body.splitlines() if line.startswith("Allowed files:")), "")
    import re
    return tuple(re.findall(r'`([^`]+)`',line))
def parse_issues(payload:dict)->list[Issue]:
    nodes=payload["data"]["repository"]["issue"]["subIssues"]["nodes"]
    return [Issue(n["number"],n["title"],n["url"],n["state"],tuple(x["name"] for x in n["labels"]["nodes"]),tuple(x["login"] for x in n["assignees"]["nodes"]),tuple((x["number"],x["state"]) for x in n["blockedBy"]["nodes"]),_allowed(n.get("body", ""))) for n in nodes]
def _run(command:list[str])->str:
    result=subprocess.run(command,cwd=ROOT,text=True,capture_output=True)
    if result.returncode: raise RuntimeError(result.stderr.strip() or result.stdout.strip())
    return result.stdout
def fetch_issues(run:Callable[[list[str]],str]=_run)->list[Issue]:
    owner,name=REPOSITORY.split("/",1); result=[]; after=None
    while True:
        cmd=["gh","api","graphql","-F",f"owner={owner}","-F",f"name={name}","-F",f"number={MAP_ISSUE}","-f",f"query={GRAPHQL}"]
        if after: cmd += ["-F",f"after={after}"]
        raw=json.loads(run(cmd)); result += parse_issues(raw)
        page=raw["data"]["repository"]["issue"]["subIssues"]["pageInfo"]
        if not page["hasNextPage"]: return result
        after=page["endCursor"]
        if not after: raise RuntimeError("GitHub pagination missing cursor")
def select_frontier(issues:Iterable[Issue])->list[Issue]:
    return sorted([i for i in issues if i.state=="OPEN" and IMPLEMENTATION_LABEL in i.labels and not i.assignees and not any(state=="OPEN" for _,state in i.blocked_by)],key=lambda i:i.number)
def build_implement_body(issue:Issue)->str:
    return f'''Implement GitHub issue #{issue.number}: {issue.url}

Read `docs/agentic-engineering.md`, the issue and execution packet references. Work only in this one isolated worktree on branch `partiful/issue-{issue.number}`. Use strict feature/bug TDD with public-seam RED then GREEN. Allowed files: {", ".join(issue.allowed_paths)}. Run exactly the issue verification commands.

Use no credentials and make no live mutations; never seek, import, recover, or create credentials. Open/update the PR, post the implementation handoff, and perform PR+handoff readback. Then make the native Kanban request-review to `partiful-code-reviewer` with the exact PR URL/head SHA. Do not merge or create child/revision/integrator cards.'''
def build_review_body(issue:Issue)->str:
    return f'''Review the exact PR head for GitHub issue #{issue.number}: {issue.url}. Read `docs/agentic-engineering.md`, checkout the exact PR head, and post one structured verdict (APPROVE or REQUEST_CHANGES), with at most three findings and the reviewed SHA. On APPROVE invoke `scripts/deterministic_merge_gate.py`; on REQUEST_CHANGES return this same card to `partiful-implementer`. Never create a chain; max 3 reviews.'''
def _task_id(raw:str)->str:
    value=json.loads(raw).get("task_id") or json.loads(raw).get("id")
    if not value: raise RuntimeError("Kanban create returned no task id")
    return str(value)
def create_card(issue:Issue, run:Callable[[list[str]],str]=_run)->str:
    cmd=["hermes","kanban","--board",BOARD,"create",f"implement: #{issue.number} {issue.title}","--body",build_implement_body(issue),"--assignee","partiful-implementer","--workspace",WORKSPACE,"--branch",f"partiful/issue-{issue.number}","--tenant","partiful-wayfinder","--priority","80","--idempotency-key",f"partiful:implementation:{issue.number}","--max-runtime","2h","--max-retries","3","--goal","--goal-max-turns","30","--skill","test-driven-development","--skill","collaborative-repository-hygiene","--skill","verification-before-completion","--json"]
    return _task_id(run(cmd))
def main()->int:
    parser=argparse.ArgumentParser(description=__doc__); parser.add_argument("--issue",type=int); parser.add_argument("--dry-run",action="store_true"); parser.add_argument("--active-cards",default="[]"); parser.add_argument("--quiet",action="store_true"); args=parser.parse_args()
    try:
        all_issues=fetch_issues(); ready=select_frontier(all_issues)
        if args.issue is not None: ready=[i for i in ready if i.number==args.issue]
        wave=select_wave([{"number":i.number,"paths":list(i.allowed_paths)} for i in ready],json.loads(args.active_cards))
        lookup={i.number:i for i in ready}; output={"selected":wave["selected"],"held":wave["held"]}
        if args.dry_run:
            if not args.quiet: print(json.dumps(output,sort_keys=True))
            return 0
        if ready: _run([sys.executable,str(ROOT/"scripts/verify_implementation_worker_profiles.py")])
        created=[{"issue":x["number"],"card":create_card(lookup[x["number"]])} for x in wave["selected"]]
        if not args.quiet: print(json.dumps({"created":created,"held":wave["held"]},sort_keys=True))
        return 0
    except (RuntimeError,ValueError,KeyError,json.JSONDecodeError) as error: print(f"FAIL: {error}",file=sys.stderr); return 1
if __name__=="__main__": raise SystemExit(main())
