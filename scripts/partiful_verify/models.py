"""Validated manifests and approval records for controlled verification."""
from __future__ import annotations

import re
from dataclasses import asdict, dataclass
from datetime import datetime, timezone
from typing import Any, Optional


_SAFE_ALIAS = re.compile(r"^[a-z][a-z0-9_-]{2,63}$")
_FIXTURE_TAG = re.compile(
    r"^partiful-verify/[a-z][a-z0-9.-]*/[0-9]{8}/[0-9a-f]{32}$"
)
_SHA = re.compile(r"^[0-9a-f]{40}$")
_RUN_ID = re.compile(r"^[0-9a-f]{32}$")
_DIGEST = re.compile(r"^[0-9a-f]{64}$")
_OPERATION = re.compile(r"^[A-Za-z][A-Za-z0-9]*$")
_PUBLIC_OPERATION = re.compile(r"^[a-z][a-z0-9-]*\.[a-z][a-z0-9.-]*$")
_OBSERVER_ASSERTION_NAMES = frozenset(
    {"mutation_observed", "restored", "deleted"}
)

READ_OPERATION_IDS = frozenset(
    {
        "getEventInfo",
        "getContacts",
        "getGuests",
        "getCurrentGuest",
        "getMyUpcomingEventsForHomePage",
        "getMyPastEventsForHomePage",
        "firestoreGetEvent",
        "firestoreGetGuest",
        "getPosterCatalog",
    }
)

ACCOUNT_ROSTER_OPERATIONS = frozenset(
    {
        "cancelEvent",
        "addInvitedGuestsAsHost",
        "createCohostRequest",
        "deleteCohostRequest",
        "removeCohost",
        "generateEventCohostLink",
        "revokeEventCohostLink",
        "createTextBlast",
    }
)
RECIPIENT_OBSERVATION_OPERATIONS = frozenset(
    {"cancelEvent", "addInvitedGuestsAsHost", "createCohostRequest", "createTextBlast"}
)
PUBLIC_PRIMARY_OPERATIONS = {
    "events.create": frozenset({"createEvent"}),
    "events.update": frozenset({"firestorePatchEvent"}),
    "events.cancel": frozenset({"cancelEvent"}),
    "guests.invite": frozenset({"addInvitedGuestsAsHost"}),
    "rsvp.set": frozenset({"addGuest", "markEventInterest"}),
    "cohosts.invite": frozenset({"createCohostRequest"}),
    "cohosts.revoke-invite": frozenset({"deleteCohostRequest"}),
    "cohosts.remove": frozenset({"removeCohost"}),
    "cohosts.link.create": frozenset({"generateEventCohostLink"}),
    "cohosts.link.revoke": frozenset({"revokeEventCohostLink"}),
    "blasts.send": frozenset({"createTextBlast"}),
}
MUTATION_OPERATION_IDS = frozenset(
    operation
    for operations in PUBLIC_PRIMARY_OPERATIONS.values()
    for operation in operations
)


class VerificationError(RuntimeError):
    """A fail-closed verification control rejected the run."""


def require_safe_alias(alias: str) -> None:
    if not isinstance(alias, str) or not _SAFE_ALIAS.fullmatch(alias):
        raise VerificationError(
            "safe alias must contain only lowercase letters, digits, hyphens, or underscores"
        )


def _validate_run_fields(
    run_id: str, issue_url: str, environment: str, fixture_tag: str
) -> None:
    if not _RUN_ID.fullmatch(run_id):
        raise VerificationError("run ID must be 128-bit lowercase hex")
    if not issue_url.startswith("https://github.com/KalebCole/partiful/issues/"):
        raise VerificationError("issue URL must name a Partiful issue")
    if environment != "production":
        raise VerificationError(
            "environment must be production until another environment is evidenced"
        )
    if not _FIXTURE_TAG.fullmatch(fixture_tag):
        raise VerificationError(
            "fixture tag must use the partiful-verify operation/date/run format"
        )
    if fixture_tag.rsplit("/", 1)[-1] != run_id:
        raise VerificationError("fixture tag must end with the exact run ID")


