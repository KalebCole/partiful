#!/usr/bin/env python3
"""Pure, deterministic predicate gate for a reviewed Partiful PR."""
from __future__ import annotations
import argparse
import json
import subprocess
from pathlib import Path
from typing import Callable
try:
    from scripts.select_implementation_wave import _overlap
except ModuleNotFoundError:  # direct script invocation
    from select_implementation_wave import _overlap

ROOT = Path(__file__).resolve().parents[1]


def validate_gate(packet: dict) -> dict:
    failures = []
    def fail(code: str, detail: str) -> None: failures.append({"code": code, "detail": detail})
    if packet.get("head") != packet.get("reviewed_sha"): fail("sha_mismatch", "reviewed SHA is not current PR head")
    allowed = packet.get("allowed_paths", [])
    for path in packet.get("paths", []):
        if not any(_overlap(path, pattern) for pattern in allowed): fail("out_of_write_set", path)
    if any(item.get("state") == "OPEN" for item in packet.get("blockers", [])): fail("open_blocker", "issue has an open blocker")
    if any(item.get("state") not in {"SUCCESS", "NEUTRAL", "SKIPPED"} for item in packet.get("checks", [])): fail("required_check_not_success", "required check pending or failed")
    review = packet.get("latest_review") or {}
    if review.get("verdict") != "APPROVE" or review.get("sha") not in (None, packet.get("head")): fail("latest_review_not_approve", "latest structured review is not an approval of head")
    if packet.get("review_cycles", 0) > 3: fail("too_many_review_cycles", "more than three review cycles")
    return {"ok": not failures, "failures": failures}


def _run(command: list[str]) -> str:
    result = subprocess.run(command, cwd=ROOT, text=True, capture_output=True)
    if result.returncode: raise RuntimeError(result.stderr.strip() or result.stdout.strip())
    return result.stdout


def main(run: Callable[[list[str]], str] = _run) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--issue", required=True, type=int); parser.add_argument("--pr", required=True, type=int); parser.add_argument("--reviewed-sha", required=True)
    args = parser.parse_args()
    # Live gathering is deliberately CLI-only; validation above is pure and injectable.
    pr = json.loads(run(["gh", "pr", "view", str(args.pr), "--json", "headRefOid,files,statusCheckRollup"]))
    owner, name = "KalebCole", "partiful"
    query = '''query($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name){issue(number:$number){blockedBy(first:100){nodes{state number}}}}}'''
    blockers = json.loads(run(["gh", "api", "graphql", "-F", f"owner={owner}", "-F", f"name={name}", "-F", f"number={args.issue}", "-f", f"query={query}"]))["data"]["repository"]["issue"]["blockedBy"]["nodes"]
    comments = json.loads(run(["gh", "api", f"repos/{owner}/{name}/issues/{args.issue}/comments", "--paginate"]))
    reviews = [comment for comment in comments if "## Implementation review" in comment.get("body", "")]
    latest = reviews[-1] if reviews else {"body": ""}
    import re
    verdict = re.search(r"^Verdict:\s*(APPROVE|REQUEST_CHANGES)\s*$", latest.get("body", ""), re.M)
    reviewed = re.search(r"^Commit:\s*([0-9a-f]{40})\s*$", latest.get("body", ""), re.M)
    allowed = json.loads((ROOT / "config/implementation-write-sets.json").read_text())[str(args.issue)]
    packet = {"head": pr["headRefOid"], "reviewed_sha": args.reviewed_sha, "paths": [item["path"] for item in pr.get("files", [])], "allowed_paths": allowed, "blockers": blockers, "checks": [{"state": item.get("conclusion") or item.get("status")} for item in pr.get("statusCheckRollup", [])], "latest_review": {"verdict": verdict.group(1) if verdict else "MISSING", "sha": reviewed.group(1) if reviewed else None}, "review_cycles": len(reviews)}
    result = validate_gate(packet)
    if not result["ok"]: print(json.dumps(result, sort_keys=True)); return 1
    run(["python3", "scripts/checkout_verified_pr_head.py", str(args.pr)])
    # Focused/full mechanical verification runs detached at GitHub's exact reviewed head.
    run(["go", "test", "./..."])
    run(["python3", "scripts/verify_go_package_graph.py"])
    run(["python3", "scripts/verify_command_model.py"])
    run(["gh", "pr", "merge", str(args.pr), "--squash", "--delete-branch"])
    state = json.loads(run(["gh", "pr", "view", str(args.pr), "--json", "state"]))
    issue_state = json.loads(run(["gh", "issue", "view", str(args.issue), "--json", "state"]))
    result["merged"] = state.get("state") == "MERGED" and issue_state.get("state") == "CLOSED"
    print(json.dumps(result, sort_keys=True)); return 0 if result["merged"] else 1

if __name__ == "__main__": raise SystemExit(main())
