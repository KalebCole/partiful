#!/usr/bin/env python3
"""Fail-closed exact-SHA merge gate for a reviewed Partiful PR."""
from __future__ import annotations
import argparse,fnmatch,json,os,re,shlex,sqlite3,subprocess
from pathlib import Path
from typing import Callable
try: from scripts.select_implementation_wave import _overlap
except ModuleNotFoundError: from select_implementation_wave import _overlap
try: from scripts.checkout_verified_pr_head import checkout_verified_pr_head
except ModuleNotFoundError: from checkout_verified_pr_head import checkout_verified_pr_head
ROOT=Path(__file__).resolve().parents[1]; CATEGORIES=("specification","correctness","domain_model","test_quality","edge_cases","security_privacy","maintainability","domain_adherence","evidence_rigor")
def validate_gate(p:dict)->dict:
 f=[]
 def fail(c,d):f.append({"code":c,"detail":d})
 sha=lambda x:isinstance(x,str) and bool(re.fullmatch(r"[0-9a-f]{40}",x))
 if not sha(p.get("head")) or not sha(p.get("reviewed_sha")) or p.get("head")!=p.get("reviewed_sha"):fail("sha_mismatch","nonempty 40-char reviewed SHA must equal head")
 if p.get("reviewer_provenance") is not True:fail("invalid_reviewer_provenance","approval must come from the live native partiful-code-reviewer run")
 r=p.get("latest_review") or {}
 if r.get("verdict")!="APPROVE" or r.get("sha")!=p.get("head"):fail("latest_review_not_approve","latest structured approval must name head")
 if set((r.get("categories") or {}))!=set(CATEGORIES) or any(v!="PASS" for v in (r.get("categories") or {}).values()):fail("incomplete_review_categories","nine category PASS verdicts required")
 if not (p.get("evidence") or {}).get("red") or not (p.get("evidence") or {}).get("green"):fail("missing_red_green_evidence","recorded RED and GREEN evidence required")
 for path in p.get("paths",[]):
  if any(_overlap(path,x) for x in p.get("excluded_paths",[])):fail("out_of_write_set",path)
  elif not any(_overlap(path,x) for x in p.get("allowed_paths",[])):fail("out_of_write_set",path)
 if any(x.get("state")=="OPEN" for x in p.get("blockers",[])):fail("open_blocker","issue has an open blocker")
 checks=p.get("checks",[]); required=p.get("required_checks",[]); no_required_ci=p.get("no_required_ci")
 if (bool(required) and no_required_ci is True) or (not required and no_required_ci is not True):
  fail("ambiguous_ci_contract","declare nonempty required_checks or no_required_ci=true, never both")
 elif required:
  if not checks:fail("missing_required_checks","required check contexts must exist")
  by_context={x.get("context"):x for x in checks if x.get("context")}
  for context in required:
   if context not in by_context:fail("missing_required_check",context)
   elif by_context[context].get("state")!="SUCCESS":fail("required_check_not_success",context)
  for x in checks:
   if not x.get("context") or x.get("state")!="SUCCESS":fail("required_check_not_success",str(x.get("context","missing")))
 elif not p.get("local_verification_ran"):fail("missing_local_verification","explicit no-required-CI declaration requires local commands")
 if p.get("review_cycles",0)>3:fail("too_many_review_cycles","more than three review cycles")
 return {"ok":not f,"failures":f}
def split_verification_command(command:str)->list[str]:return shlex.split(command)
def _run(c:list[str])->str:
 r=subprocess.run(c,cwd=ROOT,text=True,capture_output=True)
 if r.returncode:raise RuntimeError(r.stderr.strip() or r.stdout.strip())
 return r.stdout
def load_issue_contract(issue:int)->dict:
 raw=json.loads((ROOT/"config/implementation-write-sets.json").read_text())[str(issue)]
 return raw if isinstance(raw,dict) else {"paths":raw,"required_checks":["declared"],"verification":["go test ./..."]}
