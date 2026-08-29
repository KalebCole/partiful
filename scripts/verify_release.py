#!/usr/bin/env python3
"""Verify release artifacts and fail closed before GitHub Release publication."""
from __future__ import annotations

import argparse
import datetime
import hashlib
import json
import os
import re
import signal
import stat
import subprocess
import tarfile
from dataclasses import dataclass
from pathlib import Path
import sys
import tempfile
import zipfile

if __package__ in {None, ""}:
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from scripts.build_release import TARGETS, archive_name

RELEASE_FIELD_NAMES = {"cli_version", "command_contract_revision", "transport_contract_revision"}


def assert_release_fields(actual: object, expected: object, source: str) -> None:
    """Require one exact, non-empty public release-field tuple."""
    if (
        not isinstance(actual, dict)
        or set(actual) != RELEASE_FIELD_NAMES
        or not all(isinstance(value, str) and value for value in actual.values())
        or actual != expected
    ):
        raise RuntimeError(f"{source} fields do not match release metadata")


def mcp_version_response_failures(response: object, expected_fields: object) -> list[str]:
    """Return failures for one MCP version tool response."""
    if not isinstance(response, dict):
        return ["invalid MCP version response"]
    result = response.get("result")
    content = result.get("structuredContent") if isinstance(result, dict) else None
    try:
        assert_release_fields(content, expected_fields, "candidate MCP")
    except RuntimeError as error:
        return [str(error)]
    return []


def mcp_shutdown_failures(process: subprocess.Popen[str], stop_signal: int | None) -> list[str]:
    """Stop an MCP process, drain both streams, and return shutdown failures."""
    if stop_signal is None:
        if process.stdin is None:
            return ["MCP shutdown has no stdin"]
        process.stdin.close()
        process.stdin = None
    else:
        process.send_signal(stop_signal)
    stdout, stderr = process.communicate(timeout=10)
    failures = []
    if process.returncode != 0:
        failures.append("MCP shutdown returned a failure status")
    if stdout:
        failures.append("MCP shutdown wrote unexpected stdout")
    if stderr:
        failures.append("MCP shutdown wrote unexpected stderr")
    return failures


def drain_clean_shutdown(process: subprocess.Popen[str], label: str, stop_signal: int | None = None) -> None:
    """Stop an MCP process, drain both streams, and reject all extra output."""
    failures = mcp_shutdown_failures(process, stop_signal)
    if failures:
        raise RuntimeError(f"MCP {label} shutdown failed: {', '.join(failures)}")


