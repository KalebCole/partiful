"""Controlled read and one-attempt mutation verification wrappers."""
from __future__ import annotations

import hashlib
import hmac
import json
import os
import time
from dataclasses import asdict, replace
from datetime import datetime
from pathlib import Path
from types import MappingProxyType
from typing import Any, Callable

from .audit import AuditBundle, AuditStore
from .capture import CapturedExchange, capture_and_dispose
from .models import (
    MutationApproval,
    MutationStep,
    ScopedReadManifest,
    VerificationError,
    required_gates,
)


# The executable evidence manifest is the only authorization source. It has
# no OP18 entries until evidence declares them, so all wrapper calls stop.
EXECUTABLE_GATE_MANIFEST = MappingProxyType({})


def _require_executable_gate(gate: str) -> None:
    if EXECUTABLE_GATE_MANIFEST.get(gate) is not True:
        raise VerificationError("unmet gate " + gate)


def _observer_value(value: Any) -> bool | None:
    """Return only the closed observer value schema safe for immutable audit."""
    if type(value) is bool or value is None:
        return value
    return None


def _approval_bytes(approval: MutationApproval) -> bytes:
    return json.dumps(
        approval.canonical_dict(), sort_keys=True, separators=(",", ":")
    ).encode("utf-8")


def sign_fixture_approval(
    approval: MutationApproval, fixture_key: bytes
) -> MutationApproval:
    """Sign a sanitized fixture approval; this is not a live owner-approval issuer."""
    if not fixture_key:
        raise VerificationError("fixture signing key must not be empty")
    signature = hmac.new(fixture_key, _approval_bytes(approval), hashlib.sha256).hexdigest()
    return replace(approval, signature=signature)


def _verify_approval(approval: MutationApproval, key: bytes) -> None:
    expected = hmac.new(key, _approval_bytes(approval), hashlib.sha256).hexdigest()
    if not approval.signature or not hmac.compare_digest(approval.signature, expected):
        raise VerificationError("approval signature is invalid or approval was tampered")


class ApprovalUseRegistry:
    """Atomically consumes a run ID once, including after an ambiguous outcome."""

    def __init__(self, root: Path) -> None:
        self.root = root

    def claim(self, run_id: str, signature: str) -> None:
        self.root.mkdir(parents=True, exist_ok=True, mode=0o700)
        os.chmod(self.root, 0o700)
        path = self.root / f"{run_id}.used"
        try:
            descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o400)
        except FileExistsError as error:
            raise VerificationError("approval was already used") from error
        fingerprint = hashlib.sha256(signature.encode("ascii")).hexdigest().encode("ascii")
        try:
            with os.fdopen(descriptor, "wb") as stream:
                stream.write(fingerprint + b"\n")
                stream.flush()
                os.fsync(stream.fileno())
            os.chmod(path, 0o400)
        except BaseException:
            try:
                path.unlink()
            except OSError:
                pass
            raise


class ReadWrapper:
    """Runs one manifest-scoped read without granting mutation capability."""

    def __init__(
        self,
        manifest: ScopedReadManifest,
        gate_states: dict[str, bool],
        audit_store: AuditStore,
        raw_capture_root: Path,
    ) -> None:
        self.manifest = manifest
        self.gate_states = dict(gate_states)
        self.audit_store = audit_store
        self.raw_capture_root = raw_capture_root

    def run(
        self,
        operation_id: str,
        resource_alias: str,
        read: Callable[[], CapturedExchange],
    ) -> AuditBundle:
        if (
            operation_id not in self.manifest.allowed_operation_ids
            or resource_alias not in self.manifest.allowed_resource_aliases
        ):
            raise VerificationError("read is outside manifest scope")
        for gate in ("OP18-READ-WRAPPER", "OP18-RAW-CAPTURE-DISPOSAL"):
            _require_executable_gate(gate)
        started = time.monotonic_ns()
        exchange = read()
        elapsed = time.monotonic_ns() - started
        if exchange.outcome != "accepted":
            raise VerificationError("read did not return an accepted response")
        projected = exchange.response
        if isinstance(exchange.response, dict):
            projected = {
                key: exchange.response[key]
                for key in self.manifest.projection_fields
                if key in exchange.response
            }
        captured, proof = capture_and_dispose(
            self.raw_capture_root,
            self.manifest.run_id,
            1,
            CapturedExchange(
                outcome=exchange.outcome,
                status=exchange.status,
                discriminator=exchange.discriminator,
                request=exchange.request,
                response=projected,
            ),
        )
        captured["elapsed_monotonic_ns"] = elapsed
        payload = {
            "lane": "read",
            "run_id": self.manifest.run_id,
            "issue_url": self.manifest.issue_url,
            "environment": self.manifest.environment,
            "fixture_tag": self.manifest.fixture_tag,
            "executable_commit": self.manifest.executable_commit,
            "contract_revision": self.manifest.contract_revision,
            "tool_version": self.manifest.tool_version,
            "read_principal_alias": self.manifest.read_principal_alias,
            "operation_id": operation_id,
            "resource_alias": resource_alias,
            "projection_fields": list(self.manifest.projection_fields),
            "operation_log": [captured],
            "observer_assertions": [],
            "disposal_proofs": [proof],
            "retention": None,
            "terminal_status": "restored",
        }
        return self.audit_store.write(self.manifest.run_id, payload)


