from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from scripts import deterministic_merge_gate as gate
from scripts.evidence_frontier_pump import Issue as EvidenceIssue, build_body as evidence_body, select_frontier as evidence_frontier
from scripts.implementation_frontier_pump import Issue, build_implement_body, discover_live_cards, parse_allowed_files, select_wave_for_issues
from scripts.run_frontier_pumps import build_commands
from scripts.verify_implementation_worker_profiles import REQUIRED_PROFILES, verify_worker_profiles

SHA = "a" * 40
CATEGORIES = ["specification", "correctness", "domain_model", "test_quality", "edge_cases", "security_privacy", "maintainability", "domain_adherence", "evidence_rigor"]


def clean_packet() -> dict:
    return {"head": SHA, "reviewed_sha": SHA, "paths": ["internal/app/a.go"], "allowed_paths": ["internal/app/**"], "blockers": [], "checks": [{"context": "test", "state": "SUCCESS"}], "latest_review": {"verdict": "APPROVE", "sha": SHA, "categories": {x: "PASS" for x in CATEGORIES}}, "review_cycles": 3, "evidence": {"red": "go test ./... (expected fail)", "green": "go test ./..."}, "required_checks_declared": True}


class GateAcceptanceTests(unittest.TestCase):
    def test_fail_closed_for_every_required_merge_predicate(self) -> None:
        mutations = {
            "missing_sha": {"reviewed_sha": ""}, "stale_sha": {"reviewed_sha": "b" * 40},
            "missing_checks": {"checks": []}, "pending": {"checks": [{"context": "x", "state": "PENDING"}]},
            "skipped": {"checks": [{"context": "x", "state": "SKIPPED"}]}, "blocker": {"blockers": [{"state": "OPEN"}]},
            "scope": {"paths": ["README.md"]}, "missing_evidence": {"evidence": {"red": "", "green": ""}},
            "categories": {"latest_review": {"verdict": "APPROVE", "sha": SHA, "categories": {"specification": "PASS"}}},
            "cycles": {"review_cycles": 4},
        }
        for name, mutation in mutations.items():
            with self.subTest(name=name):
                packet = clean_packet(); packet.update(mutation)
                self.assertFalse(gate.validate_gate(packet)["ok"])

    def test_no_declared_ci_requires_local_commands(self) -> None:
        packet = clean_packet(); packet.update({"required_checks_declared": False, "checks": [], "local_verification_ran": False})
        self.assertFalse(gate.validate_gate(packet)["ok"])
        packet["local_verification_ran"] = True
        self.assertTrue(gate.validate_gate(packet)["ok"])

    def test_success_re_reads_then_exact_checkout_then_merges_and_reads_back(self) -> None:
        calls: list[list[str]] = []
        pr_views = iter([{"headRefOid": SHA, "files": [{"path": "internal/app/a.go"}], "statusCheckRollup": [{"name": "test", "conclusion": "SUCCESS"}]}, {"headRefOid": SHA, "files": [{"path": "internal/app/a.go"}], "statusCheckRollup": [{"name": "test", "conclusion": "SUCCESS"}]}, {"state": "MERGED"}])
        def run(cmd: list[str]) -> str:
            calls.append(cmd)
            if cmd[:3] == ["gh", "pr", "view"]: return json.dumps(next(pr_views))
            if cmd[:3] == ["gh", "api", "graphql"]: return json.dumps({"data": {"repository": {"issue": {"blockedBy": {"nodes": []}}}}})
            if "/comments" in " ".join(cmd): return json.dumps([{"body": "## Implementation review\nVerdict: APPROVE\nCommit: " + SHA + "\nRED: fail\nGREEN: pass\n" + "\n".join(f"Category-{x}: PASS" for x in CATEGORIES)}])
            if cmd[:3] == ["gh", "issue", "view"]: return json.dumps({"state": "CLOSED"})
            return ""
        with patch.object(gate, "_run", run), patch.object(gate, "checkout_verified_pr_head", return_value=SHA), patch.object(gate, "load_issue_contract", return_value={"paths": ["internal/app/**"], "required_checks": ["test"], "verification": ["go test ./..."]}):
            self.assertEqual(0, gate.main(["--issue", "35", "--pr", "49", "--reviewed-sha", SHA]))
        merge_at = next(i for i, c in enumerate(calls) if c[:3] == ["gh", "pr", "merge"])
        self.assertGreaterEqual(sum(c[:3] == ["gh", "pr", "view"] for c in calls[:merge_at]), 2)
        self.assertEqual(["gh", "pr", "merge", "49", "--squash", "--delete-branch"], calls[merge_at])


class PumpAcceptanceTests(unittest.TestCase):
    def test_multiline_allowed_files_and_live_cards_protect_idempotent_wave(self) -> None:
        body = "Allowed files:\n- `internal/app/**`\n- `cmd/partiful/**`\n\nForbidden: `README.md`"
        self.assertEqual(("internal/app/**", "cmd/partiful/**"), parse_allowed_files(body))
        raw = json.dumps({"tasks": [{"id": "card-34", "idempotency_key": "partiful:implementation:34", "status": "in_progress", "body": body}]})
        cards = discover_live_cards(lambda _: raw)
        result = select_wave_for_issues([Issue(35, "x", "u", "OPEN", ("partiful:implementation",), (), (), ("internal/app/a.go",))], cards)
        self.assertEqual([], result["selected"])
        self.assertIn("active card #34", result["held"][0]["reason"])

    def test_same_card_body_contains_both_roles_and_native_lifecycle(self) -> None:
        body = build_implement_body(Issue(35, "x", "u", "OPEN", ("partiful:implementation",), (), (), ("internal/app/a.go",)))
        for text in ("Implementer phase", "Reviewer phase", "request-review", "partiful-code-reviewer", "exact 40-character SHA", "nine categories", "Category-specification", "REQUEST_CHANGES", "same card", "attempt 3", "evidence-block", "deterministic_merge_gate.py"):
            self.assertIn(text, body)

    def test_evidence_uses_dedicated_profile_and_all_nonblocked_tasks_are_schedulable(self) -> None:
        issues = [EvidenceIssue(n, "x", "u", "OPEN", ("wayfinder:task",), (), (), "requires login") for n in (20, 21, 22, 23, 24, 25, 26, 28)]
        result = evidence_frontier(issues)
        self.assertEqual([20, 21, 22, 23, 24, 25, 26, 28], [x.number for x in result["selected"]])
        for text in ("partiful-evidence", "public/repository", "unsupported", "no credentials", "no live mutation"):
            self.assertIn(text, evidence_body(issues[0]))

    def test_all_three_profiles_are_audited(self) -> None:
        self.assertIn("partiful-evidence", REQUIRED_PROFILES)
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            for name in REQUIRED_PROFILES:
                d = root / name; d.mkdir()
                (d / "config.yaml").write_text("toolsets: [terminal, file, skills]\nterminal:\n  env_passthrough: []\n  shell_init_files: []\n  auto_source_bashrc: false\n")
                (d / ".env").write_text("")
            self.assertEqual(set(REQUIRED_PROFILES), set(verify_worker_profiles(root)))

    def test_scheduler_is_quiet_deterministic_and_never_merges(self) -> None:
        commands = build_commands(Path("/repo"))
        self.assertEqual(3, len(commands))
        self.assertTrue(all("--quiet" in command for command in commands))
        self.assertTrue(all("deterministic_merge_gate.py" not in " ".join(command) for command in commands))


if __name__ == "__main__": unittest.main()
