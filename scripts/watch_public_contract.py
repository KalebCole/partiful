#!/usr/bin/env python3
"""Run bounded credential-free public contract observations."""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import socket
import sys
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timezone
from html.parser import HTMLParser
from pathlib import Path
from typing import Any, Callable

ROOT = Path(__file__).resolve().parents[1]
DEFAULT_REGISTRY = ROOT / "testdata" / "contract" / "probe-registry.json"
_SHA256 = re.compile(r"^[0-9a-f]{64}$")
_PROBE_KINDS = {"firebase-public-configuration", "public-asset-discovery", "protected-contract"}
_ENABLED_KEYS = {
    "gate_id", "kind", "enabled", "method", "target_host_class",
    "request_template_sha256", "expected", "limits",
}


class RegistryError(ValueError):
    """The probe registry is incomplete, unsafe, or not reviewed."""


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req: Any, fp: Any, code: int, msg: str, headers: Any, newurl: str) -> None:
        return None


class _AssetLinks(HTMLParser):
    def __init__(self) -> None:
        super().__init__()
        self.links: list[str] = []

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        attributes = dict(attrs)
        if tag == "script" and attributes.get("src"):
            self.links.append(attributes["src"] or "")
        elif tag == "link" and attributes.get("href") and "stylesheet" not in (attributes.get("rel") or "").lower():
            self.links.append(attributes["href"] or "")


def disabled_probe(gate_id: str, kind: str) -> dict[str, Any]:
    return {"gate_id": gate_id, "kind": kind, "enabled": False}


def allowed_probe_ids(operation_ids: list[str]) -> list[str]:
    return [
        "DRIFT17-FIREBASE-PROBE",
        "DRIFT17-ASSET-DISCOVERY",
        *(f"DRIFT17-PROTECTED-CONTRACT:{operation_id}" for operation_id in operation_ids),
    ]


def expected_probe_kinds(operation_ids: list[str]) -> dict[str, str]:
    return {
        "DRIFT17-FIREBASE-PROBE": "firebase-public-configuration",
        "DRIFT17-ASSET-DISCOVERY": "public-asset-discovery",
        **{
            f"DRIFT17-PROTECTED-CONTRACT:{operation_id}": "protected-contract"
            for operation_id in operation_ids
        },
    }


def _validate_enabled_probe(probe: dict[str, Any]) -> None:
    if set(probe) != _ENABLED_KEYS:
        raise RegistryError(f"enabled probe {probe.get('gate_id')} must contain only the reviewed fields")
    if probe.get("kind") not in _PROBE_KINDS or probe.get("method") not in {"GET", "HEAD", "POST"}:
        raise RegistryError(f"enabled probe {probe.get('gate_id')} has an invalid kind or method")
    if probe.get("kind") == "protected-contract":
        raise RegistryError(f"protected probe must remain disabled: {probe.get('gate_id')}")
    if not isinstance(probe.get("target_host_class"), str) or not probe["target_host_class"]:
        raise RegistryError(f"enabled probe {probe.get('gate_id')} lacks a target host class")
    if not isinstance(probe.get("request_template_sha256"), str) or not _SHA256.fullmatch(probe["request_template_sha256"]):
        raise RegistryError(f"enabled probe {probe.get('gate_id')} lacks a safe request-template digest")
    expected = probe.get("expected")
    if not isinstance(expected, dict) or set(expected) != {"statuses", "codes", "configuration_rejection_codes", "shape_sha256"}:
        raise RegistryError(f"enabled probe {probe.get('gate_id')} has an invalid expected classifier")
    if not isinstance(expected["statuses"], list) or not expected["statuses"] or not all(isinstance(value, int) for value in expected["statuses"]):
        raise RegistryError(f"enabled probe {probe.get('gate_id')} has no accepted statuses")
    if not isinstance(expected["codes"], list) or not all(isinstance(value, str) for value in expected["codes"]):
        raise RegistryError(f"enabled probe {probe.get('gate_id')} has invalid stable codes")
    if not isinstance(expected["configuration_rejection_codes"], list) or not all(isinstance(value, str) for value in expected["configuration_rejection_codes"]):
        raise RegistryError(f"enabled probe {probe.get('gate_id')} has invalid configuration-rejection codes")
    if not isinstance(expected["shape_sha256"], str) or not _SHA256.fullmatch(expected["shape_sha256"]):
        raise RegistryError(f"enabled probe {probe.get('gate_id')} has an invalid response-shape digest")
    limits = probe.get("limits")
    expected_limits = {"requests", "request_bytes", "response_bytes", "timeout_seconds", "redirects"}
    if not isinstance(limits, dict) or set(limits) != expected_limits:
        raise RegistryError(f"enabled probe {probe.get('gate_id')} has incomplete limits")
    if limits["requests"] != 1 or limits["redirects"] != 0:
        raise RegistryError(f"enabled probe {probe.get('gate_id')} exceeds one request or permits redirects")
    for key in ("request_bytes", "response_bytes", "timeout_seconds"):
        if not isinstance(limits[key], int) or limits[key] < (0 if key == "request_bytes" else 1):
            raise RegistryError(f"enabled probe {probe.get('gate_id')} has invalid {key}")


