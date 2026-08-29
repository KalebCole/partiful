#!/usr/bin/env python3
"""Behavioral tests for the credential-free release build and publication gate."""
from __future__ import annotations

import hashlib
import io
import json
from pathlib import Path
import signal
import sys
import subprocess
import tarfile
import tempfile
import unittest
import warnings
import zipfile

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT))

from scripts import build_release, verify_release


class ReleaseBuildTest(unittest.TestCase):
    def test_release_uses_semantic_tagged_names_and_binds_all_version_fields_per_target(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "release"
            metadata = build_release.build_release(
                output=output,
                version="v1.2.3",
                source_date_epoch=1_700_000_000,
                source_revision="f" * 40,
                runner=build_release.fixture_runner,
                smoke=lambda *_: None,
            )
            fields = {
                "cli_version": "v1.2.3",
                "command_contract_revision": "1",
                "transport_contract_revision": "2026-08-12.7",
            }
            self.assertEqual(metadata["release_fields"], fields)
            self.assertTrue((output / "SHA256SUMS").is_file())
            self.assertFalse((output / "checksums.txt").exists())
            for target in build_release.TARGETS:
                extension = "zip" if target.goos == "windows" else "tar.gz"
                self.assertTrue((output / f"partiful_v1.2.3_{target.goos}_{target.goarch}.{extension}").is_file())
            self.assertTrue(all(item["release_fields"] == fields for item in metadata["targets"]))
            self.assertEqual(verify_release.verify_release_directory(output), [])
        for version in ("vbanana", "v1", "v1.2", "1.2.3"):
            with self.assertRaises(ValueError):
                build_release.build_release(
                    output=Path(tempfile.gettempdir()) / "invalid-release-version",
                    version=version,
                    source_date_epoch=1_700_000_000,
                    source_revision="f" * 40,
                    runner=build_release.fixture_runner,
                )

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
                target = next(target for target in build_release.TARGETS if target.name == item["target"])
                suffix = ".exe" if target.goos == "windows" else ""
                self.assertEqual(item["binaries"], [f"partiful{suffix}", f"partiful-mcp{suffix}"])
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
    def test_mcp_version_response_requires_exact_release_manifest_fields(self) -> None:
        fields = {
            "cli_version": "v1.2.3",
            "command_contract_revision": "1",
            "transport_contract_revision": "2026-08-12.7",
        }
        response = {"jsonrpc": "2.0", "id": 3, "result": {"structuredContent": fields}}
        self.assertEqual(verify_release.mcp_version_response_failures(response, fields), [])
        for name, value in fields.items():
            with self.subTest(case=f"mismatched-{name}"):
                mismatched = fields | {name: f"unexpected-{value}"}
                response = {"jsonrpc": "2.0", "id": 3, "result": {"structuredContent": mismatched}}
                self.assertTrue(verify_release.mcp_version_response_failures(response, fields))
            with self.subTest(case=f"missing-{name}"):
                missing = fields.copy()
                del missing[name]
                response = {"jsonrpc": "2.0", "id": 3, "result": {"structuredContent": missing}}
                self.assertTrue(verify_release.mcp_version_response_failures(response, fields))
        with self.subTest(case="extra-field"):
            response = {"jsonrpc": "2.0", "id": 3, "result": {"structuredContent": fields | {"unexpected": "value"}}}
            self.assertTrue(verify_release.mcp_version_response_failures(response, fields))

    def test_mcp_shutdown_drains_pipes_for_eof_and_signals(self) -> None:
        shutdowns = (("EOF", None), ("SIGINT", signal.SIGINT), ("SIGTERM", signal.SIGTERM))
        for name, shutdown_signal in shutdowns:
            with self.subTest(shutdown=name, case="clean"):
                process = _FakeMCPProcess()
                stdin = process.stdin
                self.assertEqual(verify_release.mcp_shutdown_failures(process, shutdown_signal), [])
                self.assertTrue(process.communicated)
                self.assertEqual(process.signals, [] if shutdown_signal is None else [shutdown_signal])
                self.assertEqual(stdin.closed, shutdown_signal is None)
                self.assertTrue(process.stdout.read_called)
                self.assertTrue(process.stderr.read_called)
            for stream, output in (("stdout", "unexpected protocol frame\n"), ("stderr", "diagnostic\n")):
                with self.subTest(shutdown=name, case=f"unexpected-{stream}"):
                    process = _FakeMCPProcess(**{stream: output})
                    self.assertTrue(verify_release.mcp_shutdown_failures(process, shutdown_signal))
                    self.assertTrue(process.stdout.read_called)
                    self.assertTrue(process.stderr.read_called)

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
            worker_profile_attested=True,
            mcp_stdio_interop_gate="OPEN",
        )
        self.assertEqual(verify_release.publication_failures(packet), ["MCP16-STDIO-INTEROP is OPEN"])

    def test_publication_gate_requires_all_evidence_to_publish(self) -> None:
        packet = verify_release.PublicationPacket(
            target_evidence={build_release.TARGETS[0].name: True},
            revision_match=False,
            contract_integrity=False,
            worker_profile_attested=False,
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

    def test_worker_profile_status_requires_one_exact_success_for_the_candidate_sha(self) -> None:
        revision = "a" * 40
        def payload(statuses: list[dict[str, str]], sha: str = revision) -> dict[str, object]:
            return {"sha": sha, "statuses": statuses}

        expected = {"context": "partiful/live-worker-profiles", "state": "success"}
        self.assertEqual(verify_release.worker_profile_status_failures(payload([expected]), revision), [])
        cases = {
            "absent": (payload([]), "worker-profile status is missing"),
            "pending": (payload([{**expected, "state": "pending"}]), "worker-profile status is pending"),
            "error": (payload([{**expected, "state": "error"}]), "worker-profile status is error"),
            "failure": (payload([{**expected, "state": "failure"}]), "worker-profile status is failure"),
            "wrong-context": (payload([{**expected, "context": "partiful/other"}]), "worker-profile status is missing"),
            "wrong-sha": (payload([expected], "b" * 40), "worker-profile status SHA mismatch"),
            "duplicate": (payload([expected, expected]), "worker-profile status is ambiguous"),
            "malformed": ({"sha": revision, "statuses": "success"}, "invalid worker-profile status"),
        }
        for name, (status_payload, failure) in cases.items():
            with self.subTest(case=name):
                self.assertIn(failure, verify_release.worker_profile_status_failures(status_payload, revision))

    def test_release_workflow_queries_native_status_for_the_build_revision_and_never_runs_live_profiles_hosted(self) -> None:
        workflow = (ROOT / ".github/workflows/release.yml").read_text()
        build = workflow[workflow.index("build:"):workflow.index("native-smoke:")]
        publish = workflow[workflow.index("publish:"):]
        self.assertNotIn("verify_implementation_worker_profiles.py", build)
        self.assertIn("REVISION: ${{ needs.build.outputs.revision }}", publish)
        self.assertIn('"repos/${GITHUB_REPOSITORY}/commits/${REVISION}/status?per_page=100"', publish)
        self.assertIn("--worker-profile-status-file", publish)
        self.assertNotIn("--worker-profiles-passed", workflow)

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
            checksum = hashlib.sha256((output / "SHA256SUMS").read_bytes()).hexdigest()
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

    def test_release_verifier_rejects_checksum_valid_sboms_missing_required_fields(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            for path in (("name",), ("packages", 0, "licenseConcluded"), ("relationships",)):
                with self.subTest(path=path):
                    output = self._fixture_release(Path(temporary))
                    sbom_path = output / "sbom.spdx.json"
                    sbom = json.loads(sbom_path.read_text())
                    item = sbom
                    for key in path[:-1]:
                        item = item[key]
                    del item[path[-1]]
                    sbom_path.write_text(json.dumps(sbom, sort_keys=True, separators=(",", ":")) + "\n")
                    checksum_path = output / "SHA256SUMS"
                    checksums = checksum_path.read_text().splitlines()
                    checksum_path.write_text("\n".join(
                        f"{hashlib.sha256(sbom_path.read_bytes()).hexdigest()}  sbom.spdx.json"
                        if line.endswith("  sbom.spdx.json") else line
                        for line in checksums
                    ) + "\n")
                    self.assertIn("invalid sbom", verify_release.verify_release_directory(output))

    def test_release_verifier_rejects_a_checksum_manifest_missing_a_required_archive(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = self._fixture_release(Path(temporary))
            checksums = (output / "SHA256SUMS").read_text().splitlines()
            (output / "SHA256SUMS").write_text(
                "\n".join(line for line in checksums if "darwin_amd64" not in line) + "\n"
            )
            self.assertIn("checksum manifest artifact set mismatch", verify_release.verify_release_directory(output))

    def test_release_verifier_rejects_duplicate_checksum_entries(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = self._fixture_release(Path(temporary))
            checksums = (output / "SHA256SUMS").read_text()
            (output / "SHA256SUMS").write_text(checksums + checksums.splitlines()[0] + "\n")
            self.assertIn("invalid checksum manifest", verify_release.verify_release_directory(output))

    def test_release_verifier_rejects_malformed_checksum_entries(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = self._fixture_release(Path(temporary))
            (output / "SHA256SUMS").write_text("malformed\n")
            self.assertIn("invalid checksum manifest", verify_release.verify_release_directory(output))

    def test_release_verifier_rejects_unexpected_checksum_entries(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = self._fixture_release(Path(temporary))
            checksums = (output / "SHA256SUMS").read_text()
            (output / "SHA256SUMS").write_text(checksums + ("0" * 64) + "  unexpected.txt\n")
            self.assertIn("checksum manifest artifact set mismatch", verify_release.verify_release_directory(output))

    def test_release_verifier_rejects_path_escaping_checksum_entries(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = self._fixture_release(Path(temporary))
            checksums = (output / "SHA256SUMS").read_text().replace("  manifest.json", "  ../manifest.json")
            (output / "SHA256SUMS").write_text(checksums)
            self.assertIn("invalid checksum manifest", verify_release.verify_release_directory(output))

    def test_release_verifier_rejects_archives_with_invalid_binary_members(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            for target in (build_release.TARGETS[0], build_release.TARGETS[-1]):
                suffix = ".exe" if target.goos == "windows" else ""
                expected = [f"partiful{suffix}", f"partiful-mcp{suffix}"]
                target_cases = {
                    "missing": expected[:1],
                    "extra": [*expected, "unexpected"],
                    "duplicate": [*expected, expected[0]],
                    "path-escaping": [*expected, "../partiful"],
                }
                for name, members in target_cases.items():
                    with self.subTest(target=target.name, case=name):
                        output = self._fixture_release(Path(temporary))
                        self._replace_archive_members(output, target, members)
                        self.assertTrue(
                            any(failure.startswith("invalid archive members") for failure in verify_release.verify_release_directory(output))
                        )

    def test_release_verifier_requires_regular_members_and_an_exact_canonical_target_matrix(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            for target in (build_release.TARGETS[0], build_release.TARGETS[-1]):
                with self.subTest(target=target.name, case="symlink"):
                    output = self._fixture_release(Path(temporary))
                    self._replace_archive_members(output, target, build_release.archive_binary_names(target), member_type="symlink")
                    self.assertTrue(any(failure.startswith("invalid archive members") for failure in verify_release.verify_release_directory(output)))
            for case, change in (("cross-target-alias", self._alias_target_archive), ("duplicate-target", self._duplicate_target), ("unknown-target", self._add_unknown_target)):
                with self.subTest(case=case):
                    output = self._fixture_release(Path(temporary))
                    change(output)
                    self.assertTrue(any(failure.startswith("invalid target evidence") for failure in verify_release.verify_release_directory(output)))

    def test_release_verifier_rejects_nonexecutable_regular_archive_members(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            for target in (build_release.TARGETS[0], build_release.TARGETS[-1]):
                with self.subTest(target=target.name):
                    output = self._fixture_release(Path(temporary))
                    self._replace_archive_members(output, target, build_release.archive_binary_names(target), executable=False)
                    self.assertTrue(any(failure.startswith("invalid archive members") for failure in verify_release.verify_release_directory(output)))

    def test_release_workflow_checks_out_the_selected_tag_commit_before_verification(self) -> None:
        workflow = (ROOT / ".github/workflows/release.yml").read_text()
        selection = workflow.index("- name: Select immutable release inputs")
        checkout = workflow.index("- name: Check out selected release commit")
        verification = workflow.index("- name: Verify contract integrity")
        self.assertLess(selection, checkout)
        self.assertLess(checkout, verification)
        self.assertIn("ref: ${{ steps.release.outputs.revision }}", workflow[checkout:verification])
        self.assertIn('test "$(git rev-parse HEAD)" = "${{ steps.release.outputs.revision }}"', workflow[checkout:verification])

    def test_release_workflow_smokes_exact_archives_on_each_native_target_before_publish(self) -> None:
        workflow = (ROOT / ".github/workflows/release.yml").read_text()
        native_smoke = workflow.index("native-smoke:")
        publish = workflow.index("publish:")
        self.assertLess(native_smoke, publish)
        section = workflow[native_smoke:publish]
        self.assertIn("runs-on: ${{ matrix.runner }}", section)
        self.assertIn("archive: partiful_${{ needs.build.outputs.version }}", section)
        self.assertIn("ref: ${{ needs.build.outputs.revision }}", section)
        self.assertIn("from scripts.verify_release import smoke_native_archive", section)
        self.assertIn('smoke_native_archive(Path("release") / os.environ["ARCHIVE"], Path("release") / "manifest.json")', section)

    def test_release_workflow_uses_the_executable_native_smoke_once(self) -> None:
        workflow = (ROOT / ".github/workflows/release.yml").read_text()
        section = workflow[workflow.index("native-smoke:"):workflow.index("publish:")]
        self.assertEqual(section.count("smoke_native_archive("), 1)

    def test_release_verifier_requires_source_metadata_and_toolchain_evidence(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            for key, failure in (("source_date_epoch", "invalid source metadata"), ("toolchain", "invalid toolchain metadata")):
                with self.subTest(key=key):
                    output = self._fixture_release(Path(temporary))
                    manifest_path = output / "manifest.json"
                    manifest = json.loads(manifest_path.read_text())
                    del manifest[key]
                    manifest_path.write_text(json.dumps(manifest, sort_keys=True, separators=(",", ":")) + "\n")
                    self._refresh_checksums(output)
                    self.assertIn(failure, verify_release.verify_release_directory(output))

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

    def _replace_archive_members(self, output: Path, target: build_release.Target, members: list[str], member_type: str = "file", executable: bool = True) -> None:
        manifest_path = output / "manifest.json"
        manifest = json.loads(manifest_path.read_text())
        item = next(item for item in manifest["targets"] if item["target"] == target.name)
        archive = output / item["archive"]
        if target.archive_format == "tar.gz":
            with tarfile.open(archive, "w:gz") as rewritten:
                for member in members:
                    payload = member.encode()
                    info = tarfile.TarInfo(member)
                    if member_type == "symlink":
                        info.type = tarfile.SYMTYPE
                        info.linkname = "elsewhere"
                        rewritten.addfile(info)
                    else:
                        info.size = len(payload)
                        info.mode = 0o755 if executable else 0o644
                        rewritten.addfile(info, io.BytesIO(payload))
        else:
            with warnings.catch_warnings():
                warnings.simplefilter("ignore", UserWarning)
                with zipfile.ZipFile(archive, "w") as rewritten:
                    for member in members:
                        info = zipfile.ZipInfo(member)
                        if member_type == "symlink":
                            info.create_system = 3
                            info.external_attr = 0o120777 << 16
                        else:
                            info.external_attr = (0o100755 if executable else 0o100644) << 16
                        rewritten.writestr(info, member)
        item["sha256"] = hashlib.sha256(archive.read_bytes()).hexdigest()
        manifest_path.write_text(json.dumps(manifest, sort_keys=True, separators=(",", ":")) + "\n")
        self._refresh_checksums(output)

    def _alias_target_archive(self, output: Path) -> None:
        manifest_path = output / "manifest.json"
        manifest = json.loads(manifest_path.read_text())
        manifest["targets"][2]["archive"] = manifest["targets"][0]["archive"]
        manifest["targets"][2]["sha256"] = manifest["targets"][0]["sha256"]
        manifest_path.write_text(json.dumps(manifest, sort_keys=True, separators=(",", ":")) + "\n")
        self._refresh_checksums(output)

    def _duplicate_target(self, output: Path) -> None:
        manifest_path = output / "manifest.json"
        manifest = json.loads(manifest_path.read_text())
        manifest["targets"].append(manifest["targets"][0].copy())
        manifest_path.write_text(json.dumps(manifest, sort_keys=True, separators=(",", ":")) + "\n")
        self._refresh_checksums(output)

    def _add_unknown_target(self, output: Path) -> None:
        manifest_path = output / "manifest.json"
        manifest = json.loads(manifest_path.read_text())
        manifest["targets"].append({"target": "unknown", "archive": "unknown.tar.gz", "binaries": [], "sha256": "0" * 64})
        manifest_path.write_text(json.dumps(manifest, sort_keys=True, separators=(",", ":")) + "\n")
        self._refresh_checksums(output)

    def _refresh_checksums(self, output: Path) -> None:
        hashes = {path.name: hashlib.sha256(path.read_bytes()).hexdigest() for path in output.iterdir() if path.is_file() and path.name != "SHA256SUMS"}
        (output / "SHA256SUMS").write_text("\n".join(
            f"{hashes.get(line.split('  ', 1)[1], line.split('  ', 1)[0])}  {line.split('  ', 1)[1]}"
            for line in (output / "SHA256SUMS").read_text().splitlines()
        ) + "\n")


class _FakePipe:
    def __init__(self, output: str = "") -> None:
        self.closed = False
        self.output = output
        self.read_called = False

    def close(self) -> None:
        self.closed = True

    def read(self) -> str:
        self.read_called = True
        return self.output


class _FakeMCPProcess:
    def __init__(self, stdout: str = "", stderr: str = "") -> None:
        self.stdin = _FakePipe()
        self.stdout = _FakePipe(stdout)
        self.stderr = _FakePipe(stderr)
        self.returncode = 0
        self.signals: list[signal.Signals] = []
        self.communicated = False

    def send_signal(self, shutdown_signal: signal.Signals) -> None:
        self.signals.append(shutdown_signal)

    def communicate(self, timeout: float) -> tuple[str, str]:
        self.communicated = True
        return self.stdout.read(), self.stderr.read()


if __name__ == "__main__":
    unittest.main()
