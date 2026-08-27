# CLI and MCP command model

## Status

This document is the normative public command inventory for the Partiful CLI and MCP server. [`../spec/partiful.openapi.json`](../spec/partiful.openapi.json) remains the authority for private transport facts. [`architecture.md`](architecture.md) remains the authority for shared architecture and interface rules.

The public command tree adopts the previously shipped Go CLI tree. It does not adopt the old universal JSON envelope, plan-token workflow, name-only contact identity, or duplicated interface behavior.

## Inventory invariant

The release contains exactly:

- 24 public CLI command paths;
- 23 non-interactive CLI command paths;
- 23 MCP tools; and
- one declared CLI-only exception: `auth.login`.

Every `paired` row below has exactly one MCP tool. `auth.login` is the only `cli-only` row. `partiful --version` is an alias for `partiful version`, not a second command path.

MCP hint notation is `[readOnlyHint, destructiveHint, idempotentHint, openWorldHint]`, using `T` or `F`. Hints describe normal execution. A mutation with `dry_run: true` still uses its mutation tool and sends no request.

| ID | Disposition | CLI command | Shared operation | MCP tool | Authorization | Risk | MCP hints |
| --- | --- | --- | --- | --- | --- | --- | --- |
| CMD-001 | cli-only | `partiful auth login` | `AuthLoginInteractive` | none | private interactive terminal | interactive | n/a |
| CMD-002 | paired | `partiful auth status` | `GetAuthStatus` | `auth_status` | local credentials; refresh when required | credential-refresh | `[F,F,T,T]` |
| CMD-003 | paired | `partiful auth logout` | `Logout` | `auth_logout` | local credential store | credential-delete | `[F,T,T,F]` |
| CMD-004 | paired | `partiful events list` | `ListEvents` | `events_list` | authenticated account | read | `[T,F,T,T]` |
| CMD-005 | paired | `partiful events get <event-id>` | `GetEvent` | `events_get` | authenticated account; backend decides access | read | `[T,F,T,T]` |
| CMD-006 | paired | `partiful events create` | `CreateEvent` | `events_create` | authenticated account; backend decides access | write | `[F,F,F,T]` |
| CMD-007 | paired | `partiful events update <event-id>` | `UpdateEvent` | `events_update` | authenticated account; backend decides access | destructive-write | `[F,T,F,T]` |
| CMD-008 | paired | `partiful events cancel <event-id>` | `CancelEvent` | `events_cancel` | authenticated account; backend decides access | destructive-write | `[F,T,F,T]` |
| CMD-009 | paired | `partiful guests list <event-id>` | `ListGuests` | `guests_list` | authenticated account; backend decides access | read | `[T,F,T,T]` |
| CMD-010 | paired | `partiful guests invite <event-id>` | `InviteGuest` | `guests_invite` | authenticated account; backend decides access | write | `[F,F,F,T]` |
| CMD-011 | paired | `partiful rsvp get <event-id>` | `GetRsvp` | `rsvp_get` | authenticated account; backend decides access | read | `[T,F,T,T]` |
| CMD-012 | paired | `partiful rsvp set <event-id>` | `SetRsvp` | `rsvp_set` | authenticated account; backend decides access | destructive-write | `[F,T,F,T]` |
| CMD-013 | paired | `partiful contacts list` | `ListContacts` | `contacts_list` | authenticated account | read | `[T,F,T,T]` |
| CMD-014 | paired | `partiful cohosts invite <event-id>` | `InviteCohost` | `cohosts_invite` | authenticated account; backend decides access | write | `[F,F,F,T]` |
| CMD-015 | paired | `partiful cohosts revoke-invite <event-id>` | `RevokeCohostInvite` | `cohosts_revoke_invite` | authenticated account; backend decides access | destructive-write | `[F,T,F,T]` |
| CMD-016 | paired | `partiful cohosts remove <event-id>` | `RemoveCohost` | `cohosts_remove` | authenticated account; backend decides access | destructive-write | `[F,T,F,T]` |
| CMD-017 | paired | `partiful cohosts link create <event-id>` | `CreateCohostLink` | `cohosts_link_create` | authenticated account; backend decides access | write | `[F,F,F,T]` |
| CMD-018 | paired | `partiful cohosts link revoke <event-id>` | `RevokeCohostLink` | `cohosts_link_revoke` | authenticated account; backend decides access | destructive-write | `[F,T,F,T]` |
| CMD-019 | paired | `partiful blasts send <event-id>` | `SendBlast` | `blasts_send` | authenticated account; backend decides access | destructive-write | `[F,T,F,T]` |
| CMD-020 | paired | `partiful posters list` | `ListPosters` | `posters_list` | none | read | `[T,F,T,T]` |
| CMD-021 | paired | `partiful posters search` | `SearchPosters` | `posters_search` | none | read | `[T,F,T,T]` |
| CMD-022 | paired | `partiful schema [command.path]` | `GetCommandSchema` | `schema` | none | diagnostic | `[T,F,T,F]` |
| CMD-023 | paired | `partiful doctor` | `RunDoctor` | `doctor` | none; credentials are inspected but not printed | diagnostic | `[T,F,T,F]` |
| CMD-024 | paired | `partiful version` | `GetVersion` | `version` | none | diagnostic | `[T,F,T,F]` |