def validate_probe_registry(registry: dict[str, Any], operation_ids: list[str]) -> int:
    if not isinstance(registry, dict) or set(registry) != {"version", "contract_revision", "probes"} or registry.get("version") != 1:
        raise RegistryError("probe registry top-level shape is invalid")
    if not isinstance(registry.get("contract_revision"), str) or not registry["contract_revision"]:
        raise RegistryError("probe registry contract revision is missing")
    probes = registry.get("probes")
    if not isinstance(probes, list):
        raise RegistryError("probe registry probes must be a list")
    expected_kinds = expected_probe_kinds(operation_ids)
    expected_ids = list(expected_kinds)
    actual_ids = [probe.get("gate_id") for probe in probes if isinstance(probe, dict)]
    missing = sorted(set(expected_ids) - set(actual_ids))
    unknown = sorted(set(actual_ids) - set(expected_ids), key=str)
    duplicates = sorted({gate_id for gate_id in actual_ids if actual_ids.count(gate_id) > 1}, key=str)
    if missing or unknown or duplicates or len(probes) != len(expected_ids):
        raise RegistryError(f"probe inventory mismatch: missing={missing} unknown={unknown} duplicates={duplicates}")
    for probe in probes:
        gate_id = probe.get("gate_id")
        if probe.get("kind") != expected_kinds[gate_id]:
            raise RegistryError(f"probe {gate_id} kind does not match its gate identity")
        if probe.get("enabled") is False:
            if set(probe) != {"gate_id", "kind", "enabled"}:
                raise RegistryError(f"disabled probe {probe.get('gate_id')} must not retain request details")
        elif probe.get("enabled") is True:
            _validate_enabled_probe(probe)
        else:
            raise RegistryError(f"probe {probe.get('gate_id')} has non-boolean enabled state")
    return len(probes)


def _safe_probe_report(probe: dict[str, Any], classification: str, reason: str, response: dict[str, Any] | None = None) -> dict[str, Any]:
    report: dict[str, Any] = {
        "gate_id": probe["gate_id"],
        "classification": classification,
        "reason": reason,
    }
    if response is not None:
        for key in ("status", "code", "shape_sha256", "response_bytes", "duration_ms"):
            value = response.get(key)
            if isinstance(value, (str, int)) or value is None:
                report[key] = value
    return report


def run_probes(registry: dict[str, Any], operation_ids: list[str], requester: Callable[[dict[str, Any]], dict[str, Any]]) -> list[dict[str, Any]]:
    try:
        validate_probe_registry(registry, operation_ids)
    except RegistryError:
        return [{"gate_id": "probe-registry", "classification": "inconclusive", "reason": "invalid_probe_registry"}]
    reports = []
    for probe in registry["probes"]:
        if not probe["enabled"]:
            reports.append(_safe_probe_report(probe, "inconclusive", "probe_disabled"))
            continue
        try:
            response = requester(dict(probe))
        except (OSError, TimeoutError, socket.timeout, urllib.error.URLError):
            reports.append(_safe_probe_report(probe, "inconclusive", "network_failure"))
            continue
        if not isinstance(response, dict):
            reports.append(_safe_probe_report(probe, "inconclusive", "unknown_response"))
            continue
        limits = probe["limits"]
        if not isinstance(response.get("response_bytes"), int) or response["response_bytes"] > limits["response_bytes"]:
            reports.append(_safe_probe_report(probe, "inconclusive", "response_limit", response))
            continue
        status = response.get("status")
        expected = probe["expected"]
        if status in {429} or isinstance(status, int) and status >= 500:
            reports.append(_safe_probe_report(probe, "inconclusive", "transient_remote_failure", response))
        elif probe["kind"] == "firebase-public-configuration" and response.get("code") in expected["configuration_rejection_codes"]:
            reports.append(_safe_probe_report(probe, "confirmed_public_configuration_incompatibility", "accepted_compiled_configuration_rejection", response))
        elif status not in expected["statuses"]:
            reports.append(_safe_probe_report(probe, "inconclusive", "unknown_status", response))
        elif expected["codes"] and response.get("code") not in expected["codes"]:
            reports.append(_safe_probe_report(probe, "inconclusive", "unknown_stable_code", response))
        elif response.get("shape_sha256") != expected["shape_sha256"]:
            reports.append(_safe_probe_report(probe, "confirmed_contract_drift", "accepted_response_shape_changed", response))
        else:
            reports.append(_safe_probe_report(probe, "pass", "accepted_classifier", response))
    return reports


