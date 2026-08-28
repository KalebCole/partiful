from __future__ import annotations

import json
import os
import tempfile
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Optional
from unittest.mock import patch

from scripts.partiful_verify import (
    ApprovalUseRegistry,
    AuditStore,
    CapturedExchange,
    MutationApproval,
    MutationStep,
    MutationWrapper,
    ObserverAssertion,
    ReadWrapper,
    RetentionPlan,
    ScopedReadManifest,
    VerificationError,
    required_gates,
    sign_fixture_approval,
)


NOW = datetime(2026, 8, 28, 12, 0, tzinfo=timezone.utc)
FIXTURE_KEY = b"fixture-only-signing-key"
FIXTURE_EXECUTABLE_GATES = {
    "OP18-READ-WRAPPER": True,
    "OP18-RAW-CAPTURE-DISPOSAL": True,
    "OP18-MUTATION-WRAPPER": True,
    "OP18-ACCOUNT-ROSTER": True,
    "OP18-CLEANUP:createEvent": True,
    "OP18-CLEANUP:addInvitedGuestsAsHost": True,
    "OP18-RECIPIENT-OBSERVATION:addInvitedGuestsAsHost": True,
}


def read_manifest() -> ScopedReadManifest:
    return ScopedReadManifest(
        run_id="0" * 32,
        issue_url="https://github.com/KalebCole/partiful/issues/43",
        environment="production",
        read_principal_alias="read-principal",
        fixture_tag="partiful-verify/events.get/20260828/" + "0" * 32,
        allowed_operation_ids=("getEventInfo",),
        allowed_resource_aliases=("fixture-event",),
        projection_fields=("title", "state"),
        executable_commit="a" * 40,
        contract_revision="2026-08-12.7",
        tool_version="fixture-test",
    )


def mutation_approval(
    *,
    run_id: str = "2" * 32,
    expires_at: datetime = NOW + timedelta(minutes=10),
    steps: tuple[MutationStep, ...] = (
        MutationStep("primary", "createEvent"),
        MutationStep("cleanup", "cancelEvent"),
    ),
    assertions: tuple[ObserverAssertion, ...] = (
        ObserverAssertion("after", "mutation_observed", True),
        ObserverAssertion("terminal", "restored", True),
    ),
    accounts: tuple[tuple[str, str], ...] = (("fixture_owner", "fixture-owner"),),
    retention: Optional[RetentionPlan] = None,
    public_operation: Optional[str] = None,
) -> MutationApproval:
    primary = next(step.operation_id for step in steps if step.phase == "primary")
    public_operations = {
        "createEvent": "events.create",
        "firestorePatchEvent": "events.update",
        "cancelEvent": "events.cancel",
        "addInvitedGuestsAsHost": "guests.invite",
        "addGuest": "rsvp.set",
        "markEventInterest": "rsvp.set",
        "createCohostRequest": "cohosts.invite",
        "deleteCohostRequest": "cohosts.revoke-invite",
        "removeCohost": "cohosts.remove",
        "generateEventCohostLink": "cohosts.link.create",
        "revokeEventCohostLink": "cohosts.link.revoke",
        "createTextBlast": "blasts.send",
    }
    selected_public_operation = public_operation or public_operations[primary]
    unsigned = MutationApproval(
        run_id=run_id,
        issue_url="https://github.com/KalebCole/partiful/issues/43",
        environment="production",
        starts_at=NOW - timedelta(minutes=1),
        expires_at=expires_at,
        public_operation=selected_public_operation,
        account_aliases=accounts,
        fixture_tag=f"partiful-verify/{selected_public_operation}/20260828/{run_id}",
        steps=steps,
        observer_assertions=assertions,
        rendered_operation_digest="b" * 64,
        executable_commit="a" * 40,
        contract_revision="2026-08-12.7",
        tool_version="fixture-test",
        retention=retention,
        signature="",
    )
    return sign_fixture_approval(unsigned, FIXTURE_KEY)


def success_exchange(operation_id: str) -> CapturedExchange:
    return CapturedExchange(
        outcome="accepted",
        status=200,
        discriminator="OK",
        request={
            "operation": operation_id,
            "authorization": "secret-token",
            "Private Person": "private-value",
        },
        response={"result": {"rawId": "private-id", "title": "Private title"}},
    )


