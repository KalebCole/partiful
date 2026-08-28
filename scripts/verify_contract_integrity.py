#!/usr/bin/env python3
"""Verify the reviewed transport, evidence, and public-command contracts."""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import subprocess
import sys
from pathlib import Path
from typing import Any, Callable
from urllib.parse import unquote

ROOT = Path(__file__).resolve().parents[1]
DEFAULT_SNAPSHOT = ROOT / "testdata" / "contract" / "semantic-snapshot.json"
DEFAULT_REPLAY = ROOT / "testdata" / "contract" / "fixture-replay.json"
_ALLOWED_REPLAY_PACKAGES = ("./internal/transport/...", "./internal/app/...")
_IGNORED_SEMANTIC_KEYS = {"description", "summary", "externalDocs", "example", "examples", "title"}
_SENSITIVE_KEYS = re.compile(r"(^|_)(authorization|cookie|api_?key|token|phone|message|event_?id|guest_?id|user_?id|contact_?name|access_?url|query_?string|body)($|_)", re.I)
_SHA256 = re.compile(r"^[0-9a-f]{64}$")


class IntegrityError(AssertionError):
    """A repository contract is inconsistent or unsafe."""


def _read_json(path: Path) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as error:
        raise IntegrityError(f"cannot read JSON {path}: {error}") from error


def _go_constant(text: str, name: str) -> str:
    match = re.search(rf"\b{name}\s*=\s*\"([^\"]+)\"", text)
    if match is None:
        raise IntegrityError(f"missing Go version constant {name}")
    return match.group(1)


def verify_revision_parity(root: Path = ROOT) -> str:
    spec = _read_json(root / "spec" / "partiful.openapi.json")
    ledger = _read_json(root / "evidence" / "ledger.json")
    try:
        version_source = (root / "internal" / "version" / "version.go").read_text(encoding="utf-8")
        values = {
            "OpenAPI info.version": spec["info"]["version"],
            "ledger contractRevision": ledger["contractRevision"],
            "Go TransportContractRevision": _go_constant(version_source, "TransportContractRevision"),
        }
    except (KeyError, OSError, UnicodeError, TypeError) as error:
        raise IntegrityError(f"cannot read revision tuple: {error}") from error
    if any(not isinstance(value, str) or not value for value in values.values()) or len(set(values.values())) != 1:
        raise IntegrityError("revision mismatch: " + ", ".join(f"{key}={value!r}" for key, value in values.items()))
    return next(iter(values.values()))


def _resolve_pointer(document: Any, pointer: str) -> Any:
    if pointer == "#":
        return document
    if not pointer.startswith("#/"):
        raise IntegrityError(f"invalid material claim pointer {pointer!r}")
    value = document
    for raw in pointer[2:].split("/"):
        token = raw.replace("~1", "/").replace("~0", "~")
        try:
            value = value[int(token)] if isinstance(value, list) else value[token]
        except (KeyError, IndexError, TypeError, ValueError) as error:
            raise IntegrityError(f"material claim pointer does not resolve: {pointer}") from error
    return value


def _heading_slug(heading: str) -> str:
    slug = re.sub(r"[^\w\- ]", "", heading.strip().lower(), flags=re.UNICODE)
    return re.sub(r"[ -]+", "-", slug).strip("-")


def _verify_citation(root: Path, citation: Any) -> None:
    if not isinstance(citation, str) or not citation or citation.startswith(("http://", "https://")):
        raise IntegrityError(f"citation must be a local repository path: {citation!r}")
    path_text, separator, fragment = citation.partition("#")
    path = (root / unquote(path_text)).resolve()
    try:
        path.relative_to(root.resolve())
    except ValueError as error:
        raise IntegrityError(f"citation leaves repository: {citation}") from error
    if not path.is_file():
        raise IntegrityError(f"citation path does not exist: {citation}")
    if separator:
        if path.suffix.lower() != ".md" or not fragment:
            raise IntegrityError(f"citation fragment is not a Markdown anchor: {citation}")
        headings = []
        for line in path.read_text(encoding="utf-8").splitlines():
            match = re.match(r"^#{1,6}\s+(.+?)\s*#*$", line)
            if match:
                headings.append(_heading_slug(match.group(1)))
        if unquote(fragment).lower() not in headings:
            raise IntegrityError(f"citation anchor does not resolve: {citation}")