def _origin(url: str) -> tuple[str, str, int | None]:
    parsed = urllib.parse.urlsplit(url)
    return parsed.scheme.lower(), (parsed.hostname or "").lower(), parsed.port


def _safe_observed_path(url: str) -> str:
    parsed = urllib.parse.urlsplit(url)
    return parsed.path or "/"


def _default_fetch(url: str, timeout_seconds: int, max_bytes: int) -> dict[str, Any]:
    request = urllib.request.Request(url, method="GET", headers={"User-Agent": "partiful-contract-watch/1"})
    opener = urllib.request.build_opener(_NoRedirect())
    try:
        with opener.open(request, timeout=timeout_seconds) as response:
            body = response.read(max_bytes + 1)
            return {"url": response.geturl(), "status": response.status, "body": body}
    except urllib.error.HTTPError as error:
        body = error.read(max_bytes + 1)
        return {"url": error.geturl(), "status": error.code, "body": body}


def _validate_asset_policy(policy: dict[str, Any]) -> None:
    required = {
        "enabled", "seed_urls", "methods", "same_origin", "max_redirects", "max_assets",
        "max_response_bytes", "timeout_seconds", "extractor_version", "candidate_pattern",
        "project_pattern", "expected_project",
    }
    optional = {"allow_test_http_loopback"}
    if set(policy) - required - optional or required - set(policy):
        raise RegistryError("asset policy shape is invalid")
    if policy["enabled"] is not True or policy["methods"] != ["GET"] or policy["same_origin"] is not True or policy["max_redirects"] != 0:
        raise RegistryError("asset policy must be enabled, GET-only, same-origin, and no-redirect")
    if not isinstance(policy["seed_urls"], list) or not policy["seed_urls"]:
        raise RegistryError("asset policy has no fixed seed URLs")
    allow_loopback = policy.get("allow_test_http_loopback") is True
    for url in policy["seed_urls"]:
        parsed = urllib.parse.urlsplit(url)
        loopback = parsed.scheme == "http" and parsed.hostname in {"127.0.0.1", "::1", "localhost"}
        if parsed.scheme != "https" and not (allow_loopback and loopback):
            raise RegistryError("asset seeds must use HTTPS (except explicit loopback test fixtures)")
        if parsed.username or parsed.password or parsed.query or parsed.fragment:
            raise RegistryError("asset seed URLs cannot contain credentials, queries, or fragments")
    for key in ("max_assets", "max_response_bytes", "timeout_seconds"):
        if not isinstance(policy[key], int) or policy[key] < 1:
            raise RegistryError(f"asset policy has invalid {key}")
    if policy["max_assets"] > 20 or policy["max_response_bytes"] > 2_000_000 or policy["timeout_seconds"] > 15:
        raise RegistryError("asset policy exceeds reviewed hard limits")
    try:
        candidate = re.compile(policy["candidate_pattern"])
        project = re.compile(policy["project_pattern"])
    except (TypeError, re.error) as error:
        raise RegistryError(f"asset extraction grammar is invalid: {error}") from error
    if candidate.groups != 1 or project.groups != 1:
        raise RegistryError("asset extraction patterns must each have exactly one capture group")


def _asset_failure(observed_at: str, reason: str, paths: list[str], hashes: list[str], bytes_seen: int) -> dict[str, Any]:
    return {
        "classification": "inconclusive", "reason": reason, "observed_at": observed_at,
        "observed_paths": paths, "content_sha256": hashes, "response_bytes": bytes_seen,
        "candidate_count": 0, "candidate_fingerprints": [], "expected_project_match": False,
    }


