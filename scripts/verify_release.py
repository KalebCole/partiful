#!/usr/bin/env python3
"""Verify release artifacts and fail closed before GitHub Release publication."""
from __future__ import annotations

import argparse
import datetime
import hashlib
import json
import re
import stat
import tarfile
from dataclasses import dataclass
from pathlib import Path
import sys
import zipfile

if __package__ in {None, ""}:
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from scripts.build_release import TARGETS, archive_name


@dataclass(frozen=True)
class PublicationPacket:
    target_evidence: dict[str, bool]
    revision_match: bool
    contract_integrity: bool
    worker_profiles: bool
    mcp_stdio_interop_gate: str


def publication_failures(packet: PublicationPacket) -> list[str]:
    missing = sorted(target.name for target in TARGETS if not packet.target_evidence.get(target.name))
    failures = [f"missing target evidence: {', '.join(missing)}"] if missing else []
    if not packet.revision_match:
        failures.append("release revision mismatch")
    if not packet.contract_integrity:
        failures.append("contract-integrity check failed")
    if not packet.worker_profiles:
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
    fields = manifest.get("release_fields")
    required_field_names = {"cli_version", "command_contract_revision", "transport_contract_revision"}
    if not isinstance(fields, dict) or set(fields) != required_field_names or fields.get("cli_version") != manifest.get("version") or not all(isinstance(value, str) and value for value in fields.values()):
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
    parser.add_argument("--expected-revision")
    parser.add_argument("--contract-integrity-passed", action="store_true")
    parser.add_argument("--worker-profiles-passed", action="store_true")
    parser.add_argument("--mcp-stdio-interop-gate", required=True, choices=("OPEN", "CLOSED"))
    args = parser.parse_args()
    failures = verify_release_directory(args.release_directory, args.expected_revision)
    target_evidence = {target.name: not any(target.name in failure for failure in failures) for target in TARGETS}
    failures.extend(publication_failures(PublicationPacket(target_evidence, not any(failure == "release revision mismatch" for failure in failures), args.contract_integrity_passed, args.worker_profiles_passed, args.mcp_stdio_interop_gate)))
    if failures:
        print(json.dumps(sorted(set(failures))), file=sys.stderr)
        return 1
    print("release verification passed; publication may proceed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
