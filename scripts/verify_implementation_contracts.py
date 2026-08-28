#!/usr/bin/env python3
"""Reject implementation verification targets owned by later slices."""

from __future__ import annotations

import argparse
import json
import shlex
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DEFAULT_CONTRACT = ROOT / "config" / "implementation-write-sets.json"


def _scope_root(scope: str) -> str:
    """Return the literal path prefix before a glob metacharacter."""
    parts: list[str] = []
    for part in scope.strip("./").split("/"):
        if any(char in part for char in "*?["):
            break
        parts.append(part)
    return "/".join(parts)


def _target_owners(target: str, contracts: dict[str, dict]) -> list[int]:
    normalized = target.removeprefix("./").rstrip("/")
    owners: list[int] = []
    for issue, contract in contracts.items():
        for scope in contract.get("paths", []):
            root = _scope_root(scope)
            if root and (normalized == root or normalized.startswith(f"{root}/")):
                owners.append(int(issue))
                break
    return sorted(owners)


def verify(contracts: dict[str, dict]) -> list[str]:
    errors: list[str] = []
    for issue_text, contract in contracts.items():
        issue = int(issue_text)
        for command in contract.get("verification", []):
            for token in shlex.split(command):
                if not token.startswith("./") or token in {"./", "./..."}:
                    continue
                owners = _target_owners(token, contracts)
                if owners and issue not in owners and min(owners) > issue:
                    errors.append(
                        f"issue {issue} verification target {token} is owned by later issue {min(owners)}"
                    )
    return errors


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("contract", nargs="?", type=Path, default=DEFAULT_CONTRACT)
    parser.add_argument("--quiet", action="store_true")
    args = parser.parse_args(argv)
    contracts = json.loads(args.contract.read_text())
    errors = verify(contracts)
    if errors:
        for error in errors:
            print(error, file=sys.stderr)
        return 1
    if not args.quiet:
        print(f"verified {len(contracts)} implementation contracts")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
