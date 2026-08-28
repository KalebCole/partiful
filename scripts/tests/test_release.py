#!/usr/bin/env python3
"""Behavioral tests for the credential-free release build and publication gate."""
from __future__ import annotations

import hashlib
import json
from pathlib import Path
import sys
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT))

from scripts import build_release, verify_release


class ReleaseBuildTest(unittest.TestCase):
    def test_archive_matrix_contains_both_binaries_for_all_five_supported_targets(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "release"
            build_release.build_release(
                output=output,
                version="v1.2.3",
                source_date_epoch=1_700_000_000,
                source_revision="a" * 40,
                runner=build_release.fixture_runner,
                smoke=lambda *_: None,
            )
            manifest = json.loads((output / "manifest.json").read_text())
            self.assertEqual(len(build_release.TARGETS), 5)
            self.assertEqual(manifest["version"], "v1.2.3")
            self.assertEqual(manifest["source_revision"], "a" * 40)
            self.assertEqual(
                sorted(item["target"] for item in manifest["targets"]),
                sorted(target.name for target in build_release.TARGETS),
            )
            for item in manifest["targets"]:
                self.assertEqual(item["binaries"], ["partiful", "partiful-mcp"])
                self.assertTrue((output / item["archive"]).is_file())
                self.assertRegex(item["sha256"], r"^[0-9a-f]{64}$")

    def test_two_builds_with_fixed_inputs_have_identical_release_hashes(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            first, second = root / "first", root / "second"
            for output in (first, second):
                build_release.build_release(
                    output=output,
                    version="v1.2.3",
                    source_date_epoch=1_700_000_000,
                    source_revision="b" * 40,
                    runner=build_release.fixture_runner,
                    smoke=lambda *_: None,
                )
            self.assertEqual(build_release.release_digest(first), build_release.release_digest(second))

    def test_reproducibility_check_rebuilds_and_compares_the_complete_release(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "release"
            build_release.build_release(
                output=output,
                version="v1.2.3",
                source_date_epoch=1_700_000_000,
                source_revision="d" * 40,
                runner=build_release.fixture_runner,
                smoke=lambda *_: None,
            )
            build_release.assert_reproducible(
                output=output,
                version="v1.2.3",
                source_date_epoch=1_700_000_000,
                source_revision="d" * 40,
                runner=build_release.fixture_runner,
                smoke=lambda *_: None,
            )

    def test_version_is_linker_injected_without_changing_contract_revisions(self) -> None:
        command = build_release.go_build_command(
            target=build_release.TARGETS[0], binary="partiful", output=Path("out"), version="v1.2.3"
        )
        self.assertIn("-X github.com/KalebCole/partiful/internal/version.CLIVersion=v1.2.3", " ".join(command))
        self.assertNotIn("CommandContractRevision", " ".join(command))
        self.assertNotIn("TransportContractRevision", " ".join(command))


class PublicationGateTest(unittest.TestCase):
    def test_release_verifier_runs_as_a_repository_script(self) -> None:
        completed = subprocess.run(
            [sys.executable, "scripts/verify_release.py", "--help"],
            cwd=ROOT,
            text=True,
            capture_output=True,
            check=False,
        )
        self.assertEqual(completed.returncode, 0, completed.stderr)

    def test_publication_gate_refuses_open_interoperability_gate_before_publish(self) -> None:
        packet = verify_release.PublicationPacket(
            target_evidence={target.name: True for target in build_release.TARGETS},
            revision_match=True,
            contract_integrity=True,
            worker_profiles=True,
            mcp_stdio_interop_gate="OPEN",
        )
        self.assertEqual(verify_release.publication_failures(packet), ["MCP16-STDIO-INTEROP is OPEN"])

    def test_publication_gate_requires_all_evidence_to_publish(self) -> None:
        packet = verify_release.PublicationPacket(
            target_evidence={build_release.TARGETS[0].name: True},
            revision_match=False,
            contract_integrity=False,
            worker_profiles=False,
            mcp_stdio_interop_gate="CLOSED",
        )
        self.assertEqual(
            verify_release.publication_failures(packet),
            [
                "missing target evidence: darwin-arm64, linux-amd64, linux-arm64, windows-amd64",
                "release revision mismatch",
                "contract-integrity check failed",
                "worker-profile check failed",
            ],
        )

    def test_checksum_manifest_matches_archives(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "release"
            build_release.build_release(
                output=output,
                version="v1.2.3",
                source_date_epoch=1_700_000_000,
                source_revision="c" * 40,
                runner=build_release.fixture_runner,
                smoke=lambda *_: None,
            )
            failures = verify_release.verify_release_directory(output)
            self.assertEqual(failures, [])
            checksum = hashlib.sha256((output / "checksums.txt").read_bytes()).hexdigest()
            self.assertRegex(checksum, r"^[0-9a-f]{64}$")

    def test_release_build_emits_a_deterministic_spdx_2_3_document_for_the_release(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = self._fixture_release(Path(temporary))
            sbom = json.loads((output / "sbom.spdx.json").read_text())
            self.assertEqual(sbom["spdxVersion"], "SPDX-2.3")
            self.assertEqual(sbom["dataLicense"], "CC0-1.0")
            self.assertEqual(sbom["SPDXID"], "SPDXRef-DOCUMENT")
            self.assertEqual(sbom["creationInfo"]["created"], "2023-11-14T22:13:20Z")
            self.assertEqual(sbom["creationInfo"]["creators"], ["Tool: partiful-release"])
            self.assertEqual(sbom["packages"][0]["SPDXID"], "SPDXRef-Package-partiful")
            self.assertEqual(verify_release.verify_release_directory(output), [])

    def test_release_verifier_rejects_a_malformed_or_semantically_mismatched_sbom(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = self._fixture_release(Path(temporary))
            sbom_path = output / "sbom.spdx.json"
            sbom = json.loads(sbom_path.read_text())
            del sbom["dataLicense"]
            sbom_path.write_text(json.dumps(sbom, sort_keys=True, separators=(",", ":")) + "\n")
            self.assertIn("invalid sbom", verify_release.verify_release_directory(output))
            sbom["dataLicense"] = "CC0-1.0"
            sbom["packages"][0]["versionInfo"] = "v9.9.9"
            sbom_path.write_text(json.dumps(sbom, sort_keys=True, separators=(",", ":")) + "\n")
            self.assertIn("sbom version mismatch", verify_release.verify_release_directory(output))

    def test_release_verifier_rejects_a_checksum_manifest_missing_a_required_archive(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = self._fixture_release(Path(temporary))
            checksums = (output / "checksums.txt").read_text().splitlines()
            (output / "checksums.txt").write_text(
                "\n".join(line for line in checksums if "darwin-amd64" not in line) + "\n"
            )
            self.assertIn("checksum manifest artifact set mismatch", verify_release.verify_release_directory(output))

    def test_release_verifier_rejects_duplicate_checksum_entries(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = self._fixture_release(Path(temporary))
            checksums = (output / "checksums.txt").read_text()
            (output / "checksums.txt").write_text(checksums + checksums.splitlines()[0] + "\n")
            self.assertIn("invalid checksum manifest", verify_release.verify_release_directory(output))

    def test_release_verifier_rejects_malformed_checksum_entries(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = self._fixture_release(Path(temporary))
            (output / "checksums.txt").write_text("malformed\n")
            self.assertIn("invalid checksum manifest", verify_release.verify_release_directory(output))

    def test_release_verifier_rejects_unexpected_checksum_entries(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = self._fixture_release(Path(temporary))
            checksums = (output / "checksums.txt").read_text()
            (output / "checksums.txt").write_text(checksums + ("0" * 64) + "  unexpected.txt\n")
            self.assertIn("checksum manifest artifact set mismatch", verify_release.verify_release_directory(output))

    def test_release_verifier_rejects_path_escaping_checksum_entries(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = self._fixture_release(Path(temporary))
            checksums = (output / "checksums.txt").read_text().replace("  manifest.json", "  ../manifest.json")
            (output / "checksums.txt").write_text(checksums)
            self.assertIn("invalid checksum manifest", verify_release.verify_release_directory(output))

    def test_release_workflow_checks_out_the_selected_tag_commit_before_verification(self) -> None:
        workflow = (ROOT / ".github/workflows/release.yml").read_text()
        selection = workflow.index("- name: Select immutable release inputs")
        checkout = workflow.index("- name: Check out selected release commit")
        verification = workflow.index("- name: Verify contract integrity")
        self.assertLess(selection, checkout)
        self.assertLess(checkout, verification)
        self.assertIn("ref: ${{ steps.release.outputs.revision }}", workflow[checkout:verification])
        self.assertIn('test "$(git rev-parse HEAD)" = "${{ steps.release.outputs.revision }}"', workflow[checkout:verification])

    def _fixture_release(self, root: Path) -> Path:
        output = root / "release"
        build_release.build_release(
            output=output,
            version="v1.2.3",
            source_date_epoch=1_700_000_000,
            source_revision="e" * 40,
            runner=build_release.fixture_runner,
            smoke=lambda *_: None,
        )
        return output


if __name__ == "__main__":
    unittest.main()