## Shared invocation contract

Global CLI flags are:

| Flag | Contract |
| --- | --- |
| `--json` | Write one command-specific JSON result to stdout. |
| `--plain` | Write stable TSV for commands that declare a plain projection. |
| `--no-input` | Prohibit prompts. It does not prohibit explicit stdin payloads. |
| `--dry-run` | Mutation commands only. Validate and normalize locally, render the intended operation, and send no request. |
| `--version` | Alias `partiful version`. |

`--json` and `--plain` conflict. Human-readable output is the default on a terminal. Results go to stdout. Prompts, warnings, progress, and diagnostics go to stderr. A failure leaves stdout empty. With `--json`, a classified error object goes to stderr.

The CLI uses typed positionals and flags. It has no universal `--input` flag. Unknown flags, repeated scalar flags, unknown JSON members, and conflicting input sources fail as `usage.invalid` or `input.invalid`.

MCP takes the same logical typed operation input. File and stdin flags are CLI adapter behavior: the CLI reads and validates the content before calling the shared operation, while MCP supplies that content directly as a string or object.

## Collection contract

Collection commands accept:

| CLI flag | MCP field | Type | Rule |
| --- | --- | --- | --- |
| `--limit <n>` | `limit` | integer | Default 25; range 1 through 100. |
| `--cursor <opaque>` | `cursor` | string or null | Must come from the same operation and filters. |
| `--all` | `all` | boolean | Fetch more than one page; requires `max_items`. |
| `--max-items <n>` | `max_items` | integer or null | Required with `all`; range 1 through 1000. |

A collection JSON result uses a command-specific item key plus `next_cursor` and `has_more`. It never reports a truncated result as complete.

## Shared RSVP read vocabulary

`EventReadRsvp` is a lossless normalization of the reviewed current `GuestStatus` enum:

| Public value | Private reviewed value |
| --- | --- |
| `ready-to-send` | `READY_TO_SEND` |
| `sending` | `SENDING` |
| `send-error` | `SEND_ERROR` |
| `delivery-error` | `DELIVERY_ERROR` |
| `sent` | `SENT` |
| `interested` | `INTERESTED` |
| `waitlist` | `WAITLIST` |
| `maybe` | `MAYBE` |
| `declined` | `DECLINED` |
| `going` | `GOING` |
| `pending-approval` | `PENDING_APPROVAL` |
| `approved` | `APPROVED` |
| `withdrawn` | `WITHDRAWN` |
| `waitlisted-for-approval` | `WAITLISTED_FOR_APPROVAL` |
| `rejected` | `REJECTED` |
| `responded-to-find-a-time` | `RESPONDED_TO_FIND_A_TIME` |

`events.list.my_rsvp`, `events.get.my_rsvp`, `guests.list.rsvp_status`, and `rsvp.get.status` use `EventReadRsvp` or null. An unknown present value is `contract.protocol_changed`, not null.

## Contact selector

`InviteGuest`, `InviteCohost`, `RevokeCohostInvite`, and `RemoveCohost` take exactly one selector:

| CLI flag | MCP field | Rule |
| --- | --- | --- |
| `--contact-ref <opaque>` | `contact_ref` | Primary stable input for this account and installation. |
| `--contact <display-name>` | `contact` | Convenience input; it must resolve to exactly one accessible contact. |