class MutationWrapper:
    """Consumes one approval and performs each approved mutation at most once."""

    def __init__(
        self,
        *,
        approval: MutationApproval,
        gate_states: dict[str, bool],
        approval_key: bytes,
        use_registry: ApprovalUseRegistry,
        audit_store: AuditStore,
        raw_capture_root: Path,
        now: Callable[[], datetime],
    ) -> None:
        self.approval = approval
        self.gate_states = dict(gate_states)
        self.approval_key = approval_key
        self.use_registry = use_registry
        self.audit_store = audit_store
        self.raw_capture_root = raw_capture_root
        self.now = now

    def run(
        self,
        rendered_operation_digest: str,
        preflight: Callable[[], bool],
        dispatch: Callable[[MutationStep], CapturedExchange],
        observer_factory: Callable[[], Callable[[str], dict[str, Any]]],
    ) -> AuditBundle:
        approval = self.approval
        _verify_approval(approval, self.approval_key)
        current = self.now()
        if current < approval.starts_at:
            raise VerificationError("approval window has not started")
        if current >= approval.expires_at:
            raise VerificationError("approval expired")
        if approval.retention is not None:
            if approval.retention.retain_until < approval.expires_at:
                raise VerificationError("retention deadline must cover approval window")
            if current >= approval.retention.retain_until:
                raise VerificationError("retention deadline is stale")
        if not hmac.compare_digest(
            rendered_operation_digest, approval.rendered_operation_digest
        ):
            raise VerificationError("rendered operation differs from approval")
        for gate in required_gates(approval):
            _require_executable_gate(gate)
        self.use_registry.claim(approval.run_id, approval.signature)
        if preflight() is not True:
            raise VerificationError("preflight stop condition failed")
        current = self.now()
        if current >= approval.expires_at:
            raise VerificationError("approval expired during preflight")
        if approval.retention is not None and current >= approval.retention.retain_until:
            raise VerificationError("retention deadline became stale during preflight")

        operation_log: list[dict[str, Any]] = []
        disposal_proofs: list[dict[str, Any]] = []
        observed: list[dict[str, Any]] = []
        attempts: dict[str, int] = {}

        def perform(step: MutationStep) -> CapturedExchange:
            attempts[step.operation_id] = attempts.get(step.operation_id, 0) + 1
            if attempts[step.operation_id] != 1:
                raise VerificationError("mutation dispatch counter exceeded one")
            started = time.monotonic_ns()
            try:
                exchange = dispatch(step)
            except BaseException:
                exchange = CapturedExchange(
                    outcome="ambiguous",
                    status=None,
                    discriminator="DISPATCH_EXCEPTION",
                    request=None,
                    response=None,
                )
            elapsed = time.monotonic_ns() - started
            capture, proof = capture_and_dispose(
                self.raw_capture_root,
                approval.run_id,
                len(operation_log) + 1,
                exchange,
            )
            capture.update(
                {
                    "phase": step.phase,
                    "operation_id": step.operation_id,
                    "attempt": attempts[step.operation_id],
                    "dispatch_began": True,
                    "elapsed_monotonic_ns": elapsed,
                }
            )
            operation_log.append(capture)
            disposal_proofs.append(proof)
            return exchange

        def observe(phase: str) -> tuple[bool, dict[str, Any]]:
            try:
                facts = observer_factory()(phase)
            except Exception:
                observed.append(
                    {
                        "phase": phase,
                        "name": "observer-unavailable",
                        "expected": None,
                        "actual": None,
                        "passed": False,
                    }
                )
                return False, {}
            if not isinstance(facts, dict):
                observed.append(
                    {
                        "phase": phase,
                        "name": "observer-unavailable",
                        "expected": None,
                        "actual": None,
                        "passed": False,
                    }
                )
                return False, {}
            passed = True
            for assertion in approval.observer_assertions:
                if assertion.phase != phase:
                    continue
                actual = _observer_value(facts.get(assertion.name))
                matched = assertion.name in facts and actual == assertion.expected
                observed.append(
                    {
                        "phase": phase,
                        "name": assertion.name,
                        "expected": assertion.expected,
                        "actual": actual,
                        "passed": matched,
                    }
                )
                passed = passed and matched
            return passed, facts

        before_required = any(
            assertion.phase == "before" for assertion in approval.observer_assertions
        )
        if before_required:
            before_ok, _ = observe("before")
            if not before_ok:
                raise VerificationError("preflight observer assertion failed")

        for step in (item for item in approval.steps if item.phase == "setup"):
            if self.now() >= approval.expires_at:
                if operation_log:
                    return self._finish(
                        "ambiguous-frozen", operation_log, observed, disposal_proofs
                    )
                raise VerificationError("approval expired before mutation dispatch")
            if perform(step).outcome != "accepted":
                return self._finish(
                    "ambiguous-frozen", operation_log, observed, disposal_proofs
                )

        primary = approval.primary_step
        if self.now() >= approval.expires_at:
            if operation_log:
                return self._finish(
                    "ambiguous-frozen", operation_log, observed, disposal_proofs
                )
            raise VerificationError("approval expired before mutation dispatch")
        primary_result = perform(primary)
        after_ok, after_facts = observe("after")
        if primary_result.outcome == "ambiguous" and not after_facts.get(
            "mutation_observed", False
        ):
            return self._finish(
                "ambiguous-frozen", operation_log, observed, disposal_proofs
            )
        if primary_result.outcome == "rejected" and not after_facts.get(
            "mutation_observed", False
        ):
            return self._finish(
                "ambiguous-frozen", operation_log, observed, disposal_proofs
            )
        if not after_ok:
            return self._finish(
                "ambiguous-frozen", operation_log, observed, disposal_proofs
            )

        cleanup_steps = [item for item in approval.steps if item.phase == "cleanup"]
        if not cleanup_steps:
            return self._finish(
                "approved-retained", operation_log, observed, disposal_proofs
            )
        for step in cleanup_steps:
            if self.now() >= approval.expires_at:
                return self._finish(
                    "cleanup-failed", operation_log, observed, disposal_proofs
                )
            if perform(step).outcome != "accepted":
                return self._finish(
                    "cleanup-failed", operation_log, observed, disposal_proofs
                )
        terminal_ok, terminal_facts = observe("terminal")
        terminal = "deleted" if terminal_facts.get("deleted") is True else "restored"
        if not terminal_ok:
            terminal = "cleanup-failed"
        return self._finish(terminal, operation_log, observed, disposal_proofs)

    def _finish(
        self,
        terminal_status: str,
        operation_log: list[dict[str, Any]],
        observed: list[dict[str, Any]],
        disposal_proofs: list[dict[str, Any]],
    ) -> AuditBundle:
        approval = self.approval
        retention = approval.retention.canonical_dict() if approval.retention else None
        payload = {
            "lane": "mutation",
            "run_id": approval.run_id,
            "issue_url": approval.issue_url,
            "environment": approval.environment,
            "fixture_tag": approval.fixture_tag,
            "executable_commit": approval.executable_commit,
            "contract_revision": approval.contract_revision,
            "tool_version": approval.tool_version,
            "approval": approval.canonical_dict(include_signature=True),
            "approval_fingerprint": hashlib.sha256(
                approval.signature.encode("ascii")
            ).hexdigest(),
            "applicable_gates": list(required_gates(approval)),
            "operation_log": operation_log,
            "observer_assertions": observed,
            "disposal_proofs": disposal_proofs,
            "retention": retention,
            "terminal_status": terminal_status,
        }
        return self.audit_store.write(approval.run_id, payload)