def smoke_native_archive(archive: Path, manifest_path: Path) -> None:
    """Exercise the exact native CLI and MCP binaries from one release archive."""
    manifest = json.loads(manifest_path.read_text())
    expected_fields = manifest.get("release_fields")
    assert_release_fields(expected_fields, expected_fields, "manifest")
    with tempfile.TemporaryDirectory(prefix="partiful-native-smoke-") as raw:
        root = Path(raw)
        if archive.suffix == ".zip":
            with zipfile.ZipFile(archive) as package:
                package.extractall(root)
            suffix = ".exe"
        else:
            with tarfile.open(archive, "r:gz") as package:
                package.extractall(root)
            suffix = ""
        cli, mcp = root / f"partiful{suffix}", root / f"partiful-mcp{suffix}"
        environment = os.environ.copy()
        environment["HOME"] = str(root / "home")
        environment["XDG_DATA_HOME"] = str(root / "data")
        environment["LOCALAPPDATA"] = str(root / "data")

        version = subprocess.run(
            [str(cli), "--version", "--json"], env=environment, text=True, capture_output=True, check=False
        )
        if version.returncode != 0 or version.stderr:
            raise RuntimeError("candidate CLI version failed")
        assert_release_fields(json.loads(version.stdout), expected_fields, "candidate CLI")
        help_result = subprocess.run([str(cli), "--help"], env=environment, text=True, capture_output=True, check=False)
        if help_result.returncode != 0 or help_result.stderr or "Usage: partiful <command> [flags]" not in help_result.stdout:
            raise RuntimeError("candidate CLI help failed")

        flags = subprocess.CREATE_NEW_PROCESS_GROUP if os.name == "nt" else 0

        def launch() -> subprocess.Popen[str]:
            return subprocess.Popen(
                [str(mcp)], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                env=environment, text=True, creationflags=flags,
            )

        def request(process: subprocess.Popen[str], identifier: int, method: str, params: dict[str, object]) -> dict[str, object]:
            if process.stdin is None or process.stdout is None:
                raise RuntimeError("MCP process streams are unavailable")
            process.stdin.write(json.dumps({"jsonrpc": "2.0", "id": identifier, "method": method, "params": params}, separators=(",", ":")) + "\n")
            process.stdin.flush()
            response = json.loads(process.stdout.readline())
            if response.get("id") != identifier or response.get("jsonrpc") != "2.0":
                raise RuntimeError("invalid MCP response")
            return response

        def initialize(process: subprocess.Popen[str], identifier: int) -> None:
            response = request(process, identifier, "initialize", {
                "protocolVersion": "2025-06-18", "capabilities": {},
                "clientInfo": {"name": "release-smoke", "version": "1"},
            })
            server_info = response.get("result", {})
            server_info = server_info.get("serverInfo", {}) if isinstance(server_info, dict) else {}
            if server_info.get("name") != "partiful" or server_info.get("version") != expected_fields["cli_version"]:
                raise RuntimeError("MCP initialization version mismatch")

        expected_tools = {
            "auth_status", "auth_logout", "events_list", "events_get", "events_create", "events_update", "events_cancel",
            "guests_list", "guests_invite", "rsvp_get", "rsvp_set", "contacts_list", "cohosts_invite",
            "cohosts_revoke_invite", "cohosts_remove", "cohosts_link_create", "cohosts_link_revoke", "blasts_send",
            "posters_list", "posters_search", "schema", "doctor", "version",
        }
        process = launch()
        initialize(process, 1)
        listed = request(process, 2, "tools/list", {})
        listed_result = listed.get("result", {})
        tools = {tool.get("name") for tool in listed_result.get("tools", [])} if isinstance(listed_result, dict) else set()
        if len(tools) != 23 or tools != expected_tools:
            raise RuntimeError("MCP tool inventory mismatch")
        protected = request(process, 3, "tools/call", {
            "name": "events_cancel", "arguments": {"event_id": "release-smoke", "notify_guests": False},
        })
        protected_result = protected.get("result", {})
        protected_content = protected_result.get("structuredContent", {}) if isinstance(protected_result, dict) else {}
        if protected_content.get("error", {}).get("type") != "auth.required":
            raise RuntimeError("credentialless MCP protection mismatch")
        mcp_version = request(process, 4, "tools/call", {"name": "version", "arguments": {}})
        version_failures = mcp_version_response_failures(mcp_version, expected_fields)
        if version_failures:
            raise RuntimeError(version_failures[0])
        drain_clean_shutdown(process, "EOF")

        process = launch()
        initialize(process, 5)
        interrupt = signal.CTRL_BREAK_EVENT if os.name == "nt" else signal.SIGINT
        drain_clean_shutdown(process, "SIGINT", interrupt)
        if os.name != "nt":
            process = launch()
            initialize(process, 6)
            drain_clean_shutdown(process, "SIGTERM", signal.SIGTERM)


WORKER_PROFILE_STATUS_CONTEXT = "partiful/live-worker-profiles"


def worker_profile_status_failures(payload: object, expected_revision: str) -> list[str]:
    """Require one successful native GitHub status for the exact release SHA."""
    if not isinstance(payload, dict):
        return ["invalid worker-profile status"]
    if payload.get("sha") != expected_revision:
        return ["worker-profile status SHA mismatch"]
    statuses = payload.get("statuses")
    if not isinstance(statuses, list):
        return ["invalid worker-profile status"]
    matches = [status for status in statuses if isinstance(status, dict) and status.get("context") == WORKER_PROFILE_STATUS_CONTEXT]
    if not matches:
        return ["worker-profile status is missing"]
    if len(matches) != 1:
        return ["worker-profile status is ambiguous"]
    state = matches[0].get("state")
    if state != "success":
        return [f"worker-profile status is {state}" if isinstance(state, str) else "invalid worker-profile status"]
    return []


@dataclass(frozen=True)
class PublicationPacket:
    target_evidence: dict[str, bool]
    revision_match: bool
    contract_integrity: bool
    worker_profile_attested: bool
    mcp_stdio_interop_gate: str


def publication_failures(packet: PublicationPacket) -> list[str]:
    missing = sorted(target.name for target in TARGETS if not packet.target_evidence.get(target.name))
    failures = [f"missing target evidence: {', '.join(missing)}"] if missing else []
    if not packet.revision_match:
        failures.append("release revision mismatch")
    if not packet.contract_integrity:
        failures.append("contract-integrity check failed")
    if not packet.worker_profile_attested:
        failures.append("worker-profile check failed")
    if packet.mcp_stdio_interop_gate != "CLOSED":
        failures.append(f"MCP16-STDIO-INTEROP is {packet.mcp_stdio_interop_gate}")
    return failures