class ScopedReadManifestTests(unittest.TestCase):
    def setUp(self) -> None:
        self.executable_manifest = patch(
            "scripts.partiful_verify.wrappers.EXECUTABLE_GATE_MANIFEST",
            FIXTURE_EXECUTABLE_GATES,
        )
        self.executable_manifest.start()

    def tearDown(self) -> None:
        self.executable_manifest.stop()

    def test_read_manifest_rejects_mutation_operations(self) -> None:
        with self.assertRaisesRegex(VerificationError, "read-only operation"):
            ScopedReadManifest(
                **{
                    **read_manifest().__dict__,
                    "allowed_operation_ids": ("createEvent",),
                }
            )

    def test_rejects_unsafe_alias_and_fixture_tag(self) -> None:
        with self.assertRaisesRegex(VerificationError, "safe alias"):
            ScopedReadManifest(
                run_id="0" * 32,
                issue_url="https://github.com/KalebCole/partiful/issues/43",
                environment="production",
                read_principal_alias="person@example.com",
                fixture_tag="partiful-verify/events.get/20260828/" + "0" * 32,
                allowed_operation_ids=("getEventInfo",),
                allowed_resource_aliases=("fixture-event",),
                projection_fields=("title",),
                executable_commit="a" * 40,
                contract_revision="2026-08-12.7",
                tool_version="fixture-test",
            )

        with self.assertRaisesRegex(VerificationError, "fixture tag"):
            ScopedReadManifest(
                run_id="0" * 32,
                issue_url="https://github.com/KalebCole/partiful/issues/43",
                environment="production",
                read_principal_alias="read-principal",
                fixture_tag="ordinary-event-name",
                allowed_operation_ids=("getEventInfo",),
                allowed_resource_aliases=("fixture-event",),
                projection_fields=("title",),
                executable_commit="a" * 40,
                contract_revision="2026-08-12.7",
                tool_version="fixture-test",
            )

    def test_read_wrapper_scopes_before_call_and_requires_open_controls(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            calls: list[str] = []
            wrapper = ReadWrapper(
                read_manifest(),
                {"OP18-READ-WRAPPER": True, "OP18-RAW-CAPTURE-DISPOSAL": True},
                AuditStore(Path(tmp) / "audit"),
                Path(tmp) / "raw",
            )
            with self.assertRaisesRegex(VerificationError, "outside manifest scope"):
                wrapper.run(
                    "getContacts",
                    "fixture-event",
                    lambda: calls.append("called") or success_exchange("getContacts"),
                )
            self.assertEqual([], calls)

            blocked = ReadWrapper(
                read_manifest(),
                {"OP18-READ-WRAPPER": False, "OP18-RAW-CAPTURE-DISPOSAL": True},
                AuditStore(Path(tmp) / "blocked-audit"),
                Path(tmp) / "blocked-raw",
            )
            with patch(
                "scripts.partiful_verify.wrappers.EXECUTABLE_GATE_MANIFEST",
                {"OP18-READ-WRAPPER": False},
            ):
                with self.assertRaisesRegex(VerificationError, "OP18-READ-WRAPPER"):
                    blocked.run(
                        "getEventInfo",
                        "fixture-event",
                        lambda: calls.append("called") or success_exchange("getEventInfo"),
                    )
            self.assertEqual([], calls)

    def test_read_wrapper_redacts_before_immutable_audit_and_proves_disposal(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            wrapper = ReadWrapper(
                read_manifest(),
                {"OP18-READ-WRAPPER": True, "OP18-RAW-CAPTURE-DISPOSAL": True},
                AuditStore(root / "audit"),
                root / "raw",
            )
            bundle = wrapper.run(
                "getEventInfo",
                "fixture-event",
                lambda: success_exchange("getEventInfo"),
            )

            persisted = bundle.path.read_text(encoding="utf-8")
            self.assertNotIn("secret-token", persisted)
            self.assertNotIn("private-id", persisted)
            self.assertNotIn("Private title", persisted)
            self.assertNotIn("Private Person", persisted)
            self.assertNotIn('"authorization"', persisted)
            self.assertEqual([], list((root / "raw").iterdir()))
            self.assertTrue(bundle.disposal_proofs[0]["deleted"])
            self.assertTrue(bundle.disposal_proofs[0]["redaction_report"]["passed"])
            self.assertGreaterEqual(bundle.operation_log[0]["elapsed_monotonic_ns"], 0)
            self.assertEqual(0, os.stat(bundle.path).st_mode & 0o222)
            with self.assertRaisesRegex(VerificationError, "already exists"):
                wrapper.run(
                    "getEventInfo",
                    "fixture-event",
                    lambda: success_exchange("getEventInfo"),
                )


class MutationWrapperTests(unittest.TestCase):
    def setUp(self) -> None:
        self.executable_manifest = patch(
            "scripts.partiful_verify.wrappers.EXECUTABLE_GATE_MANIFEST",
            FIXTURE_EXECUTABLE_GATES,
        )
        self.executable_manifest.start()

    def tearDown(self) -> None:
        self.executable_manifest.stop()

    def _wrapper(
        self,
        root: Path,
        approval: MutationApproval,
        gates: Optional[dict[str, bool]] = None,
    ) -> MutationWrapper:
        return MutationWrapper(
            approval=approval,
            gate_states=(
                gates
                if gates is not None
                else {gate: True for gate in required_gates(approval)}
            ),
            approval_key=FIXTURE_KEY,
            use_registry=ApprovalUseRegistry(root / "uses"),
            audit_store=AuditStore(root / "audit"),
            raw_capture_root=root / "raw",
            now=lambda: NOW,
        )

    def test_expired_or_tampered_approval_causes_zero_dispatch(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            calls: list[str] = []
            expired = mutation_approval(expires_at=NOW - timedelta(seconds=1))
            with self.assertRaisesRegex(VerificationError, "expired"):
                self._wrapper(root / "expired", expired).run(
                    "b" * 64,
                    lambda: True,
                    lambda step: calls.append(step.operation_id) or success_exchange(step.operation_id),
                    lambda: lambda phase: {"mutation_observed": True, "restored": True},
                )

            valid = mutation_approval(run_id="3" * 32)
            tampered = MutationApproval(
                **{**valid.as_dict(), "rendered_operation_digest": "c" * 64}
            )
            with self.assertRaisesRegex(VerificationError, "signature"):
                self._wrapper(root / "tampered", tampered).run(
                    "b" * 64,
                    lambda: True,
                    lambda step: calls.append(step.operation_id) or success_exchange(step.operation_id),
                    lambda: lambda phase: {"mutation_observed": True, "restored": True},
                )
            self.assertEqual([], calls)

    def test_each_unmet_applicable_gate_causes_zero_dispatch(self) -> None:
        approval = mutation_approval(
            steps=(
                MutationStep("primary", "addInvitedGuestsAsHost"),
                MutationStep("cleanup", "cancelEvent"),
            ),
            accounts=(
                ("fixture_owner", "fixture-owner"),
                ("test_recipient", "test-recipient"),
            ),
        )
        expected = {
            "OP18-MUTATION-WRAPPER",
            "OP18-ACCOUNT-ROSTER",
            "OP18-RAW-CAPTURE-DISPOSAL",
            "OP18-CLEANUP:addInvitedGuestsAsHost",
            "OP18-RECIPIENT-OBSERVATION:addInvitedGuestsAsHost",
        }
        self.assertEqual(expected, set(required_gates(approval)))

        for index, missing in enumerate(sorted(expected)):
            with self.subTest(gate=missing), tempfile.TemporaryDirectory() as tmp:
                calls: list[str] = []
                gates = {gate: gate != missing for gate in expected}
                with patch(
                    "scripts.partiful_verify.wrappers.EXECUTABLE_GATE_MANIFEST", gates
                ):
                    with self.assertRaisesRegex(VerificationError, missing):
                        self._wrapper(Path(tmp), approval, gates).run(
                            "b" * 64,
                            lambda: True,
                            lambda step: calls.append(step.operation_id) or success_exchange(step.operation_id),
                            lambda: lambda phase: {"mutation_observed": True, "restored": True},
                        )
                self.assertEqual([], calls, index)

    def test_preflight_stop_causes_zero_dispatch(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            approval = mutation_approval()
            calls: list[str] = []
            with self.assertRaisesRegex(VerificationError, "preflight"):
                self._wrapper(root, approval).run(
                    "b" * 64,
                    lambda: False,
                    lambda step: calls.append(step.operation_id) or success_exchange(step.operation_id),
                    lambda: lambda phase: {},
                )
            with self.assertRaisesRegex(VerificationError, "already used"):
                self._wrapper(root, approval).run(
                    "b" * 64,
                    lambda: True,
                    lambda step: calls.append(step.operation_id) or success_exchange(step.operation_id),
                    lambda: lambda phase: {},
                )
            self.assertEqual([], calls)

    def test_approval_expiring_during_preflight_causes_zero_dispatch(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            approval = mutation_approval()
            calls: list[str] = []
            times = iter((NOW, approval.expires_at))
            wrapper = self._wrapper(Path(tmp), approval)
            wrapper.now = lambda: next(times)

            with self.assertRaisesRegex(VerificationError, "expired"):
                wrapper.run(
                    "b" * 64,
                    lambda: True,
                    lambda step: calls.append(step.operation_id)
                    or success_exchange(step.operation_id),
                    lambda: lambda phase: {},
                )
            self.assertEqual([], calls)

    def test_approval_expiry_stops_cleanup_dispatch(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            approval = mutation_approval()
            calls: list[str] = []
            times = iter((NOW, NOW, NOW, approval.expires_at))
            wrapper = self._wrapper(Path(tmp), approval)
            wrapper.now = lambda: next(times)

            bundle = wrapper.run(
                "b" * 64,
                lambda: True,
                lambda step: calls.append(step.operation_id)
                or success_exchange(step.operation_id),
                lambda: lambda phase: {"mutation_observed": True, "restored": True},
            )

            self.assertEqual("cleanup-failed", bundle.terminal_status)
            self.assertEqual(["createEvent"], calls)

    def test_approval_binds_operation_and_fixture_tag_to_run(self) -> None:
        with self.assertRaisesRegex(VerificationError, "does not match"):
            mutation_approval(public_operation="events.cancel")

        valid = mutation_approval()
        with self.assertRaisesRegex(VerificationError, "fixture tag.*run ID"):
            MutationApproval(
                **{
                    **valid.as_dict(),
                    "fixture_tag": "partiful-verify/events.create/20260828/" + "9" * 32,
                }
            )

        with self.assertRaisesRegex(VerificationError, "unsupported mutation"):
            MutationApproval(
                **{
                    **valid.as_dict(),
                    "steps": (
                        MutationStep("primary", "unknownMutation"),
                        MutationStep("cleanup", "cancelEvent"),
                    ),
                }
            )

    def test_ambiguous_response_is_never_replayed(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            approval = mutation_approval()
            calls: list[str] = []

            def ambiguous(step: MutationStep) -> CapturedExchange:
                calls.append(step.operation_id)
                return CapturedExchange(
                    outcome="ambiguous",
                    status=None,
                    discriminator="TIMEOUT",
                    request={"operation": step.operation_id},
                    response=None,
                )

            bundle = self._wrapper(root, approval).run(
                "b" * 64,
                lambda: True,
                ambiguous,
                lambda: lambda phase: {"mutation_observed": False},
            )
            self.assertEqual("ambiguous-frozen", bundle.terminal_status)
            self.assertEqual(["createEvent"], calls)

            with self.assertRaisesRegex(VerificationError, "already used"):
                self._wrapper(root, approval).run(
                    "b" * 64,
                    lambda: True,
                    ambiguous,
                    lambda: lambda phase: {"mutation_observed": False},
                )
            self.assertEqual(["createEvent"], calls)

    def test_observer_failure_after_dispatch_freezes_with_audit(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            calls: list[str] = []

            def unavailable_observer():
                def observe(phase: str) -> dict[str, object]:
                    raise RuntimeError("fixture observer unavailable")

                return observe

            bundle = self._wrapper(Path(tmp), mutation_approval()).run(
                "b" * 64,
                lambda: True,
                lambda step: calls.append(step.operation_id)
                or success_exchange(step.operation_id),
                unavailable_observer,
            )

            self.assertEqual("ambiguous-frozen", bundle.terminal_status)
            self.assertEqual(["createEvent"], calls)
            self.assertEqual(
                "observer-unavailable", bundle.observer_assertions[-1]["name"]
            )

    def test_response_observer_conflict_runs_approved_cleanup(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            calls: list[str] = []
            observer_number = 0

            def observer_factory():
                nonlocal observer_number
                observer_number += 1
                if observer_number == 1:
                    return lambda phase: {"mutation_observed": True}
                return lambda phase: {"restored": True}

            def dispatch(step: MutationStep) -> CapturedExchange:
                calls.append(step.operation_id)
                if step.phase == "primary":
                    return CapturedExchange(
                        outcome="rejected",
                        status=409,
                        discriminator="CONFLICT",
                        request={"operation": step.operation_id},
                        response=None,
                    )
                return success_exchange(step.operation_id)

            bundle = self._wrapper(Path(tmp), mutation_approval()).run(
                "b" * 64,
                lambda: True,
                dispatch,
                observer_factory,
            )

            self.assertEqual("restored", bundle.terminal_status)
            self.assertEqual(["createEvent", "cancelEvent"], calls)

    def test_accepted_mutation_uses_fresh_observers_and_approved_cleanup(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            calls: list[str] = []
            observers: list[int] = []

            def observer_factory():
                identity = len(observers) + 1
                observers.append(identity)
                if identity == 1:
                    return lambda phase: {"mutation_observed": True}
                return lambda phase: {"restored": True}

            bundle = self._wrapper(Path(tmp), mutation_approval()).run(
                "b" * 64,
                lambda: True,
                lambda step: calls.append(step.operation_id) or success_exchange(step.operation_id),
                observer_factory,
            )

            self.assertEqual(["createEvent", "cancelEvent"], calls)
            self.assertEqual([1, 2], observers)
            self.assertEqual("restored", bundle.terminal_status)
            self.assertEqual([1, 1], [entry["attempt"] for entry in bundle.operation_log])
            self.assertTrue(
                all(entry["elapsed_monotonic_ns"] >= 0 for entry in bundle.operation_log)
            )
            payload = json.loads(bundle.path.read_text(encoding="utf-8"))
            self.assertEqual("a" * 40, payload["executable_commit"])
            self.assertEqual("2026-08-12.7", payload["contract_revision"])
            self.assertEqual("fixture-test", payload["tool_version"])

    def test_irreversible_effect_requires_and_records_approved_retention(self) -> None:
        retention = RetentionPlan(
            owner_alias="fixture-owner",
            terminal_state="invitation-recorded",
            retain_until=NOW + timedelta(days=7),
            review_url="https://github.com/KalebCole/partiful/issues/43",
        )
        approval = mutation_approval(
            run_id="4" * 32,
            steps=(MutationStep("primary", "addInvitedGuestsAsHost"),),
            assertions=(ObserverAssertion("after", "mutation_observed", True),),
            accounts=(
                ("fixture_owner", "fixture-owner"),
                ("test_recipient", "test-recipient"),
            ),
            retention=retention,
        )
        with tempfile.TemporaryDirectory() as tmp:
            bundle = self._wrapper(Path(tmp), approval).run(
                "b" * 64,
                lambda: True,
                lambda step: success_exchange(step.operation_id),
                lambda: lambda phase: {"mutation_observed": True},
            )
            self.assertEqual("approved-retained", bundle.terminal_status)
            payload = json.loads(bundle.path.read_text(encoding="utf-8"))
            self.assertEqual("fixture-owner", payload["retention"]["owner_alias"])

    def test_stale_retention_during_valid_approval_causes_zero_dispatch(self) -> None:
        retention = RetentionPlan(
            owner_alias="fixture-owner",
            terminal_state="invitation-recorded",
            retain_until=NOW + timedelta(days=1),
            review_url="https://github.com/KalebCole/partiful/issues/43",
        )
        approval = mutation_approval(
            run_id="5" * 32,
            expires_at=NOW + timedelta(days=3),
            steps=(MutationStep("primary", "addInvitedGuestsAsHost"),),
            assertions=(ObserverAssertion("after", "mutation_observed", True),),
            accounts=(
                ("fixture_owner", "fixture-owner"),
                ("test_recipient", "test-recipient"),
            ),
            retention=retention,
        )
        calls: list[str] = []
        with tempfile.TemporaryDirectory() as tmp:
            wrapper = self._wrapper(Path(tmp), approval)
            wrapper.now = lambda: NOW + timedelta(days=2)
            with self.assertRaisesRegex(VerificationError, "retention deadline"):
                wrapper.run(
                    "b" * 64,
                    lambda: True,
                    lambda step: calls.append(step.operation_id)
                    or success_exchange(step.operation_id),
                    lambda: lambda phase: {"mutation_observed": True},
                )
        self.assertEqual([], calls)

    def test_retention_must_cover_the_entire_approval_window(self) -> None:
        retention = RetentionPlan(
            owner_alias="fixture-owner",
            terminal_state="invitation-recorded",
            retain_until=NOW + timedelta(days=1),
            review_url="https://github.com/KalebCole/partiful/issues/43",
        )
        approval = mutation_approval(
            run_id="6" * 32,
            expires_at=NOW + timedelta(days=3),
            steps=(MutationStep("primary", "addInvitedGuestsAsHost"),),
            assertions=(ObserverAssertion("after", "mutation_observed", True),),
            accounts=(
                ("fixture_owner", "fixture-owner"),
                ("test_recipient", "test-recipient"),
            ),
            retention=retention,
        )
        calls: list[str] = []
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaisesRegex(VerificationError, "cover approval window"):
                self._wrapper(Path(tmp), approval).run(
                    "b" * 64,
                    lambda: True,
                    lambda step: calls.append(step.operation_id)
                    or success_exchange(step.operation_id),
                    lambda: lambda phase: {"mutation_observed": True},
                )
        self.assertEqual([], calls)


class ReviewRegressionTests(unittest.TestCase):
    def test_structural_capture_omits_opaque_identifier_keys(self) -> None:
        from scripts.partiful_verify.capture import structural_capture

        with self.assertRaisesRegex(VerificationError, "observer assertion"):
            ObserverAssertion("after", "mutation_observed", "private-expected")
        with self.assertRaisesRegex(VerificationError, "observer assertion"):
            ObserverAssertion("after", "raw-identifier", True)
        self.assertEqual({}, structural_capture({"raw-identifier": "private-value"}))
        self.assertEqual(
            {},
            structural_capture(
                {
                    "usr_7f3c9a": "private-user",
                    "gst_8a1b2c": "private-guest",
                    "evt_4d5e6f": "private-event",
                }
            ),
        )

    def test_observer_audit_omits_non_boolean_observer_values(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            approval = mutation_approval()
            with patch(
                "scripts.partiful_verify.wrappers.EXECUTABLE_GATE_MANIFEST",
                FIXTURE_EXECUTABLE_GATES,
            ):
                bundle = MutationWrapper(
                    approval=approval,
                    gate_states={gate: True for gate in required_gates(approval)},
                    approval_key=FIXTURE_KEY,
                    use_registry=ApprovalUseRegistry(root / "uses"),
                    audit_store=AuditStore(root / "audit"),
                    raw_capture_root=root / "raw",
                    now=lambda: NOW,
                ).run(
                    "b" * 64,
                    lambda: True,
                    lambda step: success_exchange(step.operation_id),
                    lambda: lambda phase: {
                        "mutation_observed": "PRIVATE-ACTUAL",
                        "usr_7f3c9a": "private-user",
                    },
                )

            persisted = bundle.path.read_text(encoding="utf-8")
            self.assertNotIn("PRIVATE-ACTUAL", persisted)
            self.assertNotIn("usr_7f3c9a", persisted)
            self.assertNotIn("private-user", persisted)
            payload = json.loads(persisted)
            for observed in payload["observer_assertions"]:
                self.assertIn(observed["actual"], (True, False, None))
                self.assertIn(observed["expected"], (True, False, None))

    def test_capture_disposes_raw_file_when_serialization_fails(self) -> None:
        from scripts.partiful_verify.capture import capture_and_dispose

        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "raw"
            exchange = success_exchange("getEventInfo")
            with patch(
                "scripts.partiful_verify.capture.json.dumps",
                side_effect=ValueError("fixture serialization failure"),
            ):
                with self.assertRaises(VerificationError):
                    capture_and_dispose(root, "0" * 32, 1, exchange)
            self.assertEqual([], list(root.iterdir()))

    def test_retention_plan_rejects_a_past_deadline(self) -> None:
        with self.assertRaisesRegex(VerificationError, "retention deadline is stale"):
            RetentionPlan(
                owner_alias="fixture-owner",
                terminal_state="invitation-recorded",
                retain_until=datetime.now(timezone.utc) - timedelta(seconds=1),
                review_url="https://github.com/KalebCole/partiful/issues/43",
            )

    def test_forged_caller_gate_mapping_cannot_authorize_dispatch(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            calls: list[str] = []
            wrapper = ReadWrapper(
                read_manifest(),
                {"OP18-READ-WRAPPER": True, "OP18-RAW-CAPTURE-DISPOSAL": True},
                AuditStore(Path(tmp) / "audit"),
                Path(tmp) / "raw",
            )
            with self.assertRaisesRegex(VerificationError, "OP18-READ-WRAPPER"):
                wrapper.run(
                    "getEventInfo",
                    "fixture-event",
                    lambda: calls.append("called") or success_exchange("getEventInfo"),
                )
            self.assertEqual([], calls)


if __name__ == "__main__":
    unittest.main()
