from __future__ import annotations

import hashlib
import json
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from scripts import watch_public_contract as watch


OPERATIONS = ["readThing", "writeThing"]


def disabled_registry() -> dict:
    entries = [
        watch.disabled_probe("DRIFT17-FIREBASE-PROBE", "firebase-public-configuration"),
        watch.disabled_probe("DRIFT17-ASSET-DISCOVERY", "public-asset-discovery"),
    ]
    entries.extend(watch.disabled_probe(f"DRIFT17-PROTECTED-CONTRACT:{operation}", "protected-contract") for operation in OPERATIONS)
    return {"version": 1, "contract_revision": "r1", "probes": entries}


class DriftWatchTests(unittest.TestCase):
    def test_registry_requires_exact_disabled_gate_inventory(self) -> None:
        registry = disabled_registry()
        self.assertEqual(4, watch.validate_probe_registry(registry, OPERATIONS))
        registry["probes"].pop()
        with self.assertRaisesRegex(watch.RegistryError, "missing"):
            watch.validate_probe_registry(registry, OPERATIONS)

    def test_unknown_registry_entry_sends_zero_requests(self) -> None:
        registry = disabled_registry()
        registry["probes"].append(watch.disabled_probe("UNKNOWN", "protected-contract"))
        calls = []
        reports = watch.run_probes(registry, OPERATIONS, lambda probe: calls.append(probe))
        self.assertEqual([], calls)
        self.assertEqual("inconclusive", reports[0]["classification"])
        self.assertEqual("invalid_probe_registry", reports[0]["reason"])

    def test_gate_identity_kind_mismatch_sends_zero_requests(self) -> None:
        mismatched_kinds = {
            "DRIFT17-FIREBASE-PROBE": "public-asset-discovery",
            "DRIFT17-ASSET-DISCOVERY": "firebase-public-configuration",
            "DRIFT17-PROTECTED-CONTRACT:readThing": "firebase-public-configuration",
        }
        for gate_id, mismatched_kind in mismatched_kinds.items():
            with self.subTest(gate_id=gate_id):
                registry = disabled_registry()
                probe = next(item for item in registry["probes"] if item["gate_id"] == gate_id)
                probe["kind"] = mismatched_kind
                probe.update({
                    "enabled": True,
                    "method": "GET",
                    "target_host_class": "public-endpoint",
                    "request_template_sha256": "a" * 64,
                    "expected": {
                        "statuses": [200], "codes": [], "configuration_rejection_codes": [],
                        "shape_sha256": "b" * 64,
                    },
                    "limits": {
                        "requests": 1, "request_bytes": 0, "response_bytes": 1024,
                        "timeout_seconds": 2, "redirects": 0,
                    },
                })
                calls = []

                def requester(candidate: dict) -> dict:
                    calls.append(candidate)
                    return {}

                reports = watch.run_probes(registry, OPERATIONS, requester)

                self.assertEqual([], calls)
                self.assertEqual("invalid_probe_registry", reports[0]["reason"])

    def test_disabled_probes_send_zero_requests_and_are_inconclusive(self) -> None:
        calls = []
        reports = watch.run_probes(disabled_registry(), OPERATIONS, lambda probe: calls.append(probe))
        self.assertEqual([], calls)
        self.assertTrue(all(report["classification"] == "inconclusive" for report in reports))
        self.assertTrue(all(report["reason"] == "probe_disabled" for report in reports))

    def test_enabled_probe_obeys_classifier_without_retaining_body(self) -> None:
        registry = disabled_registry()
        probe = registry["probes"][0]
        probe.update({
            "enabled": True,
            "method": "POST",
            "target_host_class": "firebase-identity-toolkit",
            "request_template_sha256": "a" * 64,
            "expected": {"statuses": [400], "codes": ["INVALID_CUSTOM_TOKEN"], "configuration_rejection_codes": ["API_KEY_INVALID"], "shape_sha256": "b" * 64},
            "limits": {"requests": 1, "request_bytes": 256, "response_bytes": 1024, "timeout_seconds": 2, "redirects": 0},
        })
        response = {"status": 400, "code": "INVALID_CUSTOM_TOKEN", "shape_sha256": "b" * 64, "response_bytes": 80, "duration_ms": 12, "body": "must not leak"}
        reports = watch.run_probes(registry, OPERATIONS, lambda _: response)
        report = next(item for item in reports if item["gate_id"] == "DRIFT17-FIREBASE-PROBE")
        self.assertEqual("pass", report["classification"])
        self.assertNotIn("body", json.dumps(report).lower())

    def test_known_status_with_changed_shape_is_contract_drift_but_unknown_status_is_inconclusive(self) -> None:
        registry = disabled_registry()
        probe = registry["probes"][0]
        probe.update({
            "enabled": True, "method": "GET", "target_host_class": "partiful-callable",
            "request_template_sha256": "a" * 64,
            "expected": {"statuses": [200], "codes": [], "configuration_rejection_codes": ["API_KEY_INVALID"], "shape_sha256": "b" * 64},
            "limits": {"requests": 1, "request_bytes": 0, "response_bytes": 1024, "timeout_seconds": 2, "redirects": 0},
        })
        changed = watch.run_probes(registry, OPERATIONS, lambda _: {"status": 200, "code": None, "shape_sha256": "c" * 64, "response_bytes": 40, "duration_ms": 1})
        self.assertEqual("confirmed_contract_drift", next(x for x in changed if x["gate_id"] == "DRIFT17-FIREBASE-PROBE")["classification"])
        unknown = watch.run_probes(registry, OPERATIONS, lambda _: {"status": 503, "code": None, "shape_sha256": "c" * 64, "response_bytes": 40, "duration_ms": 1})
        self.assertEqual("inconclusive", next(x for x in unknown if x["gate_id"] == "DRIFT17-FIREBASE-PROBE")["classification"])

    def test_exact_compiled_configuration_rejection_is_confirmed_incompatibility(self) -> None:
        registry = disabled_registry()
        registry["probes"][0].update({
            "enabled": True, "method": "POST", "target_host_class": "firebase-identity-toolkit",
            "request_template_sha256": "a" * 64,
            "expected": {"statuses": [400], "codes": ["INVALID_CUSTOM_TOKEN"], "configuration_rejection_codes": ["API_KEY_INVALID"], "shape_sha256": "b" * 64},
            "limits": {"requests": 1, "request_bytes": 256, "response_bytes": 1024, "timeout_seconds": 2, "redirects": 0},
        })
        reports = watch.run_probes(registry, OPERATIONS, lambda _: {
            "status": 400, "code": "API_KEY_INVALID", "shape_sha256": "d" * 64,
            "response_bytes": 60, "duration_ms": 1,
        })
        report = next(item for item in reports if item["gate_id"] == "DRIFT17-FIREBASE-PROBE")
        self.assertEqual("confirmed_public_configuration_incompatibility", report["classification"])
        self.assertEqual("accepted_compiled_configuration_rejection", report["reason"])

    def test_asset_discovery_uses_local_http_fixture_with_same_origin_and_safe_manifest(self) -> None:
        api_key = "A" * 32
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self) -> None:
                if self.path == "/":
                    body = b'<script src="/app.js?build=private"></script><script src="https://other.invalid/x.js"></script>'
                elif self.path == "/app.js?build=private":
                    body = f'firebaseConfig={{apiKey:"{api_key}",projectId:"expected-project"}}'.encode()
                else:
                    self.send_response(404); self.end_headers(); return
                self.send_response(200); self.send_header("Content-Length", str(len(body))); self.end_headers(); self.wfile.write(body)
            def log_message(self, *args: object) -> None:
                return
        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True); thread.start()
        try:
            seed = f"http://127.0.0.1:{server.server_port}/"
            policy = {
                "enabled": True, "seed_urls": [seed], "methods": ["GET"], "same_origin": True,
                "max_redirects": 0, "max_assets": 2, "max_response_bytes": 2048, "timeout_seconds": 2,
                "extractor_version": "test-1", "candidate_pattern": r'apiKey:\"([A-Z]{32})\"',
                "project_pattern": r'projectId:\"([^\"]+)\"', "expected_project": "expected-project",
                "allow_test_http_loopback": True,
            }
            manifest = watch.discover_public_assets(policy, observed_at="2026-08-28T00:00:00Z")
        finally:
            server.shutdown(); server.server_close(); thread.join()
        encoded = json.dumps(manifest, sort_keys=True)
        self.assertEqual(1, manifest["candidate_count"])
        self.assertTrue(manifest["expected_project_match"])
        self.assertNotIn(api_key, encoded)
        self.assertNotIn("?", encoded)
        self.assertNotIn("other.invalid", encoded)
        self.assertEqual(hashlib.sha256(api_key.encode()).hexdigest()[:16], manifest["candidate_fingerprints"][0])

    def test_asset_limits_fail_inconclusively_without_raw_content(self) -> None:
        policy = {"enabled": True, "seed_urls": ["https://partiful.example/"], "methods": ["GET"], "same_origin": True,
                  "max_redirects": 0, "max_assets": 1, "max_response_bytes": 4, "timeout_seconds": 1,
                  "extractor_version": "v1", "candidate_pattern": "(secret)", "project_pattern": "(project)", "expected_project": "project"}
        manifest = watch.discover_public_assets(policy, fetcher=lambda url, **_: {"url": url, "status": 200, "body": b"too large"}, observed_at="2026-08-28T00:00:00Z")
        self.assertEqual("inconclusive", manifest["classification"])
        self.assertEqual("response_limit", manifest["reason"])
        self.assertNotIn("too large", json.dumps(manifest))

    def test_asset_redirect_is_inconclusive_policy_violation(self) -> None:
        policy = {"enabled": True, "seed_urls": ["https://partiful.example/"], "methods": ["GET"], "same_origin": True,
                  "max_redirects": 0, "max_assets": 1, "max_response_bytes": 100, "timeout_seconds": 1,
                  "extractor_version": "v1", "candidate_pattern": "(candidate)", "project_pattern": "(project)", "expected_project": "project"}
        manifest = watch.discover_public_assets(policy, fetcher=lambda url, **_: {"url": url, "status": 302, "body": b"redirect"}, observed_at="2026-08-28T00:00:00Z")
        self.assertEqual("inconclusive", manifest["classification"])
        self.assertEqual("redirect_policy", manifest["reason"])

    def test_reports_are_deduplicated_by_gate_classification_and_semantic_location(self) -> None:
        reports = [
            {"gate_id": "G", "classification": "inconclusive", "reason": "timeout", "observed_at": "one"},
            {"gate_id": "G", "classification": "inconclusive", "reason": "timeout", "observed_at": "two"},
            {"gate_id": "G", "classification": "confirmed_contract_drift", "semantic_location": "#/x"},
        ]
        deduped = watch.deduplicate_reports(reports)
        self.assertEqual(2, len(deduped))
        self.assertEqual(2, deduped[0]["occurrences"])


if __name__ == "__main__":
    unittest.main()
