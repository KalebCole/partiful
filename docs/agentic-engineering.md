# Agentic engineering contract

GitHub issue requirements, dependencies, allowed files, verification commands, and blockers are canonical. Use domain vocabulary from `docs/command-model.md` and `docs/architecture.md`; implement only through public seams. Architectural/package boundaries are constraints, not escalation triggers.

## Modes

- **Feature:** start with a public-seam behavioral test that fails (RED), implement the smallest behavior (GREEN), then refactor only while green.
- **Bug:** reproduce the reported behavior at its public seam before production code and retain the regression test.
- **Refactor:** establish unchanged behavior first and do not mix new behavior.
- **Evidence:** conduct bounded, credential-free public/repository investigation, redact observations, and allow a reviewed `unsupported` conclusion. No live mutation.

## Handoff and native review

Record RED and GREEN commands/outcomes, focused and full verification, paths, exact PR head SHA, and handoff URL; read the PR and handoff back. Native-request `partiful-code-reviewer` on the same card. The reviewer exact-SHA checks out the PR and writes `## Implementation review` with `Verdict: APPROVE|REQUEST_CHANGES`, `Commit: <40-sha>`, `RED:`, `GREEN:`, and all category lines `Category-<name>: PASS|FAIL` for: specification, correctness, domain_model, test_quality, edge_cases, security_privacy, maintainability, domain_adherence, evidence_rigor. Request changes returns the same card with evidence; three reviews hard-block. Approval calls the deterministic gate.

Escalate only contradictory requirements, safety choices, or genuinely unresolved behavior. Never escalate an architectural boundary. Never seek, use, recover, or create credentials or mutate a live Partiful account.
