#!/usr/bin/env python3
"""Queue implementation, independent review, and integration cards from map #8."""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Callable, Iterable

ROOT = Path(__file__).resolve().parents[1]
REPOSITORY = "KalebCole/partiful"
MAP_ISSUE = 8
BOARD = "partiful"
WORKSPACE = f"worktree:{ROOT}"
IMPLEMENTATION_LABEL = "partiful:implementation"

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
            and IMPLEMENTATION_LABEL in issue.labels
            and not issue.assignees
            and not any(state == "OPEN" for _, state in issue.blocked_by)
        ),
        key=lambda issue: issue.number,
    )


def build_implement_body(issue: Issue, attempt: str | None = None) -> str:
    revision = ""
    if attempt:
        revision = f"""
This is revision attempt `{attempt}`. Find the existing open PR linked from the issue. Fetch its exact head commit into this isolated worktree, apply only the independently requested corrections, and push the new commit to that same PR head branch. Do not open a duplicate PR.
"""
    return f"""Implement GitHub issue #{issue.number}: {issue.url}

Role: autonomous Partiful implementer. Work only in the isolated worktree created for this card.{revision}

1. Read the issue, every authoritative decision it links, `docs/wayfinder-autonomy.md`, and current repository state. Re-read native blockers immediately before work. Stop if the issue is closed or blocked.
2. For the initial attempt, re-read the issue before claiming it; assign it to `@me` only if it is still unassigned. For a revision, preserve the existing claim.
3. Follow strict TDD: write one failing test, run it and confirm the expected failure, write the minimum implementation, then run the focused and full mechanical checks named by the issue.
4. Change only the files allowed by the issue. Keep open evidence gates fail-closed. Do not invent product behavior, transport semantics, response shapes, permission rules, retry behavior, or cleanup behavior.
5. Inspect the exact diff, commit only allowed paths, push the branch, and open or update one PR whose body contains `Closes #{issue.number}`, authoritative decision links, evidence-gate status, and exact verification output.
6. Post one `## Implementation handoff` comment on the issue with the PR URL, reviewed commit SHA, changed paths, RED and GREEN test evidence, full verification commands, and any still-open gate.
7. Read the PR and issue comment back. Complete this card with the PR URL, PR number, commit SHA, and handoff comment URL in metadata.

Do not merge the PR, close the issue, weaken tests, or make live Partiful mutations."""


def build_review_body(issue: Issue) -> str:
    return f"""Independently rubber-duck the implementation for GitHub issue #{issue.number}: {issue.url}

Role: read-only Partiful code reviewer in a fresh context. Do not use the implementer transcript.

1. Load the `rubber-duck`, `adversarial-code-review`, and `github-code-review` skills. Read the issue, its authoritative decision links, the latest `## Implementation handoff`, and the linked PR.
2. Confirm the PR head SHA, inspect the complete diff against its base, and check that changed paths stay within issue scope.
3. Inspect the production code and tests against the issue acceptance criteria, settled decisions, safety boundaries, evidence gates, and cleanup limits. Run the issue's mechanical checks against the PR head in this isolated worktree when possible.
4. Do not modify repository files, push commits, merge, close the issue, or invent missing product or transport behavior.
5. Post exactly one issue comment:

## Implementation review
PR: <PR URL>
Commit: <exact reviewed PR head SHA>
Verdict: APPROVE | REQUEST_CHANGES | EVIDENCE_REQUIRED

### Findings
1. Issue: <substantive defect or `none`>
   Impact: <why acceptance depends on it>
   Correction: <bounded correction>
   Verification: <proof required>

Maximum three substantive findings. Ignore style, naming, grammar, and cosmetic refactors. `APPROVE` is valid only when the exact commit is safe to merge. Read the comment back and complete this card with its URL, database ID, verdict, PR number, and commit SHA."""


