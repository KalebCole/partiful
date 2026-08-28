"""Immutable, integrity-marked verification audit bundles."""
from __future__ import annotations

import hashlib
import json
import os
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Optional

from .models import VerificationError


@dataclass(frozen=True)
class AuditBundle:
    path: Path
    terminal_status: str
    operation_log: tuple[dict[str, Any], ...]
    observer_assertions: tuple[dict[str, Any], ...]
    disposal_proofs: tuple[dict[str, Any], ...]
    retention: Optional[dict[str, Any]]


class AuditStore:
    """Writes one create-only, read-only JSON audit bundle per run ID."""

    def __init__(self, root: Path) -> None:
        self.root = root

    def write(self, run_id: str, payload: dict[str, Any]) -> AuditBundle:
        self.root.mkdir(parents=True, exist_ok=True, mode=0o700)
        os.chmod(self.root, 0o700)
        path = self.root / f"{run_id}.json"
        body = json.dumps(payload, sort_keys=True, separators=(",", ":"))
        stored = dict(payload)
        stored["bundle_sha256"] = hashlib.sha256(body.encode("utf-8")).hexdigest()
        encoded = (json.dumps(stored, sort_keys=True, indent=2) + "\n").encode("utf-8")
        try:
            descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o400)
        except FileExistsError as error:
            raise VerificationError("immutable audit bundle already exists") from error
        try:
            with os.fdopen(descriptor, "wb") as stream:
                stream.write(encoded)
                stream.flush()
                os.fsync(stream.fileno())
            os.chmod(path, 0o400)
        except BaseException:
            try:
                path.unlink()
            except OSError:
                pass
            raise
        return AuditBundle(
            path=path,
            terminal_status=str(payload["terminal_status"]),
            operation_log=tuple(payload.get("operation_log", ())),
            observer_assertions=tuple(payload.get("observer_assertions", ())),
            disposal_proofs=tuple(payload.get("disposal_proofs", ())),
            retention=payload.get("retention"),
        )
