from __future__ import annotations

import json
import unittest

from scripts.implementation_frontier_pump import (
    IMPLEMENTATION_LABEL,
    Issue,
    build_implement_body,
    build_integrate_body,
    build_review_body,
    create_cards,
    fetch_issues,
    select_frontier,
)


class SelectImplementationFrontierTests(unittest.TestCase):
    def test_selects_only_open_unblocked_unassigned_implementation_issues(self) -> None:
        issues = [
            Issue(20, "domain types", "https://example/20", "OPEN", (IMPLEMENTATION_LABEL,), (), ()),
            Issue(21, "application", "https://example/21", "OPEN", (IMPLEMENTATION_LABEL,), (), ((20, "OPEN"),)),
            Issue(22, "transport", "https://example/22", "OPEN", (IMPLEMENTATION_LABEL,), ("KalebCole",), ()),
            Issue(23, "release", "https://example/23", "CLOSED", (IMPLEMENTATION_LABEL,), (), ()),
            Issue(24, "decision", "https://example/24", "OPEN", ("wayfinder:decision",), (), ()),
            Issue(25, "cli", "https://example/25", "OPEN", (IMPLEMENTATION_LABEL,), (), ((19, "CLOSED"),)),
        ]

        self.assertEqual([20, 25], [issue.number for issue in select_frontier(issues)])


class ImplementationBodyContractTests(unittest.TestCase):
    def setUp(self) -> None:
        self.issue = Issue(
            20,
            "Implement domain types",
            "https://github.com/KalebCole/partiful/issues/20",
            "OPEN",
            (IMPLEMENTATION_LABEL,),
            (),
            (),
        )

    def test_implementer_uses_tdd_worktree_and_opens_pr_without_merging(self) -> None:
        body = build_implement_body(self.issue)

        self.assertIn(self.issue.url, body)
        self.assertIn("strict TDD", body)
        self.assertIn("isolated worktree", body)
        self.assertIn("Closes #20", body)
        self.assertIn("Do not merge", body)
        self.assertIn("Implementation handoff", body)
        self.assertIn("dedicated gated wrapper", body)
        self.assertIn("do not seek, import, recover, or create", body.lower())

    def test_reviewer_is_independent_and_posts_structured_verdict(self) -> None:
        body = build_review_body(self.issue)

        self.assertIn(self.issue.url, body)
        self.assertIn("rubber-duck", body)
        self.assertIn("APPROVE", body)
        self.assertIn("REQUEST_CHANGES", body)
        self.assertIn("Do not edit repository file contents", body)
        self.assertIn("checkout_verified_pr_head.py", body)
        self.assertIn("git rev-parse HEAD", body)

    def test_integrator_merges_only_after_approved_latest_commit(self) -> None:
        body = build_integrate_body(self.issue)

        self.assertIn(self.issue.url, body)
        self.assertIn("latest PR head commit", body)
        self.assertIn("required checks", body)
        self.assertIn("read after write", body.lower())
        self.assertIn("implementation_frontier_pump.py --issue 20", body)
        self.assertIn("checkout_verified_pr_head.py", body)
        self.assertIn("git rev-parse HEAD", body)


class FetchImplementationIssuesTests(unittest.TestCase):
    def test_fetches_every_subissue_page(self) -> None:
        calls: list[list[str]] = []

        def node(number: int) -> dict:
            return {
                "number": number,
                "title": f"issue {number}",
                "state": "OPEN",
                "url": f"https://example/{number}",
                "labels": {"nodes": [{"name": IMPLEMENTATION_LABEL}]},
                "assignees": {"nodes": []},
                "blockedBy": {"nodes": []},
            }

        def fake_run(command: list[str]) -> str:
            calls.append(command)
            second_page = any(value == "after=cursor-100" for value in command)
            nodes = [node(101)] if second_page else [node(i) for i in range(1, 101)]
            return json.dumps(
                {
                    "data": {
                        "repository": {
                            "issue": {
                                "subIssues": {
                                    "nodes": nodes,
                                    "pageInfo": {
                                        "hasNextPage": not second_page,
                                        "endCursor": None if second_page else "cursor-100",
                                    },
                                }
                            }
                        }
                    }
                }
            )

        issues = fetch_issues(run=fake_run)

        self.assertEqual(101, len(issues))
        self.assertEqual(2, len(calls))
        self.assertIn("after=cursor-100", calls[1])


class ImplementationCardCreationTests(unittest.TestCase):
    def test_creates_worktree_implement_review_integrate_chain(self) -> None:
        issue = Issue(20, "domain types", "https://example/20", "OPEN", (IMPLEMENTATION_LABEL,), (), ())
        calls: list[list[str]] = []

        def fake_run(command: list[str]) -> str:
            calls.append(command)
            key = next(value for value in command if value.startswith("partiful:implementation:20:"))
            task_id = {
                "partiful:implementation:20:implement": "implement-20",
                "partiful:implementation:20:review": "review-20",
                "partiful:implementation:20:integrate": "integrate-20",
            }[key]
            return json.dumps({"task_id": task_id})

        created = create_cards(issue, run=fake_run)

        self.assertEqual(("implement-20", "review-20", "integrate-20"), created)
        self.assertIn("worktree:", calls[0][calls[0].index("--workspace") + 1])
        self.assertEqual("partiful-implementer", calls[0][calls[0].index("--assignee") + 1])
        self.assertIn("--branch", calls[0])
        self.assertIn("partiful:implementation:20:implement", calls[0])
        self.assertEqual("implement-20", calls[1][calls[1].index("--parent") + 1])
        self.assertEqual("partiful-code-reviewer", calls[1][calls[1].index("--assignee") + 1])
        self.assertIn("partiful:implementation:20:review", calls[1])
        self.assertEqual("review-20", calls[2][calls[2].index("--parent") + 1])
        self.assertEqual("partiful-integrator", calls[2][calls[2].index("--assignee") + 1])
        self.assertIn("partiful:implementation:20:integrate", calls[2])

    def test_revision_attempt_has_unique_keys_and_branch(self) -> None:
        issue = Issue(20, "domain types", "https://example/20", "OPEN", (IMPLEMENTATION_LABEL,), (), ())
        calls: list[list[str]] = []

        def fake_run(command: list[str]) -> str:
            calls.append(command)
            return json.dumps({"task_id": f"task-{len(calls)}"})

        create_cards(issue, attempt="987654", run=fake_run)

        self.assertIn("partiful:implementation:20:987654:implement", calls[0])
        branch = calls[0][calls[0].index("--branch") + 1]
        self.assertIn("987654", branch)
        self.assertIn("partiful:implementation:20:987654:review", calls[1])
        self.assertIn("partiful:implementation:20:987654:integrate", calls[2])


if __name__ == "__main__":
    unittest.main()