def build_integrate_body(issue: Issue) -> str:
    return f"""Reconcile and integrate GitHub implementation issue #{issue.number}: {issue.url}

Role: Partiful integrator. Read `docs/wayfinder-autonomy.md`, the latest implementation handoff, the latest independent implementation review, the linked PR, and native blockers.

Before any write, re-read the issue, PR state, PR head SHA, recent comments, required checks, and map issue #8.

- `APPROVE`: confirm the review names the latest PR head commit exactly. Confirm every required check is successful and rerun the issue's mechanical verification against that commit when no required CI check covers it. Confirm the diff stays within scope. Merge the PR with squash, verify the PR is merged, verify the issue is closed, and read after write on both targets. If no open child issue remains under map #8, post a compact completion comment and close the map after reading it back.
- `REQUEST_CHANGES`: do not merge. If fewer than three implementation reviews exist, run `python3 scripts/implementation_frontier_pump.py --issue {issue.number} --attempt <review-comment-id>` to create a fresh implement -> review -> integrate chain, then complete this card with the new card IDs. On the third failed review, block this card with the exact unresolved defects rather than looping forever.
- `EVIDENCE_REQUIRED`: do not merge. Create the smallest `wayfinder:task` evidence issue, attach it to map #8, add it as a native blocker of #{issue.number}, and leave the implementation issue and PR open.

Reject stale approvals, failing or pending required checks, open blockers, out-of-scope changes, and unverified live-mutation behavior. Record exact PR, issue, check, and map states in the card metadata."""


def _run(command: list[str]) -> str:
    result = subprocess.run(command, cwd=ROOT, text=True, capture_output=True)
    if result.returncode:
        detail = result.stderr.strip() or result.stdout.strip()
        raise RuntimeError(
            f"command failed ({result.returncode}): {' '.join(command[:4])}: {detail}"
        )
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
    base = f"partiful:implementation:{issue.number}"
    return f"{base}:{attempt}:{stage}" if attempt else f"{base}:{stage}"


def _branch(issue: Issue, stage: str, attempt: str | None) -> str:
    suffix = re.sub(r"[^A-Za-z0-9._-]+", "-", attempt or "initial")[:40]
    return f"partiful/issue-{issue.number}-{suffix}-{stage}"


def _create_command(
    issue: Issue,
    stage: str,
    title: str,
    body: str,
    assignee: str,
    attempt: str | None,
    parent: str | None = None,
) -> list[str]:
    is_implementation = stage == "implement"
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
        "--branch",
        _branch(issue, stage, attempt),
        "--tenant",
        "partiful-wayfinder",
        "--priority",
        "80",
        "--idempotency-key",
        _key(issue, stage, attempt),
        "--max-runtime",
        "2h" if is_implementation else "60m",
        "--max-retries",
        "3",
        "--goal",
        "--goal-max-turns",
        "30" if is_implementation else "12",
        "--json",
    ]
    skills = {
        "implement": [
            "test-driven-development",
            "collaborative-repository-hygiene",
            "verification-before-completion",
            "github-pr-workflow",
        ],
        "review": ["rubber-duck", "adversarial-code-review", "github-code-review"],
        "integrate": [
            "github-pr-workflow",
            "verification-before-completion",
            "collaborative-repository-hygiene",
        ],
    }[stage]
    for skill in skills:
        command.extend(["--skill", skill])
    if parent:
        command.extend(["--parent", parent])
    return command


def create_cards(
    issue: Issue,
    attempt: str | None = None,
    run: Callable[[list[str]], str] = _run,
) -> tuple[str, str, str]:
    implement_id = _task_id(
        run(
            _create_command(
                issue,
                "implement",
                f"implement: #{issue.number} {issue.title}",
                build_implement_body(issue, attempt),
                "partiful-implementer",
                attempt,
            )
        )
    )
    review_id = _task_id(
        run(
            _create_command(
                issue,
                "review",
                f"code review: #{issue.number} {issue.title}",
                build_review_body(issue),
                "partiful-code-reviewer",
                attempt,
                parent=implement_id,
            )
        )
    )
    integrate_id = _task_id(
        run(
            _create_command(
                issue,
                "integrate",
                f"integrate: #{issue.number} {issue.title}",
                build_integrate_body(issue),
                "partiful-integrator",
                attempt,
                parent=review_id,
            )
        )
    )
    return implement_id, review_id, integrate_id


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--issue", type=int, help="queue one open implementation issue")
    parser.add_argument("--attempt", help="stable review-comment id for a revision chain")
    parser.add_argument("--dry-run", action="store_true", help="show selected frontier only")
    parser.add_argument("--quiet", action="store_true", help="print nothing on success")
    args = parser.parse_args()

    try:
        issues = fetch_issues()
        if args.issue is not None:
            selected = [issue for issue in issues if issue.number == args.issue]
            if not selected:
                raise RuntimeError(
                    f"issue #{args.issue} is not a sub-issue of map #{MAP_ISSUE}"
                )
            issue = selected[0]
            if issue.state != "OPEN" or IMPLEMENTATION_LABEL not in issue.labels:
                raise RuntimeError(
                    f"issue #{issue.number} is not an open {IMPLEMENTATION_LABEL} issue"
                )
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
