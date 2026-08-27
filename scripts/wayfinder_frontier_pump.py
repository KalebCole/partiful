#!/usr/bin/env python3
"""Queue autonomous Wayfinder decision lanes from the GitHub map frontier."""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Callable, Iterable

ROOT = Path(__file__).resolve().parents[1]
REPOSITORY = "KalebCole/partiful"
MAP_ISSUE = 8
BOARD = "partiful"
WORKSPACE = f"dir:{ROOT}"
DECISION_LABEL = "wayfinder:decision"

GRAPHQL = """
query($owner: String!, $name: String!, $number: Int!, $after: String) {
  repository(owner: $owner, name: $name) {
    issue(number: $number) {
      subIssues(first: 100, after: $after) {
        nodes {
          number title state url
          labels(first: 20) { nodes { name } }
          assignees(first: 10) { nodes { login } }
          blockedBy(first: 20) { nodes { number state } }
        }
        pageInfo { hasNextPage endCursor }
      }
    }
  }
}
"""


@dataclass(frozen=True)
class Issue:
    number: int
    title: str
    url: str
    state: str
    labels: tuple[str, ...]
    assignees: tuple[str, ...]
    blocked_by: tuple[tuple[int, str], ...]


def parse_issues(payload: dict) -> list[Issue]:
    nodes = payload["data"]["repository"]["issue"]["subIssues"]["nodes"]
    return [
        Issue(
            number=node["number"],
            title=node["title"],
            url=node["url"],
            state=node["state"],
            labels=tuple(label["name"] for label in node["labels"]["nodes"]),
            assignees=tuple(person["login"] for person in node["assignees"]["nodes"]),
            blocked_by=tuple(
                (blocker["number"], blocker["state"])
                for blocker in node["blockedBy"]["nodes"]
            ),
        )
        for node in nodes
    ]


def select_frontier(issues: Iterable[Issue]) -> list[Issue]:
    return sorted(
        (
            issue
            for issue in issues
            if issue.state == "OPEN"
            and DECISION_LABEL in issue.labels
            and not issue.assignees
            and not any(state == "OPEN" for _, state in issue.blocked_by)
        ),
        key=lambda issue: issue.number,
    )


def build_resolver_body(issue: Issue) -> str:
    return f"""Resolve GitHub decision ticket #{issue.number}: {issue.url}

Role: autonomous Wayfinder decision resolver. Work in a fresh context.

1. Read the issue, `docs/wayfinder-autonomy.md`, and only the authoritative repository sources needed by the question.
2. Re-read the issue immediately before claiming it. Stop if it is closed, blocked, or claimed by someone else. Otherwise assign it to `@me`.
3. Do not modify repository files. Do not implement code. Do not close the issue or edit map issue #8.
4. Post exactly one structured `## Candidate resolution` comment using the contract in `docs/wayfinder-autonomy.md`. Include exact evidence links or paths, rejected alternatives, consequences, evidence gates, and map impact.
5. Read the posted comment back and confirm that GitHub preserved the full content.
6. Complete this Kanban card with the candidate comment URL and sources read in metadata.

Use the narrowest reversible design that preserves settled contracts, fails closed, minimizes privacy exposure, and does not claim unobserved transport behavior. Missing transport evidence becomes a stable evidence gate, not an invention."""


def build_reviewer_body(issue: Issue) -> str:
    return f"""Independently rubber-duck GitHub decision ticket #{issue.number}: {issue.url}

Role: read-only independent critic. Start from the ticket and its latest `## Candidate resolution` comment. Do not use or request the resolver transcript.

1. Load and follow the `rubber-duck` skill and `docs/wayfinder-autonomy.md`.
2. Inspect the actual authoritative repository sources cited by the candidate.
3. Do not modify repository files. Do not modify the candidate. Do not close the issue or edit map issue #8.
4. Post exactly one `## Independent review` comment with one verdict: `APPROVE`, `REVISE`, `EVIDENCE_REQUIRED`, or `OWNER_GATE`.
5. Give at most three substantive findings. For each finding state issue, impact, correction, and verification evidence. Ignore style and cosmetic issues.
6. Read the review comment back. Complete this card with the verdict, review comment URL, and review comment database ID in metadata.

`APPROVE` means the candidate is internally coherent, evidence-backed, and safe for later tickets to rely on. Do not approve a plausible answer that claims unobserved transport behavior."""