def verify_provenance(root: Path = ROOT) -> int:
    spec = _read_json(root / "spec" / "partiful.openapi.json")
    ledger = _read_json(root / "evidence" / "ledger.json")
    allowed = ledger.get("allowedClassifications")
    claims = ledger.get("claims")
    if not isinstance(allowed, list) or not allowed or len(allowed) != len(set(allowed)):
        raise IntegrityError("allowed classifications must be a nonempty unique list")
    if not isinstance(claims, dict):
        raise IntegrityError("ledger claims must be an object")
    if ledger.get("materialClaimCount") != len(claims):
        raise IntegrityError("materialClaimCount does not match claims")
    for pointer, claim in claims.items():
        _resolve_pointer(spec, pointer)
        if not isinstance(claim, dict) or claim.get("classification") not in allowed:
            raise IntegrityError(f"material claim {pointer} has an unallowed classification")
        _verify_citation(root, claim.get("citation"))
    operation_ids = {
        operation.get("operationId")
        for path_item in spec.get("paths", {}).values()
        if isinstance(path_item, dict)
        for operation in path_item.values()
        if isinstance(operation, dict) and operation.get("operationId")
    }
    operations = ledger.get("operations")
    if not isinstance(operations, dict) or set(operations) != operation_ids:
        raise IntegrityError("ledger operation provenance does not match OpenAPI operation inventory")
    for operation_id, claim in operations.items():
        if claim.get("classification") not in allowed:
            raise IntegrityError(f"operation {operation_id} has an unallowed classification")
        _verify_citation(root, claim.get("citation"))
    return len(claims)


def _semantic_value(value: Any, key: str = "") -> Any:
    if isinstance(value, dict):
        result = {}
        for child_key in sorted(value):
            if child_key in _IGNORED_SEMANTIC_KEYS or child_key.startswith("x-evidence"):
                continue
            child = value[child_key]
            if child_key == "x-publicValue" and isinstance(child, str):
                result[child_key] = {"sha256": hashlib.sha256(child.encode()).hexdigest()}
            else:
                result[child_key] = _semantic_value(child, child_key)
        return result
    if isinstance(value, list):
        return [_semantic_value(item, key) for item in value]
    return value


def semantic_transport_snapshot(spec: dict[str, Any]) -> dict[str, Any]:
    """Return deterministic executable OpenAPI semantics without prose or raw public config."""
    selected = {
        "openapi": spec.get("openapi"),
        "servers": spec.get("servers"),
        "paths": spec.get("paths"),
        "components": spec.get("components"),
    }
    return _semantic_value(selected)


_GO_OPERATORS = tuple(sorted({
    "<<=", ">>=", "&^=", "...", "==", "!=", "<=", ">=", ":=", "++", "--",
    "&&", "||", "<-", "<<", ">>", "&^", "+=", "-=", "*=", "/=", "%=", "&=",
    "|=", "^=",
}, key=len, reverse=True))


def _go_semantic_tokens(source: str) -> list[str]:
    """Tokenize executable Go source while ignoring comments and formatting."""
    tokens: list[str] = []
    index = 0
    while index < len(source):
        if source[index].isspace():
            index += 1
        elif source.startswith("//", index):
            newline = source.find("\n", index + 2)
            index = len(source) if newline < 0 else newline + 1
        elif source.startswith("/*", index):
            end = source.find("*/", index + 2)
            if end < 0:
                raise IntegrityError("unterminated Go block comment")
            index = end + 2
        elif source[index] in {'"', "'", "`"}:
            delimiter = source[index]
            end = index + 1
            while end < len(source):
                if delimiter != "`" and source[end] == "\\":
                    end += 2
                elif source[end] == delimiter:
                    break
                else:
                    end += 1
            if end >= len(source):
                raise IntegrityError("unterminated Go literal")
            tokens.append(source[index:end + 1])
            index = end + 1
        elif source[index].isalpha() or source[index] == "_":
            end = index + 1
            while end < len(source) and (source[end].isalnum() or source[end] == "_"):
                end += 1
            tokens.append(source[index:end])
            index = end
        elif source[index].isdigit() or source[index] == "." and index + 1 < len(source) and source[index + 1].isdigit():
            end = index + 1
            while end < len(source) and (source[end].isalnum() or source[end] in "._"):
                end += 1
            tokens.append(source[index:end])
            index = end
        else:
            operator = next((value for value in _GO_OPERATORS if source.startswith(value, index)), source[index])
            tokens.append(operator)
            index += len(operator)
    return tokens


