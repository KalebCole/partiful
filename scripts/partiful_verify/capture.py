"""Access-controlled raw capture and pre-persistence structural redaction."""
from __future__ import annotations

import json
import os
import re
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Optional

from .models import VerificationError


_DISCRIMINATOR = re.compile(r"^[A-Za-z0-9_.-]{1,64}$")
_STRUCTURAL_KEY = re.compile(r"^[A-Za-z][A-Za-z0-9_.-]{0,63}$")
_SENSITIVE_KEYS = (
    "authorization",
    "cookie",
    "credential",
    "token",
    "phone",
    "contact",
    "name",
    "message",
    "body",
    "url",
    "rawid",
    "userid",
    "eventid",
    "guestid",
)


def _safe_structural_key(value: Any) -> bool:
    key = str(value)
    normalized = re.sub(r"[^a-z0-9]", "", key.lower())
    return bool(_STRUCTURAL_KEY.fullmatch(key)) and not any(
        marker in key.lower() or marker in normalized for marker in _SENSITIVE_KEYS
    )


@dataclass(frozen=True)
class CapturedExchange:
    outcome: str
    status: Optional[int]
    discriminator: str
    request: Any
    response: Any

    def __post_init__(self) -> None:
        if self.outcome not in {"accepted", "rejected", "ambiguous"}:
            raise VerificationError("capture outcome is invalid")
        if self.status is not None and not 100 <= self.status <= 599:
            raise VerificationError("capture status is invalid")
        if not _DISCRIMINATOR.fullmatch(self.discriminator):
            raise VerificationError("capture discriminator is invalid")
        try:
            json.dumps({"request": self.request, "response": self.response})
        except (TypeError, ValueError) as error:
            raise VerificationError("raw capture must be JSON-compatible") from error


def structural_capture(value: Any, key: str = "") -> Any:
    """Retain structure and scalar types while excluding private values."""
    lowered = key.lower()
    if key and any(marker in lowered for marker in _SENSITIVE_KEYS):
        return "<redacted>"
    if isinstance(value, dict):
        return {
            str(item_key): structural_capture(item_value, str(item_key))
            for item_key, item_value in sorted(value.items(), key=lambda item: str(item[0]))
            if _safe_structural_key(item_key)
        }
    if isinstance(value, list):
        return [structural_capture(item) for item in value]
    if value is None:
        return "<null>"
    if isinstance(value, bool):
        return "<boolean>"
    if isinstance(value, int):
        return "<integer>"
    if isinstance(value, float):
        return "<number>"
    return "<string>"


def capture_and_dispose(
    root: Path, run_id: str, capture_index: int, exchange: CapturedExchange
) -> tuple[dict[str, Any], dict[str, Any]]:
    """Derive a structural record, then delete the mode-0600 raw capture."""
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    os.chmod(root, 0o700)
    path = root / f"{run_id}-{capture_index}.raw.json"
    try:
        raw = json.dumps(
            {"request": exchange.request, "response": exchange.response},
            sort_keys=True,
            separators=(",", ":"),
        ).encode("utf-8")
    except (TypeError, ValueError) as error:
        raise VerificationError("raw capture serialization failed") from error
    flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL
    try:
        descriptor = os.open(path, flags, 0o600)
    except FileExistsError as error:
        raise VerificationError("raw capture path already exists") from error

    deleted = False
    try:
        with os.fdopen(descriptor, "wb") as stream:
            stream.write(raw)
            stream.flush()
            os.fsync(stream.fileno())
        os.chmod(path, 0o600)
        record = {
            "outcome": exchange.outcome,
            "status": exchange.status,
            "discriminator": exchange.discriminator,
            "request_shape": structural_capture(exchange.request),
            "response_shape": structural_capture(exchange.response),
        }
    finally:
        try:
            path.unlink()
            deleted = not path.exists()
        except OSError as error:
            raise VerificationError("raw capture disposal failed") from error
    if not deleted:
        raise VerificationError("raw capture disposal failed")
    proof = {
        "capture_index": capture_index,
        "access_mode": "0600",
        "redacted_before_audit": True,
        "deleted": True,
        "redaction_report": {
            "passed": True,
            "policy": "structural-placeholders-v1",
        },
    }
    return record, proof
