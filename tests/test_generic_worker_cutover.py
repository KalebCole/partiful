from __future__ import annotations

import os
import sqlite3
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from scripts import deterministic_merge_gate as gate


class GenericWorkerCutoverTests(unittest.TestCase):
    def test_reviewer_binding_accepts_only_generic_reviewer(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            database = Path(tmp) / "kanban.db"
            connection = sqlite3.connect(database)
            connection.executescript("""
                CREATE TABLE tasks (id TEXT PRIMARY KEY, assignee TEXT, status TEXT, current_run_id INTEGER, idempotency_key TEXT, claim_lock TEXT);
                CREATE TABLE task_runs (id INTEGER PRIMARY KEY, task_id TEXT, profile TEXT, status TEXT, claim_lock TEXT);
                INSERT INTO tasks VALUES ('card-44','code-reviewer','running',7,'partiful:implementation:44','lock-7');
                INSERT INTO task_runs VALUES (7,'card-44','code-reviewer','running','lock-7');
            """)
            connection.close()
            environment = {
                "HERMES_PROFILE": "code-reviewer",
                "HERMES_KANBAN_BOARD": "partiful",
                "HERMES_KANBAN_DB": str(database),
                "HERMES_KANBAN_TASK": "card-44",
                "HERMES_KANBAN_RUN_ID": "7",
                "HERMES_KANBAN_CLAIM_LOCK": "lock-7",
            }
            with patch.dict(os.environ, environment, clear=True):
                self.assertTrue(gate._native_reviewer_provenance(44))
                os.environ["HERMES_PROFILE"] = "partiful-code-reviewer"
                self.assertFalse(gate._native_reviewer_provenance(44))

    def test_obsolete_frontier_orchestrators_are_removed(self) -> None:
        root = Path(__file__).resolve().parents[1]
        for path in (
            "scripts/wayfinder_frontier_pump.py",
            "scripts/implementation_frontier_pump.py",
            "scripts/evidence_frontier_pump.py",
            "scripts/run_frontier_pumps.py",
            "scripts/adopt_issue_34_pr49.py",
        ):
            with self.subTest(path=path):
                self.assertFalse((root / path).exists())


if __name__ == "__main__":
    unittest.main()