No match is `resource.not_found`. An ambiguous name is `match.ambiguous`; safe error details contain candidate application-visible names and `contact_ref` values. Modified, wrong-account, or inaccessible references are `input.invalid` with code `INVALID_CONTACT_REF`. Raw Partiful, Firebase, and Firestore identifiers never enter public input or output.

## Mutation and dry-run contract

A mutation executes directly unless `dry_run` is true. The application layer performs one transport attempt and does not automatically retry an outcome that may have reached Partiful.

Dry-run performs only local work. It does not authenticate, resolve a contact, read an event, test remote authorization, or claim that Partiful will accept the request. Its JSON result is:

```json
{
  "dry_run": true,
  "operation": "events.update",
  "input": {}
}
```

Private message content is represented in dry-run output by UTF-8 byte length and SHA-256 digest, not the content. There is no plan token, `--apply`, `--plan`, `--confirm`, or second invocation.

## Mutation risk matrix

Every row executes directly by default. `--dry-run` or `dry_run: true` is the non-executing path. The client makes one transport attempt and does not automatically retry an ambiguous result.

`destructive-write` includes deletion, overwrite, access removal, and broad irreversible human contact. Individual guest and cohost invitations remain additive `write`; `blasts.send` is destructive because it contacts the full guest audience in one operation.

| Operation | Observable consequence | Primary risk | Recovery or verification contract |
| --- | --- | --- | --- |
| `auth.logout` | Removes local credentials. | Local loss of session. | Idempotent; login restores access. |
| `events.create` | Creates an event. | Duplicate event after an ambiguous result. | No automatic retry; caller checks `events.list`. |
| `events.update` | Replaces selected event fields. | Overwrites current event state. | No automatic retry; caller checks `events.get`. |
| `events.cancel` | Cancels an event and may notify guests. | Destructive state change and human contact. | No automatic retry; caller checks `events.get`. |
| `guests.invite` | Adds an invitee and may send an invitation. | Human contact and duplicate invitation. | No automatic retry; caller checks `guests.list`. |
| `rsvp.set` | Replaces the current account's RSVP or interest state. | Overwrites current attendance state. | No automatic retry; caller checks `rsvp.get`. |
| `cohosts.invite` | Sends a cohost invitation. | Human contact and potential future access. | No automatic retry; caller checks current cohost state when the transport contract can expose it. |
| `cohosts.revoke-invite` | Revokes a pending cohost invitation. | Removes pending access. | No automatic retry; caller checks current cohost state when available. |
| `cohosts.remove` | Removes a current cohost. | Destructive access removal. | No automatic retry; caller checks current cohost state when available. |
| `cohosts.link.create` | Creates an access-bearing URL. | Anyone with the URL may gain cohost access. | Return only the reviewed URL; caller can revoke it. |
| `cohosts.link.revoke` | Invalidates the cohost URL. | Destructive access removal. | No automatic retry; caller checks current link state when available. |
| `blasts.send` | Sends a message to all guests. | Broad irreversible human contact. | No automatic retry; `submitted` never claims delivery. |

## Command contracts

### Authentication and utilities

#### `auth.login`

CLI: `partiful auth login`

This is the only interactive command. Credentials, phone numbers, verification codes, tokens, and private account identifiers are accepted only through the private prompt flow and are never printed. Success:

```json
{"authenticated":true,"token_state":"healthy","expires_at":"2026-08-11T00:00:00Z"}
```

There is no `auth_login` MCP tool. An MCP operation that needs authentication returns `auth.required` with remediation naming `partiful auth login`.

#### `auth.status`

CLI and MCP input: none. The shared operation may refresh expiring credentials and atomically replace them. Success has the same shape as login; `token_state` is `healthy`, `expiring`, `expired`, or `missing`.

#### `auth.logout`

CLI and MCP input: none. Deletes the active account credentials. Success:

```json
{"authenticated":false,"token_state":"missing","expires_at":null}
```

#### `version`

CLI and MCP input: none. Success:

```json
{"cli_version":"0.1.0","command_contract_revision":"1","transport_contract_revision":"2026-08-12.7"}
```

#### `schema`

Input is optional CLI positional `[command.path]` or MCP field `command`. Without it, return all 24 command paths, all 23 MCP tool names, and `auth.login` as the sole exception. With it, return the exact positionals, flags or MCP fields, input schema, success schema, failure types, authorization class, dry-run support, and MCP hints generated from the executable command registry.