@dataclass(frozen=True)
class ScopedReadManifest:
    """The exact read capability granted to one verification run."""

    run_id: str
    issue_url: str
    environment: str
    read_principal_alias: str
    fixture_tag: str
    allowed_operation_ids: tuple[str, ...]
    allowed_resource_aliases: tuple[str, ...]
    projection_fields: tuple[str, ...]
    executable_commit: str
    contract_revision: str
    tool_version: str

    def __post_init__(self) -> None:
        _validate_run_fields(
            self.run_id, self.issue_url, self.environment, self.fixture_tag
        )
        require_safe_alias(self.read_principal_alias)
        for alias in self.allowed_resource_aliases:
            require_safe_alias(alias)
        if not self.allowed_operation_ids or not self.allowed_resource_aliases:
            raise VerificationError("read scope must not be empty")
        if len(set(self.allowed_operation_ids)) != len(self.allowed_operation_ids):
            raise VerificationError("read operation scope must be unique")
        if any(not _OPERATION.fullmatch(item) for item in self.allowed_operation_ids):
            raise VerificationError("read operation ID is invalid")
        if any(item not in READ_OPERATION_IDS for item in self.allowed_operation_ids):
            raise VerificationError("manifest contains a non-read-only operation")
        if not self.projection_fields or any(
            not _SAFE_ALIAS.fullmatch(item.replace(".", "-"))
            for item in self.projection_fields
        ):
            raise VerificationError("read projection must contain safe fields")
        if not _SHA.fullmatch(self.executable_commit):
            raise VerificationError("executable commit must be a 40-character SHA")
        if not self.contract_revision or not self.tool_version:
            raise VerificationError("contract revision and tool version are required")


@dataclass(frozen=True)
class MutationStep:
    phase: str
    operation_id: str

    def __post_init__(self) -> None:
        if self.phase not in {"setup", "primary", "cleanup"}:
            raise VerificationError("mutation step phase is invalid")
        if not _OPERATION.fullmatch(self.operation_id):
            raise VerificationError("mutation operation ID is invalid")


@dataclass(frozen=True)
class ObserverAssertion:
    phase: str
    name: str
    expected: Any

    def __post_init__(self) -> None:
        if self.phase not in {"before", "after", "terminal"}:
            raise VerificationError("observer assertion phase is invalid")
        if self.name not in _OBSERVER_ASSERTION_NAMES:
            raise VerificationError("observer assertion name is not allowlisted")
        if not isinstance(self.expected, (bool, type(None))):
            raise VerificationError("observer assertion expected value must be boolean or null")


@dataclass(frozen=True)
class RetentionPlan:
    owner_alias: str
    terminal_state: str
    retain_until: datetime
    review_url: str

    def __post_init__(self) -> None:
        require_safe_alias(self.owner_alias)
        require_safe_alias(self.terminal_state)
        if self.retain_until.tzinfo is None:
            raise VerificationError("retention deadline must be timezone-aware")
        if self.retain_until <= datetime.now(timezone.utc):
            raise VerificationError("retention deadline is stale")
        if not self.review_url.startswith(
            "https://github.com/KalebCole/partiful/issues/"
        ):
            raise VerificationError("retention review must name a Partiful issue")

    def canonical_dict(self) -> dict[str, Any]:
        return {
            "owner_alias": self.owner_alias,
            "terminal_state": self.terminal_state,
            "retain_until": self.retain_until.isoformat(),
            "review_url": self.review_url,
        }


