from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from scripts import verify_contract_integrity as integrity


class ContractIntegrityTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        (self.root / "spec").mkdir()
        (self.root / "evidence" / "research").mkdir(parents=True)
        (self.root / "internal" / "version").mkdir(parents=True)
        (self.root / "spec" / "partiful.openapi.json").write_text(json.dumps({
            "openapi": "3.1.0",
            "info": {"version": "r1", "description": "prose"},
            "servers": [{"url": "https://api.example.invalid"}],
            "paths": {"/read": {"post": {"operationId": "read", "responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"type": "object", "properties": {"state": {"type": "string", "enum": ["A"]}}}}}}}}}},
            "components": {"securitySchemes": {}, "schemas": {}},
        }))
        (self.root / "evidence" / "research" / "read.md").write_text("# Read evidence\n\n## Request shape\n")
        (self.root / "evidence" / "ledger.json").write_text(json.dumps({
            "contractRevision": "r1",
            "contractPath": "spec/partiful.openapi.json",
            "allowedClassifications": ["explicit-unknown"],
            "claims": {"#/paths/~1read/post/operationId": {"classification": "explicit-unknown", "citation": "evidence/research/read.md#request-shape"}},
            "operations": {"read": {"classification": "explicit-unknown", "citation": "evidence/research/read.md#request-shape"}},
            "materialClaimCount": 1,
        }))
        (self.root / "internal" / "version" / "version.go").write_text('package version\nconst TransportContractRevision = "r1"\nconst CommandContractRevision = "1"\n')

    def tearDown(self) -> None:
        self.temp.cleanup()

    def test_revision_parity_returns_reviewed_revision(self) -> None:
        self.assertEqual("r1", integrity.verify_revision_parity(self.root))

    def test_revision_parity_rejects_any_mismatch(self) -> None:
        ledger = json.loads((self.root / "evidence" / "ledger.json").read_text())
        ledger["contractRevision"] = "r2"
        (self.root / "evidence" / "ledger.json").write_text(json.dumps(ledger))
        with self.assertRaisesRegex(integrity.IntegrityError, "revision mismatch"):
            integrity.verify_revision_parity(self.root)

    def test_provenance_requires_allowed_classification_resolvable_pointer_and_anchor(self) -> None:
        self.assertEqual(1, integrity.verify_provenance(self.root))
        ledger = json.loads((self.root / "evidence" / "ledger.json").read_text())
        ledger["claims"]["#/paths/~1read/post/operationId"]["citation"] = "evidence/research/read.md#missing"
        (self.root / "evidence" / "ledger.json").write_text(json.dumps(ledger))
        with self.assertRaisesRegex(integrity.IntegrityError, "citation"):
            integrity.verify_provenance(self.root)

    def test_semantic_snapshot_ignores_prose_but_detects_closed_enum_change(self) -> None:
        spec = json.loads((self.root / "spec" / "partiful.openapi.json").read_text())
        before = integrity.semantic_transport_snapshot(spec)
        spec["info"]["description"] = "new prose"
        spec["paths"]["/read"]["post"]["responses"]["200"]["description"] = "new response prose"
        self.assertEqual(before, integrity.semantic_transport_snapshot(spec))
        spec["paths"]["/read"]["post"]["responses"]["200"]["content"]["application/json"]["schema"]["properties"]["state"]["enum"].append("B")
        self.assertNotEqual(before, integrity.semantic_transport_snapshot(spec))

    def test_executable_transport_snapshot_tracks_behavior_not_formatting_or_comments(self) -> None:
        transport = self.root / "internal" / "transport" / "callable"
        app = self.root / "internal" / "app"
        transport.mkdir(parents=True)
        app.mkdir(parents=True)
        source = transport / "client.go"
        source.write_text('package callable\n// old comment\nfunc invoke(t Transport) { t.Create() }\n')
        (app / "event_ops.go").write_text('package app\nfunc create(t Transport) { t.Create() }\n')
        before = integrity.executable_transport_snapshot(self.root)
        source.write_text('package callable\n\n// new comment\nfunc invoke(t Transport) {\n\tt.Create()\n}\n')
        self.assertEqual(before, integrity.executable_transport_snapshot(self.root))
        source.write_text('package callable\nfunc invoke(t Transport) { t.Cancel() }\n')
        self.assertNotEqual(before, integrity.executable_transport_snapshot(self.root))

    def test_executable_transport_snapshot_covers_required_contract_classes(self) -> None:
        transport = self.root / "internal" / "transport" / "callable"
        app = self.root / "internal" / "app"
        transport.mkdir(parents=True)
        app.mkdir(parents=True)
        sources = {
            transport / "client.go": "package callable\nfunc request() { buildRequest() }\nfunc validate() bool { return accepted() }\n",
            app / "event_ops.go": "package app\nfunc create(t Transport) { t.Create() }\n",
            app / "project.go": "package app\nfunc project(v Value) Result { return projectEvent(v) }\n",
            app / "errors.go": "package app\nfunc classify(err error) Error { return classifyKnown(err) }\n",
        }
        for path, source in sources.items():
            path.write_text(source)
        before = integrity.executable_transport_snapshot(self.root)
        mutations = {
            "request construction": (transport / "client.go", "buildRequest", "buildAlternateRequest"),
            "strict validator": (transport / "client.go", "accepted", "rejected"),
            "operation composition": (app / "event_ops.go", "Create", "Cancel"),
            "projector": (app / "project.go", "projectEvent", "projectGuest"),
            "error classifier": (app / "errors.go", "classifyKnown", "classifyUnknown"),
        }
        for contract_class, (path, old, new) in mutations.items():
            with self.subTest(contract_class=contract_class):
                original = sources[path]
                path.write_text(original.replace(old, new))
                self.assertNotEqual(before, integrity.executable_transport_snapshot(self.root))
                path.write_text(original)

    def test_semantic_diff_classifies_evidence_transport_and_command_changes(self) -> None:
        base = {"transport": {"paths": {}}, "command": {"revision": "1"}, "evidence": {"digest": "a"}}
        evidence = json.loads(json.dumps(base)); evidence["evidence"]["digest"] = "b"
        transport = json.loads(json.dumps(base)); transport["transport"]["paths"]["/x"] = {}
        command = json.loads(json.dumps(base)); command["command"]["revision"] = "2"
        self.assertEqual("evidence_only", integrity.classify_semantic_diff(base, evidence))
        self.assertEqual("transport_release_required", integrity.classify_semantic_diff(base, transport))
        self.assertEqual("command_release_required", integrity.classify_semantic_diff(base, command))
        self.assertFalse(integrity.semantic_diff_requires_baseline_update("evidence_only"))
        self.assertTrue(integrity.semantic_diff_requires_baseline_update("transport_release_required"))

    def test_sanitized_fixture_rejects_secrets_identifiers_messages_and_urls(self) -> None:
        integrity.validate_sanitized_fixture({"status": 400, "shape": {"error.code": "string"}})
        for unsafe in [
            {"authorization": "Bearer abc"}, {"phone": "+12065550123"},
            {"message": "private words"}, {"event_id": "raw-id"},
            {"source_url": "https://example.invalid/x?secret=1"}, {"api_key": "raw"},
        ]:
            with self.subTest(unsafe=unsafe), self.assertRaises(integrity.IntegrityError):
                integrity.validate_sanitized_fixture(unsafe)

    def test_command_registry_invariants_use_executable_schema_shape(self) -> None:
        commands = []
        for number in range(1, 25):
            commands.append({
                "id": f"CMD-{number:03d}", "cli_path": "auth login" if number == 1 else f"group command-{number}",
                "risk": "interactive" if number == 1 else "read", "dry_run": False,
                "mcp": None if number == 1 else {"name": f"tool_{number}"},
            })
        snapshot = {"commands": commands, "mcp_tools": [f"tool_{n}" for n in range(2, 25)], "cli_only_commands": ["auth login"]}
        self.assertEqual((24, 23), integrity.verify_command_registry(snapshot))
        semantic = integrity.semantic_command_snapshot(snapshot)
        self.assertEqual(24, len(semantic["commands"]))
        self.assertEqual({"id", "cli_path", "mcp_name", "sha256"}, set(semantic["commands"][0]))
        snapshot["commands"][2]["id"] = "CMD-002"
        with self.assertRaises(integrity.IntegrityError):
            integrity.verify_command_registry(snapshot)

    def test_fixture_replay_manifest_is_closed_and_runner_failure_is_reported(self) -> None:
        manifest = {"version": 1, "packages": ["./internal/transport/...", "./internal/app/..."]}
        calls: list[list[str]] = []
        integrity.replay_sanitized_fixtures(self.root, manifest, lambda command, **kwargs: calls.append(command) or type("Result", (), {"returncode": 0, "stdout": "ok", "stderr": ""})())
        self.assertEqual([["go", "test", "./internal/transport/...", "./internal/app/..."]], calls)
        with self.assertRaises(integrity.IntegrityError):
            integrity.replay_sanitized_fixtures(self.root, {"version": 1, "packages": ["./private"]}, lambda *args, **kwargs: None)


if __name__ == "__main__":
    unittest.main()
