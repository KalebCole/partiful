from __future__ import annotations

import json
import unittest
from unittest.mock import patch

from scripts import deterministic_merge_gate as gate

SHA = "a" * 40
CATEGORIES = ["specification", "correctness", "domain_model", "test_quality", "edge_cases", "security_privacy", "maintainability", "domain_adherence", "evidence_rigor"]


def clean_packet() -> dict:
    return {"head": SHA, "reviewed_sha": SHA, "reviewer_provenance": True, "paths": ["internal/app/a.go"], "allowed_paths": ["internal/app/**"], "blockers": [], "checks": [{"context": "test", "state": "SUCCESS"}], "required_checks": ["test"], "no_required_ci": False, "latest_review": {"verdict": "APPROVE", "sha": SHA, "categories": {x: "PASS" for x in CATEGORIES}}, "review_cycles": 3, "evidence": {"red": "go test ./... (expected fail)", "green": "go test ./..."}}


class GateAcceptanceTests(unittest.TestCase):
    def test_fail_closed_for_every_required_merge_predicate(self) -> None:
        mutations = {
            "missing_sha": {"reviewed_sha": ""}, "stale_sha": {"reviewed_sha": "b" * 40},
            "missing_checks": {"checks": []}, "pending": {"checks": [{"context": "x", "state": "PENDING"}]},
            "skipped": {"checks": [{"context": "x", "state": "SKIPPED"}]}, "blocker": {"blockers": [{"state": "OPEN"}]},
            "scope": {"paths": ["README.md"]}, "missing_evidence": {"evidence": {"red": "", "green": ""}},
            "categories": {"latest_review": {"verdict": "APPROVE", "sha": SHA, "categories": {"specification": "PASS"}}},
            "cycles": {"review_cycles": 4},
            "forged_comment": {"reviewer_provenance": False},
        }
        for name, mutation in mutations.items():
            with self.subTest(name=name):
                packet = clean_packet(); packet.update(mutation)
                self.assertFalse(gate.validate_gate(packet)["ok"])

    def test_no_declared_ci_requires_explicit_exception_and_local_commands(self) -> None:
        packet = clean_packet(); packet.update({"required_checks": [], "checks": [], "no_required_ci": True, "local_verification_ran": False})
        self.assertFalse(gate.validate_gate(packet)["ok"])
        packet["local_verification_ran"] = True
        self.assertTrue(gate.validate_gate(packet)["ok"])

    def test_ci_contract_rejects_missing_or_ambiguous_declaration(self) -> None:
        missing = clean_packet(); missing.update({"required_checks": [], "checks": [], "local_verification_ran": True}); missing.pop("no_required_ci")
        ambiguous = clean_packet(); ambiguous["no_required_ci"] = True
        for packet in (missing, ambiguous):
            with self.subTest(packet=packet):
                failures = gate.validate_gate(packet)["failures"]
                self.assertIn("ambiguous_ci_contract", {failure["code"] for failure in failures})

    def test_success_re_reads_then_exact_checkout_then_merges_and_reads_back(self) -> None:
        calls: list[list[str]] = []
        pr_views = iter([{"headRefOid": SHA, "files": [{"path": "internal/app/a.go"}], "statusCheckRollup": [{"name": "test", "conclusion": "SUCCESS"}]}, {"headRefOid": SHA, "files": [{"path": "internal/app/a.go"}], "statusCheckRollup": [{"name": "test", "conclusion": "SUCCESS"}]}, {"state": "MERGED"}])
        def run(cmd: list[str]) -> str:
            calls.append(cmd)
            if cmd == ["git", "branch", "--show-current"]: return "main"
            if cmd[:3] == ["gh", "pr", "view"]: return json.dumps(next(pr_views))
            if cmd[:3] == ["gh", "api", "graphql"]: return json.dumps({"data": {"repository": {"issue": {"blockedBy": {"nodes": []}}}}})
            if "/comments" in " ".join(cmd): return json.dumps([{"body": "## Implementation review\nVerdict: APPROVE\nCommit: " + SHA + "\nRED: fail\nGREEN: pass\n" + "\n".join(f"Category-{x}: PASS" for x in CATEGORIES)}])

            if cmd[:3] == ["gh", "issue", "view"]: return json.dumps({"state": "CLOSED"})
            return ""
        with patch.object(gate, "_run", run), patch.object(gate, "_native_reviewer_provenance", return_value=True), patch.object(gate, "checkout_verified_pr_head", return_value=SHA), patch.object(gate, "load_issue_contract", return_value={"paths": ["internal/app/**"], "required_checks": ["test"], "verification": ["go test ./..."]}):
            self.assertEqual(0, gate.main(["--issue", "35", "--pr", "49", "--reviewed-sha", SHA]))
        merge_at = next(i for i, c in enumerate(calls) if c[:3] == ["gh", "pr", "merge"])
        self.assertGreaterEqual(sum(c[:3] == ["gh", "pr", "view"] for c in calls[:merge_at]), 2)
        self.assertEqual(["gh", "pr", "merge", "49", "--squash", "--match-head-commit", SHA], calls[merge_at])
        self.assertEqual(["git", "checkout", "main"], calls[-1])



if __name__ == "__main__": unittest.main()