#### `doctor`

CLI and MCP input: none. Returns redacted checks:

```json
{"healthy":true,"checks":[{"name":"credentials","status":"pass","message":"Authentication credentials are available.","remediation":null}]}
```

Check status is `pass`, `warn`, or `fail`.

### Events

#### `events.list`

CLI input:

```text
--when <upcoming|past> [collection flags]
```

MCP input is `when` plus collection fields. Success:

```json
{"events":[],"next_cursor":null,"has_more":false}
```

Each event summary has `event_id`, `title`, `start`, `end`, `timezone`, `state`, `user_role`, and `my_rsvp`. Optional unavailable values are null. Closed enums fail on unknown present values.

#### `events.get`

Input is required `event_id`. Success:

```json
{"event":{"event_id":"evt_example","title":null,"start":null,"end":null,"timezone":null,"state":null,"user_role":null,"my_rsvp":null,"description":null,"location":null,"address":null,"visibility":null,"guest_limit":null,"poster":null,"links":null}}
```

#### `events.create`

Required CLI flags and MCP fields: `title`, `start`, and `timezone`. Optional fields: `end`, `description`, `location`, `visibility`, `guest_limit`, `links`, and `poster_id`.

CLI flags are `--title`, `--start`, `--end`, `--timezone`, `--description`, `--location`, `--visibility <private|public>`, `--guest-limit`, repeated `--link <label=url>`, and `--poster-id`. `end` must not precede `start`. Dates use RFC 3339; timezone uses IANA names. Success:

```json
{"submitted":true}
```

The missing `event_id` is intentional while the create response remains an implementation evidence gate. The command cannot ship until that response is reviewed. If the reviewed response supplies a trustworthy application-visible event identifier, a command-contract revision adds it rather than inferring identity from `events.list`.

#### `events.update`

Input is `event_id` plus at least one change. Settable fields are `title`, `description`, `start`, `end`, `timezone`, `guest_limit`, `links`, and `poster_id`. CLI uses the create flag names. Explicit clear flags are `--clear-description`, `--clear-end`, `--clear-guest-limit`, `--clear-links`, and `--clear-poster`; each conflicts with its setter. MCP uses nullable values for those five clearable fields and omission for unchanged fields. Success:

```json
{"event_id":"evt_example","fields":["start","title"],"submitted":true}
```

`fields` is the sorted product-field list.

#### `events.cancel`

Input is `event_id`, optional `message`, and optional `notify_guests` defaulting true. CLI flags are `--message` and `--notify-guests <true|false>`. Success:

```json
{"event_id":"evt_example","notify_guests":true,"submitted":true}
```

### Guests

#### `guests.list`

Input is `event_id` plus collection fields. Partiful backend authorization is authoritative; the client does not impose a host-only preflight. Success:

```json
{"guests":[],"next_cursor":null,"has_more":false}
```

Each guest contains the application-visible roster fields `display_name`, `rsvp_status`, `party_size`, and `cohost`. It excludes raw guest, user, Firebase, and Firestore identifiers.

#### `guests.invite`

Input is `event_id`, one contact selector, and optional invitation `message`. CLI supplies the message with `--message-file <file|->`; MCP supplies `message` directly. Omission submits the application-compatible empty message. Success:

```json
{"event_id":"evt_example","submitted":true}
```

### RSVP

#### `rsvp.get`

Input is `event_id`. Success:

```json
{"rsvp":{"event_id":"evt_example","status":"going"}}
```

`status` is null when no current guest exists; otherwise it uses the full reviewed read-status enum.

#### `rsvp.set`

Input is a discriminated union on `status`:

- `going` and `not-going` require `display_name`, positive `party_size`, and IANA `timezone`; accept repeated `plus_ones`, optional `message`, and optional `questionnaire_response`; and require `party_size = 1 + len(plus_ones)`.
- `interested` and `not-interested` reject every RSVP-profile field and map to the evidenced interest toggle with `interested: true` or `false`.

CLI flags are `--status`, `--display-name`, `--party-size`, repeated `--plus-one`, `--timezone`, `--message-file <file|->`, and `--questionnaire-response-file <file|->`. MCP fields use the logical names and accept message text and the questionnaire object directly. Success:

```json
{"event_id":"evt_example","intent":"not-interested","submitted":true}
```

