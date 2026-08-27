from __future__ import annotations

import unittest

from scripts.deterministic_merge_gate import validate_gate


class DeterministicMergeGateTests(unittest.TestCase):
    def test_rejects_all_required_predicate_failures(self) -> None:
        result = validate_gate({"head": "b" * 40, "reviewed_sha": "a" * 40, "paths": ["bad.go"], "allowed_paths": ["internal/app/**"], "blockers": [{"state": "OPEN"}], "checks": [{"state": "PENDING"}], "latest_review": {"verdict": "REQUEST_CHANGES"}, "review_cycles": 4})
        self.assertFalse(result["ok"])
        self.assertTrue({"sha_mismatch", "out_of_write_set", "open_blocker", "required_check_not_success", "latest_review_not_approve", "too_many_review_cycles"}.issubset({failure["code"] for failure in result["failures"]}))

    def test_accepts_exact_approved_clean_packet(self) -> None:
        sha = "a" * 40
        result = validate_gate({"head": sha, "reviewed_sha": sha, "paths": ["internal/app/auth_ops.go"], "allowed_paths": ["internal/app/**"], "blockers": [], "checks": [{"context":"test", "state": "SUCCESS"}], "latest_review": {"verdict": "APPROVE", "sha": sha, "categories": {x:"PASS" for x in ("specification","correctness","domain_model","test_quality","edge_cases","security_privacy","maintainability","domain_adherence","evidence_rigor")}}, "evidence":{"red":"failed","green":"passed"}, "review_cycles": 3})
        self.assertEqual({"ok": True, "failures": []}, result)


if __name__ == "__main__":
    unittest.main()
