# Architecture

## Status

This document records accepted product architecture decisions for the Partiful CLI and MCP server. The normative public inventory and interface contract live in [`command-model.md`](command-model.md). Transport details remain governed by [`../spec/partiful.openapi.json`](../spec/partiful.openapi.json) and its evidence ledger.

## Architectural precedents

Partiful follows two deliberate precedents:

1. [gogcli](https://gogcli.sh/) for the public command and automation contract.
2. [Printing Press](https://github.com/mvanhorn/cli-printing-press) for evidence-led API modeling, typed operations, strict protocol handling, native Go delivery, and mechanical verification.

These are precedents, not dependencies. Partiful adopts the relevant design rules without copying gogcli's Google-specific account model or exposing transport operations as public commands.

## Shared application architecture

The CLI and MCP server are two adapters over one application layer. They share:

- domain types and operation inputs;
- validation and normalization;
- transport adapters;
- credential storage and token refresh;
- application-visible field projection;
- error classification;
- mutation and dry-run behavior.

Every non-interactive public CLI operation maps one-to-one to a typed MCP tool. Interactive authentication is the exception: `auth login` is CLI-only, while MCP returns an actionable authentication-required error.

## Application parity and authorization

The public product mirrors the data and access that the current Partiful application exposes to the authenticated principal for the corresponding workflow. It does not add a separate host-only, cohost-only, or privacy-filtering policy. In particular, guest operations return every guest field exposed by the application to that principal.

Partiful's backend authorization result is authoritative. A denied operation returns a classified permission error and no partial result.

Application parity is not raw transport passthrough. Firebase metadata, transport identifiers, wrapper fields, and other response members that the Partiful application does not expose remain private adapter details.

## Public contact references

Contact discovery returns an opaque `contact_ref` beside each application's visible contact fields. Every contact-consuming CLI command and its paired MCP tool accept the same typed reference. A contact name may be used as a convenience only when it resolves to exactly one contact; an ambiguous name fails and returns the matching candidates with their references.

A `contact_ref` is derived with a keyed, non-reversible function over the active account scope and private transport identity. The stable reference secret lives in the shared credential provider. References are bound to one account and installation; the public contract does not promise portability between installations.

Reference resolution fetches the current accessible contact catalog and compares derived references. It does not require a local contact mapping database. A modified reference, a reference from another account, or a reference whose contact is no longer accessible fails as `invalid_contact_ref`. Raw Partiful, Firebase, and Firestore contact identifiers never enter the public result or input contract.

## Output contract

Partiful follows gogcli's stdout-as-an-API model:

- Human-readable output is the default terminal mode.
- `--json` emits stable, command-specific JSON to stdout.
- `--plain` may emit stable TSV where tabular output is useful.
- Primary results go to stdout.
- Prompts, progress, warnings, and diagnostics go to stderr.
- Failures use stable exit codes and stderr diagnostics.
- MCP returns the same structured application result represented by CLI JSON mode.

JSON uses domain-specific envelopes when they carry useful metadata:

```json
{
  "events": [],
  "next_cursor": null
}
```

```json
{
  "event": {
    "id": "event_ref"
  }
}
```

Partiful does not add a universal `{ "ok": true, "data": ..., "meta": ... }` wrapper. Process status and classified errors must not duplicate every successful domain result.

## Input contract

Commands use typed positional arguments and named flags as their primary input contract:

```bash
partiful events create \
  --title "Dinner" \
  --start "2026-08-30T18:00:00-07:00"
```

The command schema defines required arguments, flag types, repeatability, allowed values, and conflicts. Structured JSON flags are reserved for fields that are naturally structured. Explicit file or stdin input is reserved for large content and genuine whole-resource round-trip workflows.

Partiful does not provide a universal `--input <command.json>` mechanism. When a command supports whole-resource JSON, that source is mutually exclusive with field flags; mixed sources fail instead of using precedence or implicit merging. Unknown JSON fields fail closed.

`--no-input` means that the command cannot prompt. It does not prohibit an explicit payload from stdin.

MCP accepts the same logical typed input through a fixed tool schema. MCP tool arguments do not allow arbitrary local file or stdin expansion.

## Mutation contract

Mutations execute in one invocation. `--dry-run` performs local parsing, normalization, validation, and intended-operation rendering without sending a request.

There is no plan token, `--apply` phase, repeated approval payload, or separate mutation-planning subsystem. MCP hosts apply standard tool safety annotations and capability controls to the same operations.

## Authentication contract

The normative authentication and credential lifecycle is defined in
[`plans/2026-08-26-authentication-credential-lifecycle-design.md`](plans/2026-08-26-authentication-credential-lifecycle-design.md).
The first release supports one active Partiful account. CLI and MCP use one credential provider:

- OS credential storage by default;
- a secure file fallback for environments without an OS credential store;
- automatic shared token refresh.

Neither interface owns a separate token lifecycle.
