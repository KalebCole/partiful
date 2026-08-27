from __future__ import annotations

import contextlib
import io
import json
import os
import unittest
from pathlib import Path
from unittest.mock import patch

from scripts import adopt_issue_34_pr49 as adopt
from scripts import deterministic_merge_gate as gate
from scripts import evidence_frontier_pump as evidence
from scripts import implementation_frontier_pump as implementation
from scripts import run_frontier_pumps as pumps

ROOT = Path(__file__).resolve().parents[1]
SHA = "a" * 40
CATEGORIES = (
    "specification", "correctness", "domain_model", "test_quality", "edge_cases",
    "security_privacy", "maintainability", "domain_adherence", "evidence_rigor",
)


def review(verdict: str, sha: str = SHA) -> str:
    return "\n".join((
        "## Implementation review", f"Verdict: {verdict}", f"Commit: {sha}",
        "RED: expected failure", "GREEN: passing", *(f"Category-{name}: PASS" for name in CATEGORIES),
    ))


def pr_view(head: str) -> dict:
    return {"headRefOid": head, "files": [{"path": "internal/app/a.go"}], "statusCheckRollup": []}


class MergeGateHardeningTests(unittest.TestCase):
    def test_head_drift_during_immediate_premerge_reread_never_merges(self) -> None:
        calls: list[list[str]] = []
        views = iter([pr_view(SHA), pr_view("b" * 40)])

        def run(command: list[str]) -> str:
            calls.append(command)
            if command[:3] == ["gh", "pr", "view"]:
                return json.dumps(next(views))
            if command[:3] == ["gh", "api", "graphql"]:
                return json.dumps({"data": {"repository": {"issue": {"blockedBy": {"nodes": []}}}}})
            if "/comments" in " ".join(command):
                return json.dumps([{"body": review("APPROVE")}])
            if command == ["verify-local"]:
                return ""
            raise AssertionError(command)

        contract = {"paths": ["internal/app/**"], "required_checks": [], "verification": ["verify-local"]}
        with patch.object(gate, "_run", run), patch.object(gate, "checkout_verified_pr_head", return_value=SHA), patch.object(gate, "load_issue_contract", return_value=contract):
            self.assertNotEqual(0, gate.main(["--issue", "34", "--pr", "49", "--reviewed-sha", SHA]))
        self.assertEqual(2, sum(call[:3] == ["gh", "pr", "view"] for call in calls))
        self.assertEqual([], [call for call in calls if call[:3] == ["gh", "pr", "merge"]])

    def test_latest_structured_review_request_changes_beats_older_approval(self) -> None:
        packet = gate._packet(34, 49, SHA, lambda command: json.dumps(pr_view(SHA)) if command[:3] == ["gh", "pr", "view"] else json.dumps({"data": {"repository": {"issue": {"blockedBy": {"nodes": []}}}}}) if command[:3] == ["gh", "api", "graphql"] else json.dumps([{"body": review("APPROVE")}, {"body": review("REQUEST_CHANGES")}]), {"paths": ["internal/app/**"], "required_checks": [], "verification": ["go test ./..."]})
        self.assertEqual("REQUEST_CHANGES", packet["latest_review"]["verdict"])
        self.assertEqual(set(CATEGORIES), set(packet["latest_review"]["categories"]))
        self.assertFalse(gate.validate_gate(packet)["ok"])