def discover_public_assets(
    policy: dict[str, Any],
    fetcher: Callable[..., dict[str, Any]] | None = None,
    observed_at: str | None = None,
) -> dict[str, Any]:
    _validate_asset_policy(policy)
    fetcher = fetcher or _default_fetch
    observed_at = observed_at or datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")
    seed_origin = _origin(policy["seed_urls"][0])
    queue = list(policy["seed_urls"])
    visited: set[str] = set()
    paths: list[str] = []
    hashes: list[str] = []
    candidates: set[str] = set()
    projects: set[str] = set()
    bytes_seen = 0
    candidate_re = re.compile(policy["candidate_pattern"])
    project_re = re.compile(policy["project_pattern"])
    while queue and len(visited) < policy["max_assets"]:
        url = queue.pop(0)
        parsed_url = urllib.parse.urlsplit(url)
        request_url = urllib.parse.urlunsplit((parsed_url.scheme, parsed_url.netloc, parsed_url.path, parsed_url.query, ""))
        if request_url in visited or _origin(request_url) != seed_origin:
            continue
        visited.add(request_url)
        try:
            response = fetcher(request_url, timeout_seconds=policy["timeout_seconds"], max_bytes=policy["max_response_bytes"])
        except (OSError, TimeoutError, socket.timeout, urllib.error.URLError):
            return _asset_failure(observed_at, "network_failure", paths, hashes, bytes_seen)
        final_url = response.get("url")
        status = response.get("status")
        body = response.get("body")
        if not isinstance(final_url, str) or _origin(final_url) != seed_origin:
            return _asset_failure(observed_at, "redirect_policy", paths, hashes, bytes_seen)
        if isinstance(status, int) and 300 <= status < 400:
            return _asset_failure(observed_at, "redirect_policy", paths, hashes, bytes_seen)
        if status != 200 or not isinstance(body, bytes):
            reason = "transient_remote_failure" if status == 429 or isinstance(status, int) and status >= 500 else "unknown_status"
            return _asset_failure(observed_at, reason, paths, hashes, bytes_seen)
        if len(body) > policy["max_response_bytes"]:
            return _asset_failure(observed_at, "response_limit", paths, hashes, bytes_seen)
        bytes_seen += len(body)
        paths.append(_safe_observed_path(final_url))
        hashes.append(hashlib.sha256(body).hexdigest())
        text = body.decode("utf-8", errors="replace")
        candidates.update(match.group(1) for match in candidate_re.finditer(text))
        projects.update(match.group(1) for match in project_re.finditer(text))
        parser = _AssetLinks()
        parser.feed(text)
        for link in parser.links:
            resolved = urllib.parse.urljoin(final_url, link)
            if _origin(resolved) == seed_origin:
                queue.append(resolved)
    if queue:
        return _asset_failure(observed_at, "asset_limit", paths, hashes, bytes_seen)
    candidate_fingerprints = sorted(hashlib.sha256(value.encode()).hexdigest()[:16] for value in candidates)
    expected_match = policy["expected_project"] in projects
    if len(candidates) == 1 and expected_match:
        classification, reason = "pass", "one_discriminated_candidate"
    elif len(candidates) == 0:
        classification, reason = "inconclusive", "zero_candidates"
    elif len(candidates) > 1:
        classification, reason = "inconclusive", "multiple_candidates"
    else:
        classification, reason = "inconclusive", "project_discriminator_mismatch"
    return {
        "classification": classification, "reason": reason, "observed_at": observed_at,
        "extractor_version": policy["extractor_version"], "observed_paths": paths,
        "content_sha256": hashes, "response_bytes": bytes_seen, "candidate_count": len(candidates),
        "candidate_fingerprints": candidate_fingerprints, "expected_project_match": expected_match,
    }


def deduplicate_reports(reports: list[dict[str, Any]]) -> list[dict[str, Any]]:
    deduplicated: list[dict[str, Any]] = []
    positions: dict[str, int] = {}
    for report in reports:
        semantic = {key: value for key, value in report.items() if key not in {"observed_at", "occurrences", "duration_ms"}}
        fingerprint = hashlib.sha256(json.dumps(semantic, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
        if fingerprint in positions:
            deduplicated[positions[fingerprint]]["occurrences"] += 1
        else:
            copy = dict(report)
            copy["fingerprint"] = fingerprint[:16]
            copy["occurrences"] = 1
            positions[fingerprint] = len(deduplicated)
            deduplicated.append(copy)
    return deduplicated


def _operation_ids(root: Path) -> list[str]:
    spec = json.loads((root / "spec" / "partiful.openapi.json").read_text(encoding="utf-8"))
    return [
        operation["operationId"]
        for path_item in spec.get("paths", {}).values()
        for operation in path_item.values()
        if isinstance(operation, dict) and "operationId" in operation
    ]


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--registry", type=Path)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args(argv)
    root = args.root.resolve()
    registry_path = args.registry or (root / "testdata" / "contract" / "probe-registry.json")
    try:
        registry = json.loads(registry_path.read_text(encoding="utf-8"))
        operation_ids = _operation_ids(root)
        validate_probe_registry(registry, operation_ids)
        reports = run_probes(registry, operation_ids, lambda _: (_ for _ in ()).throw(RegistryError("no reviewed network requester is configured")))
        reports = deduplicate_reports(reports)
        result = {"contract_revision": registry["contract_revision"], "reports": reports}
        encoded = json.dumps(result, indent=2, sort_keys=True) + "\n"
        if args.output:
            args.output.write_text(encoded, encoding="utf-8")
        else:
            print(encoded, end="")
    except (OSError, UnicodeError, json.JSONDecodeError, RegistryError) as error:
        print(f"FAIL: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
