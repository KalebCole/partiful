# Autonomous Wayfinder decision and implementation lanes

This repository uses GitHub as the canonical product and implementation record and the Partiful Hermes Kanban board as the durable execution queue. Fresh agent contexts resolve, independently review, and reconcile each decision ticket, then implement, independently code-review, and integrate each executable slice. Sandcastle is not part of this workflow.

## Durable boundaries

| System | Owns | Must not own |
|---|---|---|
| GitHub map issue #8 | Destination, decision graph, implementation graph, native blockers, settled decision gists | Worker attempts or runtime state |
| GitHub decision ticket | Question, candidate resolutions, review verdicts, final resolution | Agent process lifecycle |
| GitHub implementation issue and PR | Executable scope, acceptance, code diff, verification, review, merged result | Worker process lifecycle |
| Partiful Hermes Kanban board | Claims, role-isolated decision and implementation runs, dependencies, retries, crash recovery | Canonical decisions or source history |
| Frontier pumps | Deterministic decision and implementation frontier discovery plus idempotent card creation | Technical reasoning or code authorship |
| Resolver | One evidence-backed candidate | Approval, closing, or map edits |
| Reviewer | Independent critique and one structured verdict | Silent rewrites or map edits |
| Cartographer | Verdict reconciliation and verified GitHub mutations | Unreviewed technical invention |

Do not write project state to global agent memory. Link to repository paths and GitHub comments instead of copying full source documents into card bodies.

## Ticket types

- `wayfinder:decision`: an autonomous technical decision inferable from authoritative sources or conservative engineering defaults.
- `wayfinder:grilling`: a real owner preference, risk acceptance, or irreversible scope decision. It is human-in-the-loop.
- `wayfinder:task`: deterministic evidence collection or prerequisite work.
- `wayfinder:prototype`: a bounded experiment that can disprove an assumption.

An agent must not simulate both sides of a `wayfinder:grilling` ticket. If a decision ticket discovers a genuine owner gate, the cartographer changes its type and asks one concrete scenario-based question.

## Source precedence

Use the narrowest source set needed by the ticket.

1. Settled resolution comments linked from map issue #8.
2. `docs/command-model.md`, `docs/architecture.md`, and the accepted authentication design.
3. `spec/partiful.openapi.json`, `evidence/ledger.json`, and current research artifacts for private transport facts.
4. The previous working Partiful CLI as implementation evidence and prior art only.
5. Rejected generated output as negative evidence only.

A lower-precedence artifact cannot silently reopen a settled public, safety, or authentication contract.

## Autonomous default

When evidence permits more than one internal engineering choice, select the narrowest reversible design that:

1. preserves settled contracts;
2. fails closed;
3. minimizes privacy exposure;
4. keeps dependency direction explicit; and
5. does not claim unobserved transport behavior.

Missing transport evidence becomes a stable evidence gate. It is not permission to invent a response shape, permission rule, retry behavior, business-state result, or cleanup procedure.

## Card graph

For each executable decision frontier, the pump creates this dependency chain:

```text
resolver (wayfinder-resolver)
    -> reviewer (wayfinder-reviewer)
        -> reconciler (wayfinder-cartographer)
```

All cards use a shared read-only directory workspace for repository evidence. The agents may write GitHub comments and issue metadata only as their role permits. Every card runs in goal mode with a six-turn budget. A retry starts a fresh process.

The first chain uses these idempotency keys:

```text
partiful:github:<issue>:resolve
partiful:github:<issue>:review
partiful:github:<issue>:reconcile
```

A revision chain adds the independent-review comment database ID:

```text
partiful:github:<issue>:<review-comment-id>:resolve
partiful:github:<issue>:<review-comment-id>:review
partiful:github:<issue>:<review-comment-id>:reconcile
```

## Candidate resolution contract

The resolver posts one comment in this exact shape:

```markdown
## Candidate resolution

Automation-Key: partiful:github:<issue>:candidate:<attempt>

### Decision
<one precise decision>

### Evidence
- <authoritative source and exact section or stable URL>

### Composition
<interfaces, ownership, dependency direction, or behavior>

### Rejected alternatives
- <alternative>: <reason>

### Consequences
- <fact later tickets and implementations may rely on>

### Evidence gates
- <stable gate identity, unknown behavior, and resume condition>

### Map impact
- New tickets:
- Changed blockers:
- Fog graduated:
- Tickets invalidated:
```

