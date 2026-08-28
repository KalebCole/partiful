#!/usr/bin/env python3
"""Verify release artifacts and fail closed before GitHub Release publication."""
from __future__ import annotations

import argparse
import hashlib
import json
import re
from dataclasses import dataclass
from pathlib import Path
import sys

if __package__ in {None, ""}:
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from scripts.build_release import TARGETS


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


def verify_release_directory(directory: Path, expected_revision: str | None = None) -> list[str]:
    failures: list[str] = []
    try:
        manifest = json.loads((directory / "manifest.json").read_text())
    except (OSError, json.JSONDecodeError) as error:
        return [f"invalid manifest: {error}"]
    if expected_revision and manifest.get("source_revision") != expected_revision:
        failures.append("release revision mismatch")
    archives = manifest.get("targets", [])
    by_target = {item.get("target"): item for item in archives if isinstance(item, dict)}
    for target in TARGETS:
        item = by_target.get(target.name)
        if not item:
            failures.append(f"missing target evidence: {target.name}")
            continue
        archive = directory / str(item.get("archive", ""))
        if not archive.is_file() or item.get("sha256") != hashlib.sha256(archive.read_bytes()).hexdigest():
            failures.append(f"invalid archive evidence: {target.name}")
        if item.get("binaries") != ["partiful", "partiful-mcp"]:
            failures.append(f"invalid binary matrix: {target.name}")
    checksum_path = directory / "checksums.txt"
    if not checksum_path.is_file():
        failures.append("missing checksums.txt")
    else:
        required_names = {
            f"partiful_{manifest.get('version', '')}_{target.name}.{'tar.gz' if target.archive_format == 'tar.gz' else 'zip'}"
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
    if not (directory / "sbom.spdx.json").is_file():
        failures.append("missing sbom.spdx.json")
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
