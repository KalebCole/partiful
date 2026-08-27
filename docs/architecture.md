# Architecture

## Status

This document records accepted product architecture decisions for the Partiful CLI and MCP server. Transport details remain governed by `spec/partiful.openapi.json` and its evidence ledger.

## Architectural precedents

Partiful follows two deliberate precedents:

1. [gogcli](https://gogcli.sh/) for the public command and automation contract.
2. Printing Press for evidence-led API modeling, typed operations, strict protocol handling, native Go delivery, and mechanical verification.

These are precedents, not dependencies. Partiful adopts the relevant design rules without copying gogcli's Google-specific account model or exposing transport operations as public commands.

## Shared application architecture

The CLI and MCP server are two adapters over one application layer. They share:

- domain types and operation inputs;
- validation and normalization;
- transport adapters;
- credential storage and token refresh;
- privacy filtering;
- error classification;
- mutation and dry-run behavior.

Every non-interactive public CLI operation maps one-to-one to a typed MCP tool. Interactive authentication is the exception: `auth login` is CLI-only, while MCP returns an actionable authentication-required error.

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
  --starts-at "2026-08-30T18:00:00-07:00"
```

The command schema defines required arguments, flag types, repeatability, allowed values, and conflicts. Structured JSON flags are reserved for fields that are naturally structured. Explicit file or stdin input is reserved for large content and genuine whole-resource round-trip workflows.

Partiful does not provide a universal `--input <command.json>` mechanism. When a command supports whole-resource JSON, that source is mutually exclusive with field flags; mixed sources fail instead of using precedence or implicit merging. Unknown JSON fields fail closed.

`--no-input` means that the command cannot prompt. It does not prohibit an explicit payload from stdin.

MCP accepts the same logical typed input through a fixed tool schema. MCP tool arguments do not allow arbitrary local file or stdin expansion.

## Mutation contract

Mutations execute in one invocation. `--dry-run` performs local parsing, normalization, validation, and intended-operation rendering without sending a request.

There is no plan token, `--apply` phase, repeated approval payload, or separate mutation-planning subsystem. MCP hosts apply standard tool safety annotations and capability controls to the same operations.

## Authentication contract

The first release supports one active Partiful account. CLI and MCP use one credential provider:

- OS credential storage by default;
- a secure file fallback for environments without an OS credential store;
- automatic shared token refresh.

Neither interface owns a separate token lifecycle.
