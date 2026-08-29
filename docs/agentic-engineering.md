# Agentic engineering contract

GitHub issue requirements, dependencies, allowed files, verification commands, and blockers are canonical. Use domain vocabulary from `docs/command-model.md` and `docs/architecture.md`; implement only through public seams. Architectural/package boundaries are constraints, not escalation triggers.

## Modes

- **Feature:** start with a public-seam behavioral test that fails (RED), implement the smallest behavior (GREEN), then refactor only while green.
- **Bug:** reproduce the reported behavior at its public seam before production code and retain the regression test.
- **Refactor:** establish unchanged behavior first and do not mix new behavior.
- **Evidence:** conduct bounded, credential-free public/repository investigation, redact observations, and allow a reviewed `unsupported` conclusion. No live mutation.

## Delivery team and handoff

The verified delivery team has two roles:

- `project-manager` coordinates the work and verifies the result.
- `coding-worker` builds, tests, self-reviews, writes documentation, and performs repository branch and pull request work.

No separate reviewer, implementer, evidence, or integrator profile is required. The `coding-worker` capability contract is exactly: file, terminal, skills, todo, clarify, session_search, delegation, web, and browser.

For repository branch and pull request work, `coding-worker` may declare `GH_TOKEN` and `GITHUB_TOKEN`. Other `coding-worker` environment names are rejected, including `NOTION_API_KEY`, without printing their values.

For this profile, `terminal.auto_source_bashrc` is false, `terminal.env_passthrough` is empty, and `terminal.shell_init_files` contains only that profile directory's `shell/profile-env.sh`.

Escalate only contradictory requirements, safety choices, or genuinely unresolved behavior. Never escalate an architectural boundary. Never seek, use, recover, or create credentials or mutate a live Partiful account.
