#!/usr/bin/env python3
"""Queue idempotent credential-free evidence cards for ready Wayfinder tasks."""
from __future__ import annotations
import argparse,json,os,subprocess,sys
from dataclasses import dataclass
from pathlib import Path
from typing import Callable,Iterable
ROOT=Path(__file__).resolve().parents[1]; REPOSITORY="KalebCole/partiful"; BOARD="partiful"; TASK_LABEL="wayfinder:task"
@dataclass(frozen=True)
class Issue: number:int; title:str; url:str; state:str; labels:tuple[str,...]; assignees:tuple[str,...]; blocked_by:tuple[tuple[int,str],...]; body:str
def select_frontier(issues:Iterable[Issue])->dict:
 selected=[];held=[]
 for i in sorted(issues,key=lambda x:x.number):
  if 20<=i.number<=29 and i.state=="OPEN" and TASK_LABEL in i.labels and not i.assignees and not any(s=="OPEN" for _,s in i.blocked_by): selected.append(i)
 return {"selected":selected,"held":held}
def build_body(i:Issue)->str:return f'''Evidence mode for GitHub task #{i.number}: {i.url}. Use dedicated `partiful-evidence` profile and terminal/file/skills only. Perform the narrowest bounded probe: bounded credential-free public/repository investigation is allowed; redact values and report sources. A reviewed `unsupported` conclusion is permitted. Use no credentials: never seek, use, recover, import, or create credentials; no live mutation. GitHub blockers naturally hold blocked tasks. Do not create review or integrate child cards.'''
def _run(c:list[str])->str:
 r=subprocess.run(c,cwd=ROOT,text=True,capture_output=True,env={"PATH":"/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin","HOME":os.environ.get("HOME",str(Path.home()))})
 if r.returncode:raise RuntimeError(r.stderr.strip() or r.stdout.strip())
 return r.stdout
def fetch_issues(run:Callable[[list[str]],str]=_run)->list[Issue]:
 q='''query($owner:String!,$name:String!){repository(owner:$owner,name:$name){issue(number:8){subIssues(first:100){nodes{number title url body state labels(first:20){nodes{name}} assignees(first:10){nodes{login}} blockedBy(first:20){nodes{number state}}}}}}}''';o,n=REPOSITORY.split("/");nodes=json.loads(run(["gh","api","graphql","-F",f"owner={o}","-F",f"name={n}","-f",f"query={q}"]))["data"]["repository"]["issue"]["subIssues"]["nodes"]
 return [Issue(x["number"],x["title"],x["url"],x["state"],tuple(y["name"] for y in x["labels"]["nodes"]),tuple(y["login"] for y in x["assignees"]["nodes"]),tuple((y["number"],y["state"]) for y in x["blockedBy"]["nodes"]),x["body"]) for x in nodes]
def idempotency_key(i:Issue)->str:return f"partiful:evidence:{i.number}"
def discover_live_cards(run:Callable[[list[str]],str]=_run)->list[dict]:
 raw=json.loads(run(["hermes","kanban","--board",BOARD,"list","--json"]));return raw.get("tasks",raw if isinstance(raw,list) else [])
def create_card(i:Issue,run:Callable[[list[str]],str]=_run)->str:
 v=json.loads(run(["hermes","kanban","--board",BOARD,"create",f"evidence: #{i.number} {i.title}","--body",build_body(i),"--assignee","partiful-evidence","--workspace",f"dir:{ROOT}","--tenant","partiful-wayfinder","--idempotency-key",idempotency_key(i),"--goal","--goal-max-turns","6","--json"]));return str(v.get("task_id") or v["id"])
def create_missing_cards(issues:Iterable[Issue],cards:Iterable[dict],run:Callable[[list[str]],str]=_run)->list[str]:
 existing={str(card.get("idempotency_key",card.get("idempotencyKey",""))) for card in cards if str(card.get("status",card.get("state",""))).lower() not in {"done","closed","cancelled","completed","archived"}}
 return [create_card(issue,run) for issue in issues if idempotency_key(issue) not in existing]
def main(argv:list[str]|None=None)->int:
 p=argparse.ArgumentParser();p.add_argument("--dry-run",action="store_true");p.add_argument("--quiet",action="store_true");a=p.parse_args(argv)
 try:
  wave=select_frontier(fetch_issues());output={"selected":[x.number for x in wave["selected"]],"held":wave["held"]}
  if a.dry_run:
   if not a.quiet:print(json.dumps(output,sort_keys=True))
   return 0
  _run(["python3",str(ROOT/"scripts/verify_implementation_worker_profiles.py")]); created=create_missing_cards(wave["selected"],discover_live_cards())
  if not a.quiet:print(json.dumps({"created":created,"held":wave["held"]},sort_keys=True))
  return 0
 except (RuntimeError,ValueError,KeyError,json.JSONDecodeError) as e:print(f"FAIL: {e}",file=sys.stderr);return 1
if __name__=="__main__":raise SystemExit(main())
