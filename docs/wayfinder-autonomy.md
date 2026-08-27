# Wayfinder autonomy

GitHub is canonical for requirements, dependencies, issue blockers, verification, PR reviews, merges, and evidence. Partiful Kanban owns only execution lifecycle: one durable idempotent card per GitHub issue, role assignment, retries, and `request-review` transitions. There is no custom workflow database/controller, integrator role, Sandcastle, live credential, or live mutation path.

## Single-card lifecycle

An implementation card has key `partiful:implementation:<issue>`, branch `partiful/issue-<issue>`, one isolated worktree, and assignee `partiful-implementer`. It reads `docs/agentic-engineering.md`, follows strict TDD, stays in the issue write set, opens/updates a PR and handoff, reads both back, then requests native review from `partiful-code-reviewer`. The reviewer checks the exact PR head and posts a structured verdict. `REQUEST_CHANGES` returns the same card; after at most three review cycles it remains blocked. `APPROVE` invokes the deterministic exact-SHA merge gate.

The merge gate rejects head mismatch, out-of-write-set paths, open GitHub blockers, pending/failed required checks, missing/non-approved latest review, and more than three cycles. It runs focused/full verification detached at the reviewed head, squash merges only then, and reads PR `MERGED` and issue `CLOSED` afterward. Rollback is GitHub revert plus a new issue/card; never rewrite merged history.

## Evidence lane

Open unassigned unblocked `wayfinder:task` issues use one `wayfinder-resolver` evidence card only when its audited terminal/file/skills capability and repository evidence can support the task. Tasks needing authenticated/live observation hold as `capability_required`; #27 waits for #22 and #29 waits for #41 using GitHub blockers. Evidence probes are bounded and redacted, with no credentials or live mutations.

Implementation worker profiles expose exactly terminal, file, and skills; `.env` is empty; shell startup, environment passthrough, and init files are disabled. Same-profile concurrency is allowed only where wave write sets are disjoint. `scripts/select_implementation_wave.py` greedily selects by issue number and holds overlaps conservatively.
