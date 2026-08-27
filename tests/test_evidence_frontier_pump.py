from __future__ import annotations

import unittest

from scripts.evidence_frontier_pump import Issue, build_body, create_card, select_frontier


class EvidenceFrontierPumpTests(unittest.TestCase):
    def test_selects_supported_unassigned_task_and_holds_capability_required(self) -> None:
        issues = [
            Issue(20, "live observation", "u20", "OPEN", ("wayfinder:task",), (), (), "requires authenticated observation"),
            Issue(28, "repository probe", "u28", "OPEN", ("wayfinder:task",), (), (), "credential-free repository contract evidence"),
            Issue(27, "blocked", "u27", "OPEN", ("wayfinder:task",), (), ((22, "OPEN"),), "repository evidence"),
        ]
        result = select_frontier(issues)
        self.assertEqual([20, 28], [issue.number for issue in result["selected"]])
        self.assertEqual([], result["held"])

    def test_body_has_bounded_probe_and_no_live_mutation(self) -> None:
        body = build_body(Issue(28, "probe", "url", "OPEN", ("wayfinder:task",), (), (), "repository evidence"))
        for text in ("bounded probe", "redact", "no credentials", "no live mutation", "Do not edit repository files", "Do not create review or integrate"):
            self.assertIn(text, body)

    def test_create_card_uses_isolated_issue_worktree(self) -> None:
        issue = Issue(28, "probe", "url", "OPEN", ("wayfinder:task",), (), (), "repository evidence")
        commands: list[list[str]] = []
        create_card(issue, lambda command: commands.append(command) or '{"task_id":"card-28"}')
        command = commands[0]
        self.assertTrue(command[command.index("--workspace") + 1].startswith("worktree:"))
        self.assertEqual("partiful/evidence-28", command[command.index("--branch") + 1])


if __name__ == "__main__":
    unittest.main()