Use `none` explicitly in an empty map-impact or evidence-gate field. The resolver reads its comment back, records the comment URL, and completes its card. It does not close the ticket.

## Independent review contract

The reviewer receives the ticket, latest candidate, repository sources, and this contract. It does not receive the resolver transcript. It posts one comment:

```markdown
## Independent review

Candidate: <candidate comment URL>
Verdict: APPROVE | REVISE | EVIDENCE_REQUIRED | OWNER_GATE

### Findings
1. Issue: <substantive defect>
   Impact: <why success depends on it>
   Correction: <bounded correction>
   Verification: <evidence that would prove the correction>
```

Maximum three findings. Ignore style, naming, grammar, and generic best-practice commentary.

- `APPROVE`: internally coherent, evidence-backed, safe for downstream reliance.
- `REVISE`: the technical answer can be corrected with existing evidence.
- `EVIDENCE_REQUIRED`: a specific external or empirical fact is missing.
- `OWNER_GATE`: the choice is a product preference, risk acceptance, spending commitment, privacy exposure, or irreversible scope decision.

The reviewer never edits the candidate and never closes the issue.

## Reconciliation contract

Immediately before writing, the cartographer re-reads issue state, assignees, recent comments, map issue #8, and native blockers.

### APPROVE

1. Confirm the verdict reviews the latest candidate.
2. Post a compact `## Resolution` linking the candidate and review.
3. Close the ticket as completed.
4. Add or update one decision gist under `## Decisions so far` on map #8.
5. Apply only map-impact mutations covered by the review.
6. Read every changed target back and verify it.

### REVISE

Create a fresh resolver, reviewer, and reconciler chain using the review comment database ID as the attempt key. Do not edit the old candidate. After two revision verdicts, the cartographer adjudicates the candidate and both reviews in a fresh context. It either identifies a missing evidence task or selects the correction best supported by source precedence. It must not convert ordinary reviewer disagreement into an owner question.

### EVIDENCE_REQUIRED

Create the smallest `wayfinder:task`, attach it to map #8, add it as a native blocker of the decision, and unassign the decision. The task must state the stable evidence-gate identity and what observation closes it.

### OWNER_GATE

Change the issue from `wayfinder:decision` to `wayfinder:grilling`, unassign it, and ask one concrete scenario-based question. Do not bundle multiple choices into one owner gate.

## Implementation frontier

The final partition ticket produces GitHub implementation issues for the same Partiful Hermes Kanban board. Each implementation issue must define:

- exact repository scope and allowed files;
- authoritative decision links;
- prerequisites and native blockers;
- owned package boundaries;
- public behavior and permitted transport operations;
- stable evidence-gate identities;
- tests and mechanical verification commands;
- credential and live-mutation limits;
- cleanup requirements;
- forbidden changes; and
- independent-review acceptance.

The implementation frontier pump turns every open, unblocked, unassigned `partiful:implementation` child issue into this dependency chain:

```text
partiful-implementer (isolated worktree, strict TDD, opens PR)
  -> partiful-code-reviewer (fresh context, exact-commit verdict)
    -> partiful-integrator (merge or bounded revision chain)
```

The implementer never merges. The reviewer never edits. The integrator merges only an approved latest commit with successful verification and reads GitHub state back after every write. `REQUEST_CHANGES` creates a fresh implementation chain; the third failed review blocks rather than looping forever. Sandcastle is not used.

## Operator commands

Preview the current unclaimed frontier:

```bash
python3 scripts/wayfinder_frontier_pump.py --dry-run
python3 scripts/implementation_frontier_pump.py --dry-run
```

Queue the current frontier idempotently:

```bash
python3 scripts/wayfinder_frontier_pump.py
python3 scripts/implementation_frontier_pump.py
```

Queue a fresh revision chain for one ticket:

```bash
python3 scripts/wayfinder_frontier_pump.py --issue <number> --attempt <review-comment-id>
python3 scripts/implementation_frontier_pump.py --issue <number> --attempt <review-comment-id>
```

The scheduled pump uses `--quiet`: success with no new frontier prints nothing; failures exit nonzero so Hermes cron alerts the owner.