class AdoptionAndLifecycleHardeningTests(unittest.TestCase):
    def test_adoption_uses_exact_pr_sha_one_review_card_and_idempotent_rerun(self) -> None:
        commands: list[list[str]] = []
        cards: dict[str, str] = {}
        state = {"status": "running"}

        def run(command: list[str]) -> str:
            commands.append(command)
            if "create" in command:
                key = command[command.index("--idempotency-key") + 1]
                cards.setdefault(key, "card-34")
                return json.dumps({"task_id": cards[key]})
            if "show" in command:
                return json.dumps({"id": "card-34", "status": state["status"]})
            if "request-review" in command:
                state["status"] = "review"
                return ""
            raise AssertionError(command)

        self.assertEqual("card-34", adopt.adopt(run))
        self.assertEqual("card-34", adopt.adopt(run))
        self.assertEqual({"partiful:implementation:34": "card-34"}, cards)
        create_commands = [command for command in commands if "create" in command]
        review_commands = [command for command in commands if "request-review" in command]
        show_commands = [command for command in commands if "show" in command]
        self.assertEqual(2, len(create_commands))
        self.assertEqual(2, len(show_commands))
        self.assertEqual(1, len(review_commands))
        for command in create_commands:
            self.assertIn("partiful:implementation:34", command)
            self.assertIn(adopt.SHA, command[command.index("--body") + 1])
            self.assertEqual("partiful-implementer", command[command.index("--assignee") + 1])
            self.assertEqual("running", command[command.index("--initial-status") + 1])
            self.assertEqual(
                "partiful/issue-34-initial-implement",
                command[command.index("--branch") + 1],
            )
            self.assertIn("PR #49", command[command.index("--body") + 1])
        self.assertEqual(
            ["hermes", "kanban", "--board", "partiful", "show", "card-34", "--json"],
            show_commands[0],
        )
        command = review_commands[0]
        self.assertEqual(["hermes", "kanban", "--board", "partiful", "request-review", "card-34"], command[:6])
        self.assertEqual("partiful-code-reviewer", command[command.index("--reviewer") + 1])
        self.assertIn(adopt.SHA, command[command.index("--metadata") + 1])

    def test_native_same_card_transitions_use_review_then_return_commands(self) -> None:
        commands: list[list[str]] = []
        run = lambda command: commands.append(command) or ""
        implementation.request_native_review("card-34", "https://example.test/pr/49", SHA, run)
        implementation.request_changes_on_same_card("card-34", "missing exact-SHA evidence", run)
        self.assertEqual(["hermes", "kanban", "--board", "partiful", "request-review", "card-34"], commands[0][:6])
        self.assertIn("partiful-code-reviewer", commands[0])
        self.assertEqual(["hermes", "kanban", "--board", "partiful", "request-changes", "card-34", "missing exact-SHA evidence"], commands[1])


class PumpHardeningTests(unittest.TestCase):
    def test_child_failure_returns_same_nonzero_in_order_and_stops(self) -> None:
        calls: list[list[str]] = []
        outcomes = iter([(0, "first\n", ""), (17, "", "second failed\n")])

        class Result:
            def __init__(self, rc: int, stdout: str, stderr: str) -> None:
                self.returncode, self.stdout, self.stderr = rc, stdout, stderr

        def run(command: list[str], **_: object) -> Result:
            calls.append(command)
            return Result(*next(outcomes))

        stderr = io.StringIO()
        with patch.object(pumps, "build_commands", return_value=[["first"], ["second"], ["unsafe-later"]]), patch.object(pumps.subprocess, "run", run), contextlib.redirect_stderr(stderr):
            self.assertEqual(17, pumps.main(["--quiet"]))
        self.assertEqual([["first"], ["second"]], calls)
        self.assertIn("second failed", stderr.getvalue())

    def test_evidence_key_is_stable_and_live_duplicate_is_suppressed(self) -> None:
        issue = evidence.Issue(20, "probe", "url", "OPEN", ("wayfinder:task",), (), (), "")
        commands: list[list[str]] = []
        self.assertEqual("partiful:evidence:20", evidence.idempotency_key(issue))
        self.assertEqual([], evidence.create_missing_cards([issue], [{"idempotency_key": "partiful:evidence:20", "status": "ready"}], lambda command: commands.append(command) or json.dumps({"id": "unused"})))
        self.assertEqual([], commands)

    def test_kanban_list_payload_accepts_native_json_array(self) -> None:
        cards = [{"id": "card-34", "status": "review", "idempotency_key": "partiful:implementation:34"}]
        run = lambda _command: json.dumps(cards)
        self.assertEqual(
            [{"issue": 34, "paths": [], "card": "card-34"}],
            implementation.discover_live_cards(run),
        )
        self.assertEqual(cards, evidence.discover_live_cards(run))

    def test_evidence_quiet_has_no_stdout_and_keeps_failure_status(self) -> None:
        stdout, stderr = io.StringIO(), io.StringIO()
        with patch.object(evidence, "fetch_issues", side_effect=RuntimeError("boom")), contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            self.assertEqual(1, evidence.main(["--quiet"]))
        self.assertEqual("", stdout.getvalue())
        self.assertIn("FAIL: boom", stderr.getvalue())

    def test_frontier_subprocess_environment_keeps_home_without_secret_passthrough(self) -> None:
        environments: list[dict[str, str]] = []

        class Result:
            returncode = 0
            stdout = "ok"
            stderr = ""

        def run(_command: list[str], **kwargs: object) -> Result:
            environments.append(dict(kwargs["env"]))
            return Result()

        with patch.dict(os.environ, {"HOME": "/private/test-home", "GH_TOKEN": "secret", "UNRELATED_SECRET": "nope"}, clear=True), patch.object(implementation.subprocess, "run", run), patch.object(evidence.subprocess, "run", run):
            self.assertEqual("ok", implementation._run(["gh", "api"]))
            self.assertEqual("ok", evidence._run(["gh", "api"]))

        self.assertEqual(2, len(environments))
        for environment in environments:
            self.assertEqual("/private/test-home", environment.get("HOME"))
            self.assertIn("PATH", environment)
            self.assertIn(
                "/private/test-home/.hermes/hermes-agent/venv/bin",
                environment["PATH"].split(":"),
            )
            self.assertNotIn("GH_TOKEN", environment)
            self.assertNotIn("UNRELATED_SECRET", environment)


