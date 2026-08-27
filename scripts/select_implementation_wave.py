#!/usr/bin/env python3
"""Select a deterministic, write-set-disjoint implementation wave."""
from __future__ import annotations
import argparse
import json
from typing import Iterable


def _overlap(left: str, right: str) -> str | None:
    left, right = left.rstrip("/"), right.rstrip("/")
    if left.endswith("/**"):
        prefix = left[:-3]
        if right == prefix or right.startswith(prefix + "/"):
            return right
    if right.endswith("/**"):
        prefix = right[:-3]
        if left == prefix or left.startswith(prefix + "/"):
            return left
    return left if left == right else None


def select_wave(candidates: Iterable[dict], active_cards: Iterable[dict] = ()) -> dict:
    selected, held = [], []
    active_cards = list(active_cards)
    occupied = [(item.get("number", item.get("issue")), path) for item in active_cards for path in item.get("paths", [])]
    for candidate in sorted(candidates, key=lambda item: item["number"]):
        conflict = next(((number, _overlap(path, used)) for number, used in occupied for path in candidate.get("paths", []) if _overlap(path, used)), None)
        if conflict:
            number, path = conflict
            source = "active card" if any(item.get("number", item.get("issue")) == number for item in active_cards) else "selected issue"
            held.append({"number": candidate["number"], "reason": f"{source} #{number} overlaps {path}"})
        else:
            selected.append(candidate)
            occupied.extend((candidate["number"], path) for path in candidate.get("paths", []))
    return {"selected": selected, "held": held}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("candidates", help="JSON candidate array")
    parser.add_argument("--active", default="[]", help="JSON active-card array")
    args = parser.parse_args()
    print(json.dumps(select_wave(json.loads(args.candidates), json.loads(args.active)), sort_keys=True))
    return 0

if __name__ == "__main__":
    raise SystemExit(main())
