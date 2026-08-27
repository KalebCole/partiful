#!/usr/bin/env python3
"""Queue one evidence card for each credential-free ready Wayfinder task."""
from __future__ import annotations
import argparse,json,subprocess
from dataclasses import dataclass
from pathlib import Path
from typing import Callable,Iterable
ROOT=Path(__file__).resolve().parents[1]; REPOSITORY="KalebCole/partiful"; BOARD="partiful"; TASK_LABEL="wayfinder:task"
@dataclass(frozen=True)
class Issue:
 number:int; title:str; url:str; state:str; labels:tuple[str,...]; assignees:tuple[str,...]; blocked_by:tuple[tuple[int,str],...]; body:str
def capability_required(body:str)->bool:
 text=body.lower(); return any(term in text for term in ("authenticated","live observation","live account","login")) or ("credential" in text and "credential-free" not in text)
def select_frontier(issues:Iterable[Issue])->dict:
 selected=[]; held=[]
 for issue in sorted(issues,key=lambda x:x.number):
  if not (20<=issue.number<=29 and issue.state=="OPEN" and TASK_LABEL in issue.labels and not issue.assignees and not any(s=="OPEN" for _,s in issue.blocked_by)): continue
  if capability_required(issue.body): held.append({"number":issue.number,"reason":"capability_required"})
  else: selected.append(issue)
 return {"selected":selected,"held":held}
def build_body(issue:Issue)->str:
 return f'''Collect bounded repository evidence for GitHub task #{issue.number}: {issue.url}. Use only `wayfinder-resolver`'s audited terminal/file/skills capabilities and repository evidence. Perform the narrowest bounded probe, redact sensitive values, and report exact evidence links/paths. Use no credentials and make no live mutation. Do not create review or integrate child cards; GitHub owns requirements and blockers, Kanban owns this one execution card.'''
def _run(cmd:list[str])->str:
 r=subprocess.run(cmd,cwd=ROOT,text=True,capture_output=True)
 if r.returncode: raise RuntimeError(r.stderr.strip() or r.stdout.strip())
 return r.stdout
def fetch_issues(run:Callable[[list[str]],str]=_run)->list[Issue]:
 query='''query($owner:String!,$name:String!){repository(owner:$owner,name:$name){issue(number:8){subIssues(first:100){nodes{number title url body state labels(first:20){nodes{name}} assignees(first:10){nodes{login}} blockedBy(first:20){nodes{number state}}}}}}}'''
 owner,name=REPOSITORY.split("/",1)
 payload=json.loads(run(["gh","api","graphql","-F",f"owner={owner}","-F",f"name={name}","-f",f"query={query}"]))
 nodes=payload["data"]["repository"]["issue"]["subIssues"]["nodes"]
 return [Issue(x["number"],x["title"],x["url"],x["state"],tuple(i["name"] for i in x["labels"]["nodes"]),tuple(i["login"] for i in x["assignees"]["nodes"]),tuple((i["number"],i["state"]) for i in x["blockedBy"]["nodes"]),x["body"]) for x in nodes]
def create_card(issue:Issue,run:Callable[[list[str]],str]=_run)->str:
 cmd=["hermes","kanban","--board",BOARD,"create",f"evidence: #{issue.number} {issue.title}","--body",build_body(issue),"--assignee","wayfinder-resolver","--workspace",f"dir:{ROOT}","--tenant","partiful-wayfinder","--idempotency-key",f"partiful:evidence:{issue.number}","--goal","--goal-max-turns","6","--skill","wayfinder","--json"]
 value=json.loads(run(cmd)); return str(value.get("task_id") or value["id"])
def main()->int:
 p=argparse.ArgumentParser(description=__doc__);p.add_argument("--dry-run",action="store_true");args=p.parse_args(); wave=select_frontier(fetch_issues())
 if args.dry_run: print(json.dumps({"selected":[x.number for x in wave["selected"]],"held":wave["held"]}));return 0
 print(json.dumps({"created":[create_card(x) for x in wave["selected"]],"held":wave["held"]}));return 0
if __name__=="__main__": raise SystemExit(main())
