#!/usr/bin/env python3
"""Fetch a GitHub PR and detach the current worktree at its exact head."""

from __future__ import annotations

import argparse
import json
import subprocess
from pathlib import Path
from typing import Callable

ROOT = Path(__file__).resolve().parents[1]


def _run(command: list[str]) -> str:
    result = subprocess.run(command, cwd=ROOT, text=True, capture_output=True)
    if result.returncode:
        detail = result.stderr.strip() or result.stdout.strip()
        raise RuntimeError(f"command failed ({result.returncode}): {' '.join(command)}: {detail}")
    return result.stdout


def checkout_verified_pr_head(
    pr_number: int,
    *,
    run: Callable[[list[str]], str] = _run,
) -> str:
    if pr_number <= 0:
        raise ValueError("PR number must be positive")

    payload = json.loads(
        run(["gh", "pr", "view", str(pr_number), "--json", "headRefOid"])
    )
    declared = str(payload.get("headRefOid") or "").strip()
    if len(declared) != 40:
        raise RuntimeError(f"GitHub returned an invalid PR head SHA: {declared!r}")

    run(["git", "fetch", "--force", "origin", f"pull/{pr_number}/head"])
    fetched = run(["git", "rev-parse", "FETCH_HEAD"]).strip()
    if fetched != declared:
        raise RuntimeError(
            f"fetched PR head mismatch: GitHub declared {declared}, fetch returned {fetched}"
        )

    run(["git", "checkout", "--detach", declared])
    actual = run(["git", "rev-parse", "HEAD"]).strip()
    if actual != declared:
        raise RuntimeError(
            f"detached checkout mismatch: expected {declared}, worktree has {actual}"
        )
    return declared


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Detach this worktree at a verified GitHub PR head commit"
    )
    parser.add_argument("pr_number", type=int)
    args = parser.parse_args()
    print(checkout_verified_pr_head(args.pr_number))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
