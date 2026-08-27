from __future__ import annotations

import json
import unittest

from scripts.implementation_frontier_pump import (
    IMPLEMENTATION_LABEL,
    Issue,
    build_implement_body,
    build_review_body,
    create_card,
    discover_live_cards,
    parse_allowed_files,
    select_frontier,
    select_wave_for_issues,
)


class SingleCardFrontierTests(unittest.TestCase):
    def test_selects_only_open_unblocked_unassigned_issues(self) -> None:
        issues = [
            Issue(34, "app", "https://example/34", "OPEN", (IMPLEMENTATION_LABEL,), (), (), ("internal/app/service.go",)),
            Issue(35, "auth", "https://example/35", "OPEN", (IMPLEMENTATION_LABEL,), (), (), ("internal/app/auth_ops.go",)),
            Issue(36, "blocked", "https://example/36", "OPEN", (IMPLEMENTATION_LABEL,), (), ((34, "OPEN"),), ("internal/app/event_ops.go",)),
            Issue(37, "claimed", "https://example/37", "OPEN", (IMPLEMENTATION_LABEL,), ("KalebCole",), (), ("internal/app/people_ops.go",)),
            Issue(38, "closed", "https://example/38", "CLOSED", (IMPLEMENTATION_LABEL,), (), (), ("internal/app/blast_ops.go",)),
        ]
        self.assertEqual([34, 35], [item.number for item in select_frontier(issues)])

    def test_wave_rejects_ready_issue_without_allowed_paths(self) -> None:
        issue = Issue(34, "app", "https://example/34", "OPEN", (IMPLEMENTATION_LABEL,), (), (), ())
        with self.assertRaisesRegex(RuntimeError, "issue #34 has no allowed paths"):
            select_wave_for_issues([issue], [])

    def test_discovery_rejects_active_card_without_allowed_paths(self) -> None:
        payload = [{
            "id": "card-34",
            "status": "running",
            "idempotency_key": "partiful:implementation:34",
            "body": "Implement issue #34 without a write-set contract.",
        }]
        with self.assertRaisesRegex(RuntimeError, "card card-34 for issue #34 has no allowed paths"):
            discover_live_cards(lambda _command: json.dumps(payload))


class SingleCardContractTests(unittest.TestCase):
    def setUp(self) -> None:
        self.issue = Issue(35, "auth", "https://github.com/KalebCole/partiful/issues/35", "OPEN", (IMPLEMENTATION_LABEL,), (), (), ("internal/app/auth_ops.go",))

    def test_implement_body_requires_execution_packet_and_native_review(self) -> None:
        body = build_implement_body(self.issue)
        for text in ("docs/agentic-engineering.md", "strict feature/bug TDD", "Allowed files", "internal/app/auth_ops.go", "verification", "no credentials", "no live mutations", "PR+handoff readback", "request-review", "partiful-code-reviewer"):
            self.assertIn(text, body)
        self.assertEqual(self.issue.allowed_paths, tuple(parse_allowed_files(body)))

    def test_review_body_uses_exact_head_and_routes_verdicts(self) -> None:
        body = build_review_body(self.issue)
        for text in ("exact PR head", "structured verdict", "APPROVE", "REQUEST_CHANGES", "deterministic_merge_gate.py", "same card", "max 3 reviews"):
            self.assertIn(text, body)

    def test_create_card_has_one_stable_identity_and_worktree(self) -> None:
        calls: list[list[str]] = []
        def run(command: list[str]) -> str:
            calls.append(command)
            return json.dumps({"task_id": "implement-35"})
        self.assertEqual("implement-35", create_card(self.issue, run=run))
        command = calls[0]
        self.assertIn("partiful:implementation:35", command)
        self.assertIn("partiful/issue-35", command)
        self.assertEqual("partiful-implementer", command[command.index("--assignee") + 1])
        self.assertNotIn("--parent", command)
        self.assertNotIn("partiful-integrator", command)


if __name__ == "__main__":
    unittest.main()