def sbom_failures(sbom: object, manifest: dict[str, object]) -> list[str]:
    """Validate the required SPDX 2.3 release-document shape and identity."""
    if not isinstance(sbom, dict):
        return ["invalid sbom"]
    required_document = {
        "SPDXID": "SPDXRef-DOCUMENT",
        "spdxVersion": "SPDX-2.3",
        "dataLicense": "CC0-1.0",
    }
    if any(sbom.get(key) != value for key, value in required_document.items()):
        return ["invalid sbom"]
    if sbom.get("name") != f"partiful-{manifest.get('version')}":
        return ["invalid sbom"]
    creation = sbom.get("creationInfo")
    packages = sbom.get("packages")
    if not isinstance(creation, dict) or not isinstance(creation.get("created"), str) or creation.get("creators") != ["Tool: partiful-release"]:
        return ["invalid sbom"]
    try:
        datetime.datetime.fromisoformat(creation["created"].replace("Z", "+00:00"))
    except ValueError:
        return ["invalid sbom"]
    if not isinstance(packages, list) or len(packages) != 1 or not isinstance(packages[0], dict):
        return ["invalid sbom"]
    package = packages[0]
    if package.get("SPDXID") != "SPDXRef-Package-partiful" or package.get("name") != "partiful" or package.get("downloadLocation") != "NOASSERTION":
        return ["invalid sbom"]
    required_package = {
        "filesAnalyzed": False,
        "licenseConcluded": "NOASSERTION",
        "licenseDeclared": "NOASSERTION",
        "copyrightText": "NOASSERTION",
    }
    if any(package.get(key) != value for key, value in required_package.items()):
        return ["invalid sbom"]
    if package.get("versionInfo") != manifest.get("version"):
        return ["sbom version mismatch"]
    if sbom.get("documentNamespace") != f"https://github.com/KalebCole/partiful/releases/{manifest.get('version')}/{manifest.get('source_revision')}":
        return ["sbom revision mismatch"]
    if sbom.get("relationships") != [{
        "spdxElementId": "SPDXRef-DOCUMENT",
        "relationshipType": "DESCRIBES",
        "relatedSpdxElement": "SPDXRef-Package-partiful",
    }]:
        return ["invalid sbom"]
    expected_annotation = [{
        "annotationType": "OTHER",
        "annotator": "Tool: partiful-release",
        "annotationDate": creation["created"],
        "comment": json.dumps(manifest.get("release_fields"), sort_keys=True, separators=(",", ":")),
        "spdxElementId": "SPDXRef-DOCUMENT",
    }]
    if sbom.get("annotations") != expected_annotation:
        return ["invalid sbom"]
    return []


def archive_member_failures(archive: Path, target: object, binaries: object) -> list[str]:
    """Require each target archive to contain only its declared executable names."""
    target_name = getattr(target, "name", "")
    target_os = getattr(target, "goos", "")
    target_format = getattr(target, "archive_format", "")
    suffix = ".exe" if target_os == "windows" else ""
    expected = [f"partiful{suffix}", f"partiful-mcp{suffix}"]
    if binaries != expected:
        return [f"invalid binary matrix: {target_name}"]
    try:
        if target_format == "tar.gz":
            with tarfile.open(archive, "r:gz") as package:
                entries = package.getmembers()
                if not all(member.isfile() and member.mode & 0o111 for member in entries):
                    return [f"invalid archive members: {target_name}"]
                members = [member.name for member in entries]
        else:
            with zipfile.ZipFile(archive) as package:
                entries = package.infolist()
                if not all(stat.S_ISREG(entry.external_attr >> 16) and entry.external_attr >> 16 & 0o111 for entry in entries):
                    return [f"invalid archive members: {target_name}"]
                members = [entry.filename for entry in entries]
    except (OSError, tarfile.TarError, zipfile.BadZipFile):
        return [f"invalid archive members: {target_name}"]
    if len(members) != len(expected) or set(members) != set(expected):
        return [f"invalid archive members: {target_name}"]
    return []


