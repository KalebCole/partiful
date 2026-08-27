#!/usr/bin/env python3
"""Adopt existing PR #49 as one native issue #34 review card."""
from __future__ import annotations

import argparse
import json
import re
import subprocess
from pathlib import Path
from typing import Callable

ROOT = Path(__file__).resolve().parents[1]
PR = 49
PR_BRANCH = "partiful/issue-34-initial-implement"
KEY = "partiful:implementation:34"
Run = Callable[[list[str]], str]


def _run(command: list[str]) -> str:
    result = subprocess.run(command, cwd=ROOT, text=True, capture_output=True)
    if result.returncode:
        raise RuntimeError(result.stderr.strip() or result.stdout.strip())
    return result.stdout


def inspect_pr(run: Run) -> tuple[str, str]:
    value = json.loads(run(["gh", "pr", "view", str(PR), "--json", "headRefOid,headRefName"]))
    sha = str(value.get("headRefOid", ""))
    branch = str(value.get("headRefName", ""))
    if not re.fullmatch(r"[0-9a-f]{40}", sha):
        raise RuntimeError("PR #49 returned no valid exact head SHA")
    if branch != PR_BRANCH:
        raise RuntimeError(f"PR #49 branch drifted: {branch!r}")
    return sha, branch


def adopt(run: Run | None = None, pr: tuple[str, str] | None = None) -> str:
    run = run or _run
    sha, branch = pr or inspect_pr(run)
    contract = json.loads((ROOT / "config" / "implementation-write-sets.json").read_text())
    allowed_paths = tuple(contract["34"]["paths"])
    body = (
        f"Adopt existing PR #49 directly in review phase. PR branch: {branch}. "
        f"Exact SHA: {sha}. Do not rerun implementation or create children. "
        "Review using docs/agentic-engineering.md and native same-card lifecycle. "
        "If review requests changes, check out and push this existing PR branch from "
        "the same card workspace; do not open a replacement PR."
        "\n\nAllowed files: " + ", ".join(f"`{path}`" for path in allowed_paths) + "."
    )
    command = [
        "hermes", "kanban", "--board", "partiful", "create",
        "implement: #34 application kernel",
        "--assignee", "partiful-implementer",
        "--workspace", f"worktree:{ROOT}",
        "--branch", branch,
        "--tenant", "partiful-wayfinder",
        "--idempotency-key", KEY,
        "--initial-status", "running",
        "--body", body,
        "--json",
    ]
    payload = json.loads(run(command))
    card = payload.get("task_id") or payload.get("id")
    if not card:
        raise RuntimeError("Kanban create returned no task id")
    card = str(card)
    state_value = json.loads(run(["hermes", "kanban", "--board", "partiful", "show", card, "--json"]))
    status = str(state_value.get("task", state_value).get("status", ""))
    if status == "review":
        return card
    if status != "running":
        raise RuntimeError(f"adopted card {card} has unexpected status {status!r}")
    run([
        "hermes", "kanban", "--board", "partiful", "request-review", card,
        "--reviewer", "partiful-code-reviewer",
        "--summary", f"Adopt PR #49 at exact SHA {sha}",
        "--metadata", json.dumps({"issue": 34, "pr": PR, "sha": sha}, sort_keys=True),
    ])
    return card


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--apply", action="store_true")
    parser.add_argument("--quiet", action="store_true")
    args = parser.parse_args(argv)
    try:
        pr = inspect_pr(_run)
        if not args.apply:
            if not args.quiet:
                print(json.dumps({"issue": 34, "pr": PR, "sha": pr[0], "branch": pr[1], "action": "dry-run"}, sort_keys=True))
            return 0
        card = adopt(_run, pr)
        if not args.quiet:
            print(json.dumps({"card": card, "issue": 34, "pr": PR, "sha": pr[0], "branch": pr[1]}, sort_keys=True))
        return 0
    except (RuntimeError, KeyError, json.JSONDecodeError) as error:
        print(f"FAIL: {error}")
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
