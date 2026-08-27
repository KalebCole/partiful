from __future__ import annotations

import unittest

from scripts.select_implementation_wave import select_wave


class SelectImplementationWaveTests(unittest.TestCase):
    def test_greedy_issue_order_holds_overlaps_and_active_conflicts(self) -> None:
        result = select_wave(
            [
                {"number": 36, "paths": ["internal/app/event_ops.go"]},
                {"number": 35, "paths": ["internal/app/**"]},
                {"number": 37, "paths": ["internal/cli/**"]},
                {"number": 38, "paths": ["internal/cli/main.go"]},
            ],
            active_cards=[{"issue": 34, "paths": ["internal/app/**"]}],
        )
        self.assertEqual([37], [item["number"] for item in result["selected"]])
        self.assertEqual({35: "active card #34 overlaps internal/app/**", 36: "active card #34 overlaps internal/app/event_ops.go", 38: "selected issue #37 overlaps internal/cli/main.go"}, {item["number"]: item["reason"] for item in result["held"]})

    def test_exact_and_recursive_paths_overlap_conservatively(self) -> None:
        result = select_wave([
            {"number": 40, "paths": ["internal/mcp/server.go"]},
            {"number": 41, "paths": ["internal/mcp/**"]},
            {"number": 42, "paths": ["docs/a.md"]},
        ])
        self.assertEqual([40, 42], [item["number"] for item in result["selected"]])
        self.assertEqual(41, result["held"][0]["number"])


if __name__ == "__main__":
    unittest.main()