def verify_release_directory(directory: Path, expected_revision: str | None = None) -> list[str]:
    failures: list[str] = []
    try:
        manifest = json.loads((directory / "manifest.json").read_text())
    except (OSError, json.JSONDecodeError) as error:
        return [f"invalid manifest: {error}"]
    if expected_revision and manifest.get("source_revision") != expected_revision:
        failures.append("release revision mismatch")
    archives = manifest.get("targets", [])
    epoch = manifest.get("source_date_epoch")
    if not isinstance(epoch, int) or isinstance(epoch, bool) or epoch < 0:
        failures.append("invalid source metadata")
    toolchain = manifest.get("toolchain")
    if not isinstance(toolchain, dict) or set(toolchain) != {"go", "build_flags"} or not all(isinstance(value, str) and value for value in toolchain.values()):
        failures.append("invalid toolchain metadata")
    fields = manifest.get("release_fields")
    if not isinstance(fields, dict) or set(fields) != RELEASE_FIELD_NAMES or fields.get("cli_version") != manifest.get("version") or not all(isinstance(value, str) and value for value in fields.values()):
        failures.append("invalid release fields")
    expected_targets = {target.name for target in TARGETS}
    if not isinstance(archives, list) or any(not isinstance(item, dict) for item in archives):
        failures.append("invalid target evidence: matrix")
        archives = []
    names = [item.get("target") for item in archives]
    if len(names) != len(expected_targets) or set(names) != expected_targets:
        failures.append("invalid target evidence: matrix")
    by_target = {item.get("target"): item for item in archives}
    for target in TARGETS:
        item = by_target.get(target.name)
        if not item:
            failures.append(f"missing target evidence: {target.name}")
            continue
        expected_archive = archive_name(str(manifest.get("version", "")), target)
        if item.get("archive") != expected_archive:
            failures.append(f"invalid target evidence: {target.name}")
            continue
        if item.get("release_fields") != fields:
            failures.append(f"invalid release fields: {target.name}")
            continue
        archive = directory / expected_archive
        if not archive.is_file() or item.get("sha256") != hashlib.sha256(archive.read_bytes()).hexdigest():
            failures.append(f"invalid archive evidence: {target.name}")
            continue
        failures.extend(archive_member_failures(archive, target, item.get("binaries")))
    checksum_path = directory / "SHA256SUMS"
    if not checksum_path.is_file():
        failures.append("missing SHA256SUMS")
    else:
        required_names = {
            archive_name(str(manifest.get("version", "")), target)
            for target in TARGETS
        } | {"manifest.json", "sbom.spdx.json"}
        entries: dict[str, str] = {}
        malformed = False
        for line in checksum_path.read_text().splitlines():
            match = re.fullmatch(r"([0-9a-f]{64})  ([^/\\]+)", line)
            if not match or match.group(2) in entries:
                malformed = True
                continue
            digest, name = match.groups()
            entries[name] = digest
        if malformed:
            failures.append("invalid checksum manifest")
        if set(entries) != required_names:
            failures.append("checksum manifest artifact set mismatch")
        for name, digest in entries.items():
            artifact = directory / name
            if not artifact.is_file() or hashlib.sha256(artifact.read_bytes()).hexdigest() != digest:
                failures.append(f"invalid checksum: {name}")
    sbom_path = directory / "sbom.spdx.json"
    if not sbom_path.is_file():
        failures.append("missing sbom.spdx.json")
    else:
        try:
            failures.extend(sbom_failures(json.loads(sbom_path.read_text()), manifest))
        except (OSError, json.JSONDecodeError):
            failures.append("invalid sbom")
    return failures


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--release-directory", type=Path, required=True)
    parser.add_argument("--expected-revision", required=True)
    parser.add_argument("--contract-integrity-passed", action="store_true")
    parser.add_argument("--worker-profile-status-file", type=Path, required=True)
    parser.add_argument("--mcp-stdio-interop-gate", required=True, choices=("OPEN", "CLOSED"))
    args = parser.parse_args()
    failures = verify_release_directory(args.release_directory, args.expected_revision)
    try:
        worker_profile_payload = json.loads(args.worker_profile_status_file.read_text())
    except (OSError, json.JSONDecodeError):
        worker_profile_failures = ["invalid worker-profile status"]
    else:
        worker_profile_failures = worker_profile_status_failures(worker_profile_payload, args.expected_revision)
    failures.extend(worker_profile_failures)
    target_evidence = {target.name: not any(target.name in failure for failure in failures) for target in TARGETS}
    failures.extend(publication_failures(PublicationPacket(target_evidence, not any(failure == "release revision mismatch" for failure in failures), args.contract_integrity_passed, not worker_profile_failures, args.mcp_stdio_interop_gate)))
    if failures:
        print(json.dumps(sorted(set(failures))), file=sys.stderr)
        return 1
    print("release verification passed; publication may proceed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
