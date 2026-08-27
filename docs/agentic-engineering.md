# Agentic engineering contract

Every implementation card reads this document plus the GitHub issue, which is canonical for allowed files, dependencies, and verification.

## Modes

- **Feature:** begin with a public-seam test that fails (RED), implement the smallest behavior (GREEN), then refactor only while green.
- **Bug:** reproduce the reported behavior through its public seam before changing production code; retain the regression test.
- **Refactor:** prove unchanged behavior first; do not mix new behavior into a refactor.
- **Evidence:** collect a bounded, redacted observation without changing live state. Behavioral blockers require behavioral evidence; structural blockers may be resolved by static repository evidence without a runtime probe.

## Implementation and review

Work only in the exact GitHub `Allowed files:` write set. Record RED and GREEN commands and outcomes, focused and full verification, changed paths, exact PR head SHA, and handoff URL. Never seek, use, recover, or create credentials and never mutate a live Partiful account.

Review the exact head SHA in these sections: specification, domain model, test quality, correctness, edge cases, security/privacy, and maintainability. Post at most three substantive findings. The structured verdict is `APPROVE` or `REQUEST_CHANGES`; approval names the exact SHA. `REQUEST_CHANGES` returns the same card to the implementer. An approved SHA passes the deterministic merge gate only after all mechanical predicates succeed.