`intent` echoes the write request; it is not an observed RSVP `status`. The vocabularies are deliberately different: `not-going` writes the private `DECLINED` state and may later read as `declined`, while `not-interested` toggles interest off and does not promise a specific subsequent RSVP status. The shared input schema must encode and test all four union branches. Different private transports do not create separate public commands.

### Contacts

#### `contacts.list`

Input is optional `query` (`--query`) plus collection fields. The filter is a case-insensitive display-name query. Success:

```json
{"contacts":[{"contact_ref":"opaque","display_name":"Example Contact","shared_event_count":2}],"next_cursor":null,"has_more":false}
```

### Cohosts

The contact commands take `event_id` and exactly one contact selector.

- `cohosts.invite` returns status `invited`.
- `cohosts.revoke-invite` returns status `revoked`.
- `cohosts.remove` returns status `removed`.

Their success shape is:

```json
{"event_id":"evt_example","cohost":{"display_name":"Example Contact","status":"invited"}}
```

`cohosts.link.create` and `cohosts.link.revoke` take only `event_id`. Creation may return a reviewed Partiful URL:

```json
{"event_id":"evt_example","link":{"url":"https://partiful.com/e/evt_example?accept-cohost=token","state":"active"}}
```

Revocation returns:

```json
{"event_id":"evt_example","link":{"url":null,"state":"revoked"}}
```

### Blasts

#### `blasts.send`

Input is `event_id`, fixed audience `all-guests`, required message, and `show_on_event_page` defaulting false. CLI flags are:

```text
--audience all-guests --message-file <file|-> [--show-on-event-page]
```

MCP accepts `message` directly. Success:

```json
{"event_id":"evt_example","submitted":true,"audience":"all-guests","show_on_event_page":false,"recipient_status":"not-reported"}
```

`submitted` does not claim delivery.

### Posters

`posters.list` takes collection fields. `posters.search` requires `query` (`--query`) plus collection fields. Success:

```json
{"posters":[{"poster_id":"poster_example","name":"Example poster","url":"https://example.invalid/poster.jpg","content_type":"image/jpeg","width":1200,"height":630,"tags":[],"categories":[]}],"next_cursor":null,"has_more":false}
```

## Error and exit contract

| Exit | Stable error types |
| --- | --- |
| 0 | success |
| 2 | `usage.invalid`, `input.invalid`, `match.ambiguous` |
| 3 | `auth.required`, `auth.expired`, `auth.human_required` |
| 4 | `permission.denied` |
| 5 | `resource.not_found` |
| 6 | `state.conflict` |
| 7 | reserved; the removed plan-token safety errors are not public |
| 8 | `remote.unavailable`, `remote.rate_limited` |
| 9 | `contract.protocol_changed` |
| 10 | `internal.failure` |

A CLI JSON error is:

```json
{"error":{"type":"permission.denied","code":"PERMISSION_DENIED","message":"Partiful denied this operation.","retryable":false,"details":{}}}
```

MCP returns the same classified fields through its structured tool error. Remote bodies, credentials, messages, private identifiers, and transport metadata never enter errors.

## Authorization rule

The client checks only authentication and local input invariants. Partiful backend authorization is final for event, guest, RSVP, cohost, and blast operations. A denial returns `permission.denied` and no partial result. The client does not convert an empty or denied roster into successful empty data, and it does not invent host-only or cohost-only policy beyond Partiful.

## Excluded public capabilities

There are no MCP-only product operations. Firebase Auth calls, Firestore reads, callable-function names, storage paths, token refresh, contact-reference derivation, and transport diagnostics remain private adapters or infrastructure. They have no CLI command and no MCP tool. The public model also has no generic request command, raw identifier lookup, mutation planner, or confirmation-token command.

## Generation and verification requirements

One typed command registry generates CLI parsing, CLI schema output, MCP tool registration, MCP input schemas, dry-run support, and documentation checks. Verification must assert:

1. all 24 command IDs and paths are unique;
2. the disposition set is exactly `paired` or `cli-only`;
3. there are 23 unique paired MCP tool names;
4. `auth.login` is the only CLI-only path;
5. every mutation supports dry-run and no read supports it;
6. every contact-consuming operation uses the shared exclusive selector;
7. `rsvp.set` validates all four discriminated branches;
8. CLI file/stdin adapters and MCP direct-content fields reach the same shared operation input; and
9. every local relative Markdown link resolves.