class DurableContractHardeningTests(unittest.TestCase):
    def test_wayfinder_document_preserves_decision_lane_and_operator_contracts(self) -> None:
        text = (ROOT / "docs/wayfinder-autonomy.md").read_text()
        self.assertIn("`EVIDENCE_REQUIRED`", text)
        self.assertIn("`OWNER_GATE`", text)
        self.assertIn("After two revision verdicts, the cartographer adjudicates", text)
        self.assertIn("Implementer, reviewer, and evidence profiles are audited fail-closed", text)
        self.assertIn("python3 scripts/run_frontier_pumps.py --quiet", text)
        self.assertIn("scripts/adopt_issue_34_pr49.py", text)

    def test_write_set_contract_exactly_matches_issues_and_issue_specific_limits(self) -> None:
        contract = json.loads((ROOT / "config/implementation-write-sets.json").read_text())
        self.assertEqual({str(issue) for issue in range(34, 45)}, set(contract))
        self.assertNotIn("internal/compose/import_graph_test.go", contract["41"]["paths"])
        self.assertEqual(["internal/compose/import_graph_test.go"], contract["41"]["excluded_paths"])
        self.assertEqual("README.md only installation and release verification sections; path-only gate fails closed for README.md", contract["44"]["supplemental_scope"])
        shared = {
            "go test ./... -count=1",
            "python3 scripts/verify_go_package_graph.py",
            "python3 scripts/verify_command_model.py",
            "python3 scripts/verify_implementation_worker_profiles.py",
        }
        focused = {
            "34": "go test ./internal/app -count=1",
            "35": "go test ./internal/app -count=1",
            "36": "go test ./internal/app -count=1",
            "37": "go test ./internal/app -count=1",
            "38": "go test ./internal/app -count=1",
            "39": "go test ./internal/cli ./cmd/partiful -count=1",
            "40": "go test ./internal/mcp ./cmd/partiful-mcp -count=1",
            "41": "python3 scripts/smoke_binaries.py",
            "42": "python3 -m unittest discover -s scripts/tests -p 'test_*contract*.py' -v",
            "43": "python3 -m unittest discover -s scripts/tests -p 'test_partiful_verify.py' -v",
            "44": "python3 -m unittest discover -s scripts/tests -p 'test_release.py' -v",
        }
        for issue, issue_contract in contract.items():
            verification = set(issue_contract["verification"])
            self.assertTrue(shared <= verification, issue)
            self.assertIn(focused[issue], verification, issue)


if __name__ == "__main__":
    unittest.main()