def _native_reviewer_provenance(issue:int)->bool:
 if os.environ.get("HERMES_PROFILE")!="partiful-code-reviewer" or os.environ.get("HERMES_KANBAN_BOARD")!="partiful":return False
 db,task_id,run_id,claim=(os.environ.get(k,"") for k in ("HERMES_KANBAN_DB","HERMES_KANBAN_TASK","HERMES_KANBAN_RUN_ID","HERMES_KANBAN_CLAIM_LOCK"))
 if not db or not task_id or not run_id.isdigit() or not claim:return False
 try:
  conn=sqlite3.connect(f"file:{Path(db).resolve()}?mode=ro",uri=True)
  try:
   task=conn.execute("SELECT assignee,status,current_run_id,idempotency_key,claim_lock FROM tasks WHERE id=?",(task_id,)).fetchone()
   run=conn.execute("SELECT task_id,profile,status,claim_lock FROM task_runs WHERE id=?",(int(run_id),)).fetchone()
  finally:conn.close()
 except (OSError,sqlite3.Error):return False
 return task==("partiful-code-reviewer","running",int(run_id),f"partiful:implementation:{issue}",claim) and run==(task_id,"partiful-code-reviewer","running",claim)
def _packet(issue:int,pr:int,reviewed_sha:str,run:Callable[[list[str]],str],contract:dict)->dict:
 view=json.loads(run(["gh","pr","view",str(pr),"--json","headRefOid,files,statusCheckRollup"]));o,n="KalebCole","partiful";q='query($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name){issue(number:$number){blockedBy(first:100){nodes{state number}}}}}'
 blockers=json.loads(run(["gh","api","graphql","-F",f"owner={o}","-F",f"name={n}","-F",f"number={issue}","-f",f"query={q}"]))["data"]["repository"]["issue"]["blockedBy"]["nodes"]
 comments=json.loads(run(["gh","api",f"repos/{o}/{n}/issues/{issue}/comments","--paginate"]));reviews=[x for x in comments if "## Implementation review" in x.get("body","")];body=reviews[-1].get("body","") if reviews else ""
 get=lambda pat:(re.search(pat,body,re.M).group(1) if re.search(pat,body,re.M) else None)
 cats={c:get(rf"^Category-{c}:\s*(PASS|FAIL)\s*$") for c in CATEGORIES}
 checks=[{"context":x.get("name") or x.get("context"),"state":x.get("conclusion") or x.get("status")} for x in view.get("statusCheckRollup",[])]
 return {"head":view.get("headRefOid"),"reviewed_sha":reviewed_sha,"reviewer_provenance":_native_reviewer_provenance(issue),"paths":[x["path"] for x in view.get("files",[])],"allowed_paths":contract["paths"],"excluded_paths":contract.get("excluded_paths",[]),"blockers":blockers,"checks":checks,"required_checks":contract.get("required_checks",[]),"no_required_ci":contract.get("no_required_ci"),"local_verification_ran":bool(contract.get("verification")),"latest_review":{"verdict":get(r"^Verdict:\s*(APPROVE|REQUEST_CHANGES)\s*$") or "MISSING","sha":get(r"^Commit:\s*([0-9a-f]{40})\s*$"),"categories":cats},"review_cycles":len(reviews),"evidence":{"red":get(r"^RED:\s*(.+)$"),"green":get(r"^GREEN:\s*(.+)$")}}
def main(argv:list[str]|None=None)->int:
 p=argparse.ArgumentParser();p.add_argument("--issue",type=int,required=True);p.add_argument("--pr",type=int,required=True);p.add_argument("--reviewed-sha",required=True);a=p.parse_args(argv)
 try:
  contract=load_issue_contract(a.issue); packet=_packet(a.issue,a.pr,a.reviewed_sha,_run,contract);result=validate_gate(packet)
  if not result["ok"]:print(json.dumps(result,sort_keys=True));return 1
  branch=_run(["git","branch","--show-current"]).strip()
  try:
   if checkout_verified_pr_head(a.pr)!=a.reviewed_sha:raise RuntimeError("detached head drift")
   for command in contract.get("verification",[]):_run(split_verification_command(command))
   # Re-read every mutable predicate immediately before merge.
   final=validate_gate(_packet(a.issue,a.pr,a.reviewed_sha,_run,contract))
   if not final["ok"]:print(json.dumps(final,sort_keys=True));return 1
   _run(["gh","pr","merge",str(a.pr),"--squash","--match-head-commit",a.reviewed_sha]); merged=json.loads(_run(["gh","pr","view",str(a.pr),"--json","state"]));closed=json.loads(_run(["gh","issue","view",str(a.issue),"--json","state"]));final["merged"]=merged.get("state")=="MERGED" and closed.get("state")=="CLOSED";print(json.dumps(final,sort_keys=True));return 0 if final["merged"] else 1
  finally:
   if branch:_run(["git","checkout",branch])
 except (RuntimeError,KeyError,json.JSONDecodeError) as e:print(f"FAIL: {e}");return 1
if __name__=="__main__":raise SystemExit(main())
