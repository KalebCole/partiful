from __future__ import annotations

import json
import unittest

from scripts.checkout_verified_pr_head import checkout_verified_pr_head


class CheckoutVerifiedPrHeadTests(unittest.TestCase):
    def test_fetches_pr_ref_and_detaches_at_exact_declared_head(self) -> None:
        calls: list[list[str]] = []
        sha = "a" * 40

        def run(command: list[str]) -> str:
            calls.append(command)
            if command[:3] == ["gh", "pr", "view"]:
                return json.dumps({"headRefOid": sha})
            if command[:3] == ["git", "rev-parse", "FETCH_HEAD"]:
                return sha + "\n"
            if command[:3] == ["git", "rev-parse", "HEAD"]:
                return sha + "\n"
            return ""

        result = checkout_verified_pr_head(42, run=run)

        self.assertEqual(sha, result)
        self.assertIn(["git", "fetch", "--force", "origin", "pull/42/head"], calls)
        self.assertIn(["git", "checkout", "--detach", sha], calls)
        self.assertEqual(["git", "rev-parse", "HEAD"], calls[-1])

    def test_rejects_fetched_commit_that_differs_from_github_head(self) -> None:
        declared = "a" * 40
        fetched = "b" * 40

        def run(command: list[str]) -> str:
            if command[:3] == ["gh", "pr", "view"]:
                return json.dumps({"headRefOid": declared})
            if command[:3] == ["git", "rev-parse", "FETCH_HEAD"]:
                return fetched + "\n"
            return ""

        with self.assertRaisesRegex(RuntimeError, "fetched PR head mismatch"):
            checkout_verified_pr_head(42, run=run)


if __name__ == "__main__":
    unittest.main()