def build_reconcile_body(issue: Issue) -> str:
    partition_materialization = ""
    if issue.number == 19:
        partition_materialization = """

For approved or two-`REVISE`-adjudicated ticket #19 only, materialize the reviewed partition before closing it:

1. Create one GitHub child issue of map #8 for every reviewed implementation slice. Preserve each reviewed title and complete scope; apply the `partiful:implementation` label; leave each issue unassigned.
2. Add every reviewed prerequisite as a GitHub native blocker. Never replace native relationships with prose ordering.
3. Read map #8 and verify every created issue is attached exactly once, has the label and full body, and has the reviewed native blocker set.
4. Only after that read-back succeeds, post the #19 resolution, close #19, and allow the implementation frontier pump to import the new issues into the Partiful board.
"""
    return f"""Reconcile the autonomous decision cycle for GitHub ticket #{issue.number}: {issue.url}

Role: Wayfinder cartographer. Read `docs/wayfinder-autonomy.md`, the latest candidate, and the latest independent review. Do not modify repository files.

Before any write, re-read the issue state, current assignees, recent comments, map issue #8, and native blockers.

- `APPROVE`: post a compact final `## Resolution` that links the candidate and review; close the ticket as completed; add or update its one-line decision gist on map #8; apply only the reviewed map-impact changes.
- `REVISE`: count prior `## Independent review` comments whose verdict is `REVISE`. If fewer than two exist, do not finalize: use `scripts/wayfinder_frontier_pump.py --issue {issue.number} --attempt <review-comment-database-id>` to create a fresh resolver -> reviewer -> reconciler chain, then complete this card with the new card IDs. If two or more exist, do not create another chain. Adjudicate in this fresh cartographer context: compare the candidates and reviews against authoritative sources and the precedence rules in `docs/wayfinder-autonomy.md`. Create the smallest evidence ticket if proof is missing; otherwise post an `## Adjudicated resolution` that links both review cycles, close the ticket, and update map #8. Do not convert ordinary reviewer disagreement into an owner question.
- `EVIDENCE_REQUIRED`: create the smallest `wayfinder:task` evidence ticket, attach it to map #8, add it as a native blocker of #{issue.number}, unassign #{issue.number}, and complete this card with the new issue URL.
- `OWNER_GATE`: relabel #{issue.number} from `wayfinder:decision` to `wayfinder:grilling`, unassign it, and block this card with one concrete scenario-based question for the owner.{partition_materialization}

After every GitHub write, read after write and verify the exact target. Before closing normally, confirm that the latest independent-review verdict is `APPROVE` and that it reviews the latest candidate. The only exception is the explicit two-`REVISE` adjudication path above."""


def _run(command: list[str]) -> str:
    result = subprocess.run(command, cwd=ROOT, text=True, capture_output=True)
    if result.returncode:
        detail = result.stderr.strip() or result.stdout.strip()
        raise RuntimeError(f"command failed ({result.returncode}): {' '.join(command[:4])}: {detail}")
    return result.stdout


def fetch_issues(run: Callable[[list[str]], str] = _run) -> list[Issue]:
    owner, name = REPOSITORY.split("/", 1)
    issues: list[Issue] = []
    after: str | None = None
    while True:
        command = [
            "/opt/homebrew/bin/gh",
            "api",
            "graphql",
            "-F",
            f"owner={owner}",
            "-F",
            f"name={name}",
            "-F",
            f"number={MAP_ISSUE}",
            "-f",
            f"query={GRAPHQL}",
        ]
        if after is not None:
            command.extend(["-F", f"after={after}"])
        payload = json.loads(run(command))
        issues.extend(parse_issues(payload))
        page_info = payload["data"]["repository"]["issue"]["subIssues"]["pageInfo"]
        if not page_info["hasNextPage"]:
            return issues
        after = page_info["endCursor"]
        if not after:
            raise RuntimeError("GitHub reported another sub-issue page without an end cursor")


