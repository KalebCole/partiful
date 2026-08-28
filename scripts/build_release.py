#!/usr/bin/env python3
"""Create deterministic archives for the reviewed Partiful native release surface."""
from __future__ import annotations

import argparse
import datetime
import gzip
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import zipfile
import re
from dataclasses import dataclass
from typing import Callable

ROOT = Path(__file__).resolve().parents[1]
VERSION_SYMBOL = "github.com/KalebCole/partiful/internal/version.CLIVersion"
BINARY_NAMES = ("partiful", "partiful-mcp")
SEMVER_TAG = re.compile(r"v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$")


@dataclass(frozen=True)
class Target:
    name: str
    goos: str
    goarch: str
    archive_format: str


TARGETS = (
    Target("darwin-amd64", "darwin", "amd64", "tar.gz"),
    Target("darwin-arm64", "darwin", "arm64", "tar.gz"),
    Target("linux-amd64", "linux", "amd64", "tar.gz"),
    Target("linux-arm64", "linux", "arm64", "tar.gz"),
    Target("windows-amd64", "windows", "amd64", "zip"),
)
Runner = Callable[[list[str], dict[str, str]], None]
Smoke = Callable[[Path, Target], None]


def sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def go_build_command(*, target: Target, binary: str, output: Path, version: str) -> list[str]:
    package = "./cmd/partiful" if binary == "partiful" else "./cmd/partiful-mcp"
    return [
        "go", "build", "-trimpath", "-mod=readonly", "-buildvcs=false",
        "-ldflags", f"-s -w -X {VERSION_SYMBOL}={version}", "-o", str(output), package,
    ]


def subprocess_runner(command: list[str], env: dict[str, str]) -> None:
    completed = subprocess.run(command, cwd=ROOT, env=env, text=True, capture_output=True, check=False)
    if completed.returncode:
        raise RuntimeError(f"release build failed: {' '.join(command)}\n{completed.stderr.strip()}")


def fixture_runner(command: list[str], env: dict[str, str]) -> None:
    """Write deterministic fixture binaries for unit tests; never invokes a compiler."""
    output = Path(command[command.index("-o") + 1])
    output.parent.mkdir(parents=True, exist_ok=True)
    normalized = list(command)
    normalized[normalized.index("-o") + 1] = "<output>"
    output.write_bytes(("fixture-binary\n" + "\n".join(normalized) + "\n" + env["GOOS"] + "/" + env["GOARCH"]).encode())
    output.chmod(0o755)


def _tar_archive(source: Path, destination: Path, epoch: int) -> None:
    with destination.open("wb") as raw:
        with gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=epoch) as compressed:
            with tarfile.open(fileobj=compressed, mode="w") as archive:
                for child in sorted(source.iterdir()):
                    info = archive.gettarinfo(str(child), arcname=child.name)
                    info.uid = info.gid = 0
                    info.uname = info.gname = ""
                    info.mtime = epoch
                    with child.open("rb") as stream:
                        archive.addfile(info, stream)


