from __future__ import annotations

import json
import unittest

from scripts.wayfinder_frontier_pump import (
    Issue,
    build_reconcile_body,
    build_resolver_body,
    build_reviewer_body,
    create_cards,
    select_frontier,
)


class SelectFrontierTests(unittest.TestCase):
    def test_selects_only_open_unblocked_unassigned_decisions(self) -> None:
        issues = [
            Issue(11, "composition", "https://example/11", "OPEN", ("wayfinder:decision",), (), ()),
            Issue(12, "packages", "https://example/12", "OPEN", ("wayfinder:decision",), (), ((11, "OPEN"),)),
            Issue(13, "registry", "https://example/13", "OPEN", ("wayfinder:decision",), ("KalebCole",), ()),
            Issue(14, "paging", "https://example/14", "CLOSED", ("wayfinder:decision",), (), ()),
            Issue(15, "evidence", "https://example/15", "OPEN", ("wayfinder:grilling",), (), ()),
            Issue(16, "runtime", "https://example/16", "OPEN", ("wayfinder:decision",), (), ((10, "CLOSED"),)),
        ]

        self.assertEqual([11, 16], [issue.number for issue in select_frontier(issues)])


class BodyContractTests(unittest.TestCase):
    def setUp(self) -> None:
        self.issue = Issue(
            11,
            "Define operation-to-transport composition",
            "https://github.com/KalebCole/partiful/issues/11",
            "OPEN",
            ("wayfinder:decision",),
            (),
            (),
        )

    def test_resolver_body_requires_candidate_without_finalizing(self) -> None:
        body = build_resolver_body(self.issue)

        self.assertIn(self.issue.url, body)
        self.assertIn("docs/wayfinder-autonomy.md", body)
        self.assertIn("Candidate resolution", body)
        self.assertIn("Do not close", body)

    def test_reviewer_body_requires_independent_rubber_duck_verdict(self) -> None:
        body = build_reviewer_body(self.issue)

        self.assertIn(self.issue.url, body)
        self.assertIn("rubber-duck", body)
        self.assertIn("APPROVE", body)
        self.assertIn("REVISE", body)
        self.assertIn("Do not modify the candidate", body)

    def test_reconcile_body_requires_approved_review_and_read_after_write(self) -> None:
        body = build_reconcile_body(self.issue)

        self.assertIn(self.issue.url, body)
        self.assertIn("APPROVE", body)
        self.assertIn("read after write", body.lower())
        self.assertIn("Do not modify repository files", body)


class CardCreationTests(unittest.TestCase):
    def test_creates_reviewer_and_reconcile_cards_with_dependencies(self) -> None:
        issue = Issue(
            11,
            "composition",
            "https://example/11",
            "OPEN",
            ("wayfinder:decision",),
            (),
            (),
        )
        calls: list[list[str]] = []

        def fake_run(command: list[str]) -> str:
            calls.append(command)
            if "partiful:github:11:resolve" in command:
                task_id = "resolver-11"
            elif "partiful:github:11:review" in command:
                task_id = "reviewer-11"
            else:
                task_id = "reconcile-11"
            return json.dumps({"task_id": task_id})

        created = create_cards(issue, run=fake_run)

        self.assertEqual(("resolver-11", "reviewer-11", "reconcile-11"), created)
        self.assertIn("--goal", calls[0])
        self.assertIn("--goal-max-turns", calls[0])
        self.assertIn("partiful:github:11:resolve", calls[0])
        self.assertIn("--parent", calls[1])
        review_parent_index = calls[1].index("--parent")
        self.assertEqual("resolver-11", calls[1][review_parent_index + 1])
        self.assertIn("partiful:github:11:review", calls[1])
        self.assertIn("--parent", calls[2])
        reconcile_parent_index = calls[2].index("--parent")
        self.assertEqual("reviewer-11", calls[2][reconcile_parent_index + 1])
        self.assertIn("partiful:github:11:reconcile", calls[2])


if __name__ == "__main__":
    unittest.main()