def executable_transport_snapshot(root: Path = ROOT) -> dict[str, Any]:
    """Fingerprint executable Go semantics without comments or source formatting."""
    candidates = list((root / "internal" / "transport").rglob("*.go"))
    app_root = root / "internal" / "app"
    if app_root.is_dir():
        candidates.extend(app_root.glob("*_ops.go"))
        candidates.extend(app_root / name for name in ("invocation.go", "project.go", "errors.go") if (app_root / name).is_file())
    files: dict[str, dict[str, Any]] = {}
    for path in sorted(set(candidates)):
        if path.name.endswith("_test.go"):
            continue
        tokens = _go_semantic_tokens(path.read_text(encoding="utf-8"))
        digest = hashlib.sha256(json.dumps(tokens, ensure_ascii=False, separators=(",", ":")).encode()).hexdigest()
        files[path.relative_to(root).as_posix()] = {"token_count": len(tokens), "token_sha256": digest}
    if not files:
        raise IntegrityError("no executable transport sources found")
    return {"format": 2, "files": files}


def classify_semantic_diff(before: dict[str, Any], after: dict[str, Any]) -> str:
    if before.get("command") != after.get("command"):
        return "command_release_required"
    if before.get("transport") != after.get("transport"):
        return "transport_release_required"
    if before != after:
        return "evidence_only"
    return "unchanged"


def semantic_diff_requires_baseline_update(classification: str) -> bool:
    return classification in {"transport_release_required", "command_release_required"}


def validate_sanitized_fixture(value: Any, location: str = "$") -> None:
    if isinstance(value, dict):
        for key, child in value.items():
            if not isinstance(key, str):
                raise IntegrityError(f"fixture key is not text at {location}")
            if _SENSITIVE_KEYS.search(key):
                raise IntegrityError(f"fixture contains forbidden field {location}.{key}")
            validate_sanitized_fixture(child, f"{location}.{key}")
    elif isinstance(value, list):
        for index, child in enumerate(value):
            validate_sanitized_fixture(child, f"{location}[{index}]")
    elif isinstance(value, str):
        if re.search(r"Bearer\s+\S+|\+?\d{10,}|https?://[^\s?]+\?", value, re.I):
            raise IntegrityError(f"fixture contains sensitive value at {location}")


def verify_command_registry(snapshot: dict[str, Any]) -> tuple[int, int]:
    commands = snapshot.get("commands")
    tools = snapshot.get("mcp_tools")
    cli_only = snapshot.get("cli_only_commands")
    if not isinstance(commands, list) or len(commands) != 24:
        raise IntegrityError("executable command registry must contain 24 commands")
    ids = [command.get("id") for command in commands]
    paths = [command.get("cli_path") for command in commands]
    if ids != [f"CMD-{number:03d}" for number in range(1, 25)] or len(set(paths)) != 24:
        raise IntegrityError("executable command IDs and paths must be exact and unique")
    paired = [command for command in commands if command.get("mcp") is not None]
    derived_tools = [command["mcp"].get("name") for command in paired]
    if cli_only != ["auth login"] or not isinstance(tools, list) or len(paired) != 23 or tools != derived_tools or len(set(tools)) != 23:
        raise IntegrityError("auth.login must be the sole CLI-only command and 23 MCP tools must be unique")
    for command in commands:
        risk = command.get("risk")
        if bool(command.get("dry_run")) != (risk in {"credential-delete", "write", "destructive-write"}):
            raise IntegrityError(f"command {command.get('id')} dry-run invariant failed")
    return len(commands), len(tools)


def semantic_command_snapshot(snapshot: dict[str, Any]) -> dict[str, Any]:
    verify_command_registry(snapshot)
    commands = []
    for command in snapshot["commands"]:
        encoded = json.dumps(command, sort_keys=True, separators=(",", ":")).encode()
        mcp = command.get("mcp")
        commands.append({
            "id": command["id"],
            "cli_path": command["cli_path"],
            "mcp_name": mcp.get("name") if isinstance(mcp, dict) else None,
            "sha256": hashlib.sha256(encoded).hexdigest(),
        })
    return {"format": 1, "commands": commands}