def _zip_archive(source: Path, destination: Path, epoch: int) -> None:
    import datetime
    date_time = datetime.datetime.fromtimestamp(max(epoch, 315532800), tz=datetime.timezone.utc).timetuple()[:6]
    with zipfile.ZipFile(destination, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        for child in sorted(source.iterdir()):
            info = zipfile.ZipInfo(child.name, date_time=date_time)
            info.external_attr = 0o100755 << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            archive.writestr(info, child.read_bytes())


def archive_target(source: Path, destination: Path, target: Target, epoch: int) -> None:
    if target.archive_format == "tar.gz":
        _tar_archive(source, destination, epoch)
    else:
        _zip_archive(source, destination, epoch)


def archive_binary_names(target: Target) -> list[str]:
    suffix = ".exe" if target.goos == "windows" else ""
    return [f"{binary}{suffix}" for binary in BINARY_NAMES]


def archive_name(version: str, target: Target) -> str:
    extension = "tar.gz" if target.archive_format == "tar.gz" else "zip"
    return f"partiful_{version}_{target.goos}_{target.goarch}.{extension}"


def release_fields(version: str) -> dict[str, str]:
    """Read the two reviewed source-owned revisions for release evidence."""
    source = (ROOT / "internal/version/version.go").read_text()
    revisions = dict(re.findall(r"(CommandContractRevision|TransportContractRevision)\s*=\s*\"([^\"]+)\"", source))
    if set(revisions) != {"CommandContractRevision", "TransportContractRevision"}:
        raise RuntimeError("unable to read reviewed release contract revisions")
    return {
        "cli_version": version,
        "command_contract_revision": revisions["CommandContractRevision"],
        "transport_contract_revision": revisions["TransportContractRevision"],
    }


def release_digest(directory: Path) -> dict[str, str]:
    return {path.relative_to(directory).as_posix(): sha256(path) for path in sorted(directory.rglob("*")) if path.is_file()}


def _toolchain_metadata() -> dict[str, str]:
    completed = subprocess.run(["go", "version"], cwd=ROOT, text=True, capture_output=True, check=False)
    return {"go": completed.stdout.strip() if completed.returncode == 0 else "unavailable", "build_flags": "-trimpath -mod=readonly -buildvcs=false"}


def spdx_document(*, version: str, source_revision: str, source_date_epoch: int, fields: dict[str, str]) -> dict[str, object]:
    """Return the deterministic SPDX 2.3 inventory for one immutable release."""
    created = datetime.datetime.fromtimestamp(source_date_epoch, tz=datetime.timezone.utc).isoformat().replace("+00:00", "Z")
    return {
        "SPDXID": "SPDXRef-DOCUMENT",
        "spdxVersion": "SPDX-2.3",
        "dataLicense": "CC0-1.0",
        "name": f"partiful-{version}",
        "documentNamespace": f"https://github.com/KalebCole/partiful/releases/{version}/{source_revision}",
        "creationInfo": {"created": created, "creators": ["Tool: partiful-release"]},
        "packages": [{
            "SPDXID": "SPDXRef-Package-partiful",
            "name": "partiful",
            "versionInfo": version,
            "downloadLocation": "NOASSERTION",
            "filesAnalyzed": False,
            "licenseConcluded": "NOASSERTION",
            "licenseDeclared": "NOASSERTION",
            "copyrightText": "NOASSERTION",
        }],
        "relationships": [{
            "spdxElementId": "SPDXRef-DOCUMENT",
            "relationshipType": "DESCRIBES",
            "relatedSpdxElement": "SPDXRef-Package-partiful",
        }],
        "annotations": [{
            "annotationType": "OTHER",
            "annotator": "Tool: partiful-release",
            "annotationDate": created,
            "comment": json.dumps(fields, sort_keys=True, separators=(",", ":")),
            "spdxElementId": "SPDXRef-DOCUMENT",
        }],
    }


def build_release(*, output: Path, version: str, source_date_epoch: int, source_revision: str, runner: Runner = subprocess_runner, smoke: Smoke | None = None) -> dict:
    if not SEMVER_TAG.fullmatch(version) or not re.fullmatch(r"[0-9a-f]{40}", source_revision):
        raise ValueError("version must be a vX.Y.Z semantic tag and source_revision must be 40 lowercase hex characters")
    if output.exists():
        shutil.rmtree(output)
    output.mkdir(parents=True)
    archives: list[dict[str, object]] = []
    fields = release_fields(version)
    with tempfile.TemporaryDirectory(prefix="partiful-release-") as temporary:
        staging = Path(temporary)
        for target in TARGETS:
            target_stage = staging / target.name
            target_stage.mkdir()
            environment = os.environ.copy() | {"CGO_ENABLED": "0", "GOOS": target.goos, "GOARCH": target.goarch, "SOURCE_DATE_EPOCH": str(source_date_epoch)}
            for binary in BINARY_NAMES:
                suffix = ".exe" if target.goos == "windows" else ""
                runner(go_build_command(target=target, binary=binary, output=target_stage / f"{binary}{suffix}", version=version), environment)
            if smoke is not None:
                smoke(target_stage, target)
            filename = archive_name(version, target)
            archive_target(target_stage, output / filename, target, source_date_epoch)
            archives.append({"target": target.name, "archive": filename, "binaries": archive_binary_names(target), "sha256": sha256(output / filename), "release_fields": fields})
    metadata = {"version": version, "source_revision": source_revision, "source_date_epoch": source_date_epoch, "toolchain": _toolchain_metadata(), "release_fields": fields, "targets": archives}
    (output / "manifest.json").write_text(json.dumps(metadata, sort_keys=True, separators=(",", ":")) + "\n")
    sbom = spdx_document(version=version, source_revision=source_revision, source_date_epoch=source_date_epoch, fields=fields)
    (output / "sbom.spdx.json").write_text(json.dumps(sbom, sort_keys=True, separators=(",", ":")) + "\n")
    checksums = [f"{item['sha256']}  {item['archive']}" for item in archives]
    checksums.extend(f"{sha256(output / name)}  {name}" for name in ("manifest.json", "sbom.spdx.json"))
    (output / "SHA256SUMS").write_text("\n".join(sorted(checksums)) + "\n")
    return metadata


def assert_reproducible(*, output: Path, version: str, source_date_epoch: int, source_revision: str, runner: Runner = subprocess_runner, smoke: Smoke | None = None) -> None:
    """Rebuild from the same immutable inputs and compare every release artifact."""
    with tempfile.TemporaryDirectory(prefix="partiful-release-reproducibility-") as temporary:
        comparison = Path(temporary) / "release"
        build_release(output=comparison, version=version, source_date_epoch=source_date_epoch, source_revision=source_revision, runner=runner, smoke=smoke)
        if release_digest(output) != release_digest(comparison):
            raise RuntimeError("release artifacts are not reproducible from identical immutable inputs")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--source-date-epoch", type=int, required=True)
    parser.add_argument("--source-revision", required=True)
    parser.add_argument("--verify-reproducible", action="store_true")
    args = parser.parse_args()
    build_release(output=args.output, version=args.version, source_date_epoch=args.source_date_epoch, source_revision=args.source_revision)
    if args.verify_reproducible:
        assert_reproducible(output=args.output, version=args.version, source_date_epoch=args.source_date_epoch, source_revision=args.source_revision)
    print(json.dumps(release_digest(args.output), sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