def _task_id(output: str) -> str:
    payload = json.loads(output)
    task_id = payload.get("task_id") or payload.get("id")
    if not task_id:
        raise RuntimeError(f"Kanban create returned no task id: {payload!r}")
    return str(task_id)


def _key(issue: Issue, stage: str, attempt: str | None) -> str:
    base = f"partiful:github:{issue.number}"
    return f"{base}:{attempt}:{stage}" if attempt else f"{base}:{stage}"


def _create_command(
    issue: Issue,
    stage: str,
    title: str,
    body: str,
    assignee: str,
    attempt: str | None,
    parent: str | None = None,
) -> list[str]:
    command = [
        "hermes",
        "kanban",
        "--board",
        BOARD,
        "create",
        title,
        "--body",
        body,
        "--assignee",
        assignee,
        "--workspace",
        WORKSPACE,
        "--tenant",
        "partiful-wayfinder",
        "--priority",
        "100",
        "--idempotency-key",
        _key(issue, stage, attempt),
        "--max-runtime",
        "45m",
        "--max-retries",
        "3",
        "--goal",
        "--goal-max-turns",
        "6",
        "--skill",
        "wayfinder",
        "--skill",
        "github-issues",
        "--json",
    ]
    if stage == "review":
        command.extend(["--skill", "rubber-duck"])
    else:
        command.extend(["--skill", "domain-modeling"])
    if parent:
        command.extend(["--parent", parent])
    return command


def create_cards(
    issue: Issue,
    attempt: str | None = None,
    run: Callable[[list[str]], str] = _run,
) -> tuple[str, str, str]:
    resolver_id = _task_id(
        run(
            _create_command(
                issue,
                "resolve",
                f"resolve: #{issue.number} {issue.title}",
                build_resolver_body(issue),
                "wayfinder-resolver",
                attempt,
            )
        )
    )
    reviewer_id = _task_id(
        run(
            _create_command(
                issue,
                "review",
                f"review: #{issue.number} {issue.title}",
                build_reviewer_body(issue),
                "wayfinder-reviewer",
                attempt,
                parent=resolver_id,
            )
        )
    )
    reconcile_id = _task_id(
        run(
            _create_command(
                issue,
                "reconcile",
                f"reconcile: #{issue.number} {issue.title}",
                build_reconcile_body(issue),
                "wayfinder-cartographer",
                attempt,
                parent=reviewer_id,
            )
        )
    )
    return resolver_id, reviewer_id, reconcile_id


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--issue", type=int, help="queue one open decision even when assigned")
    parser.add_argument("--attempt", help="stable suffix for a fresh revision chain")
    parser.add_argument("--dry-run", action="store_true", help="show the selected frontier only")
    parser.add_argument("--quiet", action="store_true", help="print nothing on success")
    args = parser.parse_args()

    try:
        issues = fetch_issues()
        if args.issue is not None:
            selected = [issue for issue in issues if issue.number == args.issue]
            if not selected:
                raise RuntimeError(f"issue #{args.issue} is not a sub-issue of map #{MAP_ISSUE}")
            issue = selected[0]
            if issue.state != "OPEN" or DECISION_LABEL not in issue.labels:
                raise RuntimeError(f"issue #{issue.number} is not an open {DECISION_LABEL} ticket")
            if any(state == "OPEN" for _, state in issue.blocked_by):
                raise RuntimeError(f"issue #{issue.number} still has an open blocker")
            frontier = selected
        else:
            frontier = select_frontier(issues)

        if args.dry_run:
            if not args.quiet:
                print(json.dumps([issue.__dict__ for issue in frontier], indent=2))
            return 0

        created = [
            {"issue": issue.number, "cards": create_cards(issue, attempt=args.attempt)}
            for issue in frontier
        ]
        if not args.quiet and created:
            print(json.dumps(created, indent=2))
        return 0
    except (KeyError, TypeError, ValueError, json.JSONDecodeError, RuntimeError) as error:
        print(f"FAIL: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