def executable_command_snapshot(root: Path = ROOT) -> dict[str, Any]:
    result = subprocess.run(
        ["go", "run", "./cmd/partiful", "--json", "schema"], cwd=root,
        text=True, capture_output=True, check=False,
    )
    if result.returncode != 0:
        raise IntegrityError(f"cannot read executable command registry: {result.stderr.strip()}")
    try:
        snapshot = json.loads(result.stdout)
    except json.JSONDecodeError as error:
        raise IntegrityError("executable command registry did not emit one JSON result") from error
    verify_command_registry(snapshot)
    return snapshot


def replay_sanitized_fixtures(root: Path, manifest: dict[str, Any], runner: Callable[..., Any] = subprocess.run) -> None:
    if set(manifest) != {"version", "packages"} or manifest.get("version") != 1 or manifest.get("packages") != list(_ALLOWED_REPLAY_PACKAGES):
        raise IntegrityError("fixture replay manifest is not the reviewed closed package set")
    command = ["go", "test", *_ALLOWED_REPLAY_PACKAGES]
    result = runner(command, cwd=root, text=True, capture_output=True, check=False)
    if result.returncode != 0:
        raise IntegrityError(f"sanitized fixture replay failed: {(result.stderr or result.stdout).strip()}")


def build_snapshot(root: Path = ROOT) -> dict[str, Any]:
    spec = _read_json(root / "spec" / "partiful.openapi.json")
    ledger = _read_json(root / "evidence" / "ledger.json")
    command = executable_command_snapshot(root)
    version_source = (root / "internal" / "version" / "version.go").read_text(encoding="utf-8")
    evidence_semantics = {pointer: {"classification": claim.get("classification"), "citation": claim.get("citation")} for pointer, claim in ledger["claims"].items()}
    return {
        "format": 1,
        "transport_revision": spec["info"]["version"],
        "command_revision": _go_constant(version_source, "CommandContractRevision"),
        "transport": {
            "openapi": semantic_transport_snapshot(spec),
            "executable": executable_transport_snapshot(root),
        },
        "command": semantic_command_snapshot(command),
        "evidence": {"sha256": hashlib.sha256(json.dumps(evidence_semantics, sort_keys=True, separators=(",", ":")).encode()).hexdigest()},
    }


def verify(root: Path = ROOT, snapshot_path: Path | None = None, replay: bool = True) -> dict[str, Any]:
    snapshot_path = snapshot_path or (root / "testdata" / "contract" / "semantic-snapshot.json")
    revision = verify_revision_parity(root)
    claim_count = verify_provenance(root)
    current = build_snapshot(root)
    expected = _read_json(snapshot_path)
    classification = classify_semantic_diff(expected, current)
    if semantic_diff_requires_baseline_update(classification):
        raise IntegrityError(f"semantic snapshot differs: {classification}; regenerate only in the reviewed contract change")
    if replay:
        replay_sanitized_fixtures(root, _read_json(root / "testdata" / "contract" / "fixture-replay.json"))
    return {"revision": revision, "material_claims": claim_count, "commands": 24, "mcp_tools": 23, "semantic_diff": classification}


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--snapshot", type=Path)
    parser.add_argument("--write-snapshot", action="store_true", help="write the current safe semantic baseline")
    parser.add_argument("--skip-replay", action="store_true")
    args = parser.parse_args(argv)
    try:
        root = args.root.resolve()
        snapshot_path = args.snapshot or (root / "testdata" / "contract" / "semantic-snapshot.json")
        if args.write_snapshot:
            verify_revision_parity(root)
            verify_provenance(root)
            snapshot_path.parent.mkdir(parents=True, exist_ok=True)
            snapshot_path.write_text(json.dumps(build_snapshot(root), indent=2, sort_keys=True) + "\n", encoding="utf-8")
            print(f"WROTE: {snapshot_path.relative_to(root)}")
            return 0
        summary = verify(root, snapshot_path, replay=not args.skip_replay)
    except (IntegrityError, OSError, UnicodeError) as error:
        print(f"FAIL: {error}", file=sys.stderr)
        return 1
    print("PASS: " + " ".join(f"{key}={value}" for key, value in summary.items()))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