@dataclass(frozen=True)
class MutationApproval:
    """One tamper-evident approval for one bounded mutation run."""

    run_id: str
    issue_url: str
    environment: str
    starts_at: datetime
    expires_at: datetime
    public_operation: str
    account_aliases: tuple[tuple[str, str], ...]
    fixture_tag: str
    steps: tuple[MutationStep, ...]
    observer_assertions: tuple[ObserverAssertion, ...]
    rendered_operation_digest: str
    executable_commit: str
    contract_revision: str
    tool_version: str
    retention: Optional[RetentionPlan]
    signature: str

    def __post_init__(self) -> None:
        _validate_run_fields(
            self.run_id, self.issue_url, self.environment, self.fixture_tag
        )
        if self.starts_at.tzinfo is None or self.expires_at.tzinfo is None:
            raise VerificationError("approval window must be timezone-aware")
        if self.starts_at >= self.expires_at:
            raise VerificationError("approval window is invalid")
        if not _PUBLIC_OPERATION.fullmatch(self.public_operation):
            raise VerificationError("public operation is invalid")
        roles: list[str] = []
        for role, alias in self.account_aliases:
            if role not in {"read_principal", "fixture_owner", "test_recipient"}:
                raise VerificationError("account role is invalid")
            require_safe_alias(alias)
            roles.append(role)
        if "fixture_owner" not in roles or len(set(roles)) != len(roles):
            raise VerificationError("approval requires one unique fixture_owner alias")
        if not self.steps:
            raise VerificationError("approval must contain mutation steps")
        primary_positions = [
            index for index, step in enumerate(self.steps) if step.phase == "primary"
        ]
        if len(primary_positions) != 1:
            raise VerificationError("approval requires exactly one primary mutation")
        primary_position = primary_positions[0]
        if any(step.phase == "setup" for step in self.steps[primary_position + 1 :]):
            raise VerificationError("setup mutation must precede the primary mutation")
        if any(step.phase == "cleanup" for step in self.steps[:primary_position]):
            raise VerificationError("cleanup mutation must follow the primary mutation")
        operation_ids = [step.operation_id for step in self.steps]
        unsupported = sorted(set(operation_ids) - MUTATION_OPERATION_IDS)
        if unsupported:
            raise VerificationError("unsupported mutation operation: " + unsupported[0])
        if len(set(operation_ids)) != len(operation_ids):
            raise VerificationError("each mutation operation ID may dispatch at most once")
        primary = self.steps[primary_position].operation_id
        if primary not in PUBLIC_PRIMARY_OPERATIONS.get(self.public_operation, frozenset()):
            raise VerificationError(
                "private primary operation does not match the approved public operation"
            )
        fixture_operation = self.fixture_tag.split("/")[1]
        if fixture_operation != self.public_operation:
            raise VerificationError("fixture tag operation does not match public operation")
        if primary in ACCOUNT_ROSTER_OPERATIONS and "test_recipient" not in roles:
            raise VerificationError("account roster operation requires a test_recipient alias")
        if not _DIGEST.fullmatch(self.rendered_operation_digest):
            raise VerificationError("rendered operation digest must be SHA-256")
        if not _SHA.fullmatch(self.executable_commit):
            raise VerificationError("executable commit must be a 40-character SHA")
        if not self.contract_revision or not self.tool_version:
            raise VerificationError("contract revision and tool version are required")
        assertion_keys = [
            (assertion.phase, assertion.name) for assertion in self.observer_assertions
        ]
        if len(set(assertion_keys)) != len(assertion_keys):
            raise VerificationError("observer assertions must be unique")
        cleanup = any(step.phase == "cleanup" for step in self.steps)
        if cleanup and not any(
            assertion.phase == "terminal" for assertion in self.observer_assertions
        ):
            raise VerificationError("cleanup requires a terminal observer assertion")
        if not cleanup and self.retention is None:
            raise VerificationError(
                "an irreversible effect requires an approved tracked-retention plan"
            )
        if self.signature and not _DIGEST.fullmatch(self.signature):
            raise VerificationError("approval signature must be a SHA-256 HMAC")

    @property
    def primary_step(self) -> MutationStep:
        return next(step for step in self.steps if step.phase == "primary")

    def as_dict(self) -> dict[str, Any]:
        """Return constructor-compatible fields, including the current signature."""
        return {
            "run_id": self.run_id,
            "issue_url": self.issue_url,
            "environment": self.environment,
            "starts_at": self.starts_at,
            "expires_at": self.expires_at,
            "public_operation": self.public_operation,
            "account_aliases": self.account_aliases,
            "fixture_tag": self.fixture_tag,
            "steps": self.steps,
            "observer_assertions": self.observer_assertions,
            "rendered_operation_digest": self.rendered_operation_digest,
            "executable_commit": self.executable_commit,
            "contract_revision": self.contract_revision,
            "tool_version": self.tool_version,
            "retention": self.retention,
            "signature": self.signature,
        }

    def canonical_dict(self, include_signature: bool = False) -> dict[str, Any]:
        result: dict[str, Any] = {
            "run_id": self.run_id,
            "issue_url": self.issue_url,
            "environment": self.environment,
            "starts_at": self.starts_at.isoformat(),
            "expires_at": self.expires_at.isoformat(),
            "public_operation": self.public_operation,
            "account_aliases": [list(item) for item in self.account_aliases],
            "fixture_tag": self.fixture_tag,
            "steps": [asdict(step) for step in self.steps],
            "observer_assertions": [asdict(item) for item in self.observer_assertions],
            "rendered_operation_digest": self.rendered_operation_digest,
            "executable_commit": self.executable_commit,
            "contract_revision": self.contract_revision,
            "tool_version": self.tool_version,
            "retention": self.retention.canonical_dict() if self.retention else None,
        }
        if include_signature:
            result["signature"] = self.signature
        return result


def required_gates(approval: MutationApproval) -> tuple[str, ...]:
    primary = approval.primary_step.operation_id
    gates = {
        "OP18-MUTATION-WRAPPER",
        "OP18-RAW-CAPTURE-DISPOSAL",
        "OP18-CLEANUP:" + primary,
    }
    if primary in ACCOUNT_ROSTER_OPERATIONS:
        gates.add("OP18-ACCOUNT-ROSTER")
    if primary in RECIPIENT_OBSERVATION_OPERATIONS:
        gates.add("OP18-RECIPIENT-OBSERVATION:" + primary)
    return tuple(sorted(gates))
