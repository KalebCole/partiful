# Authentication and credential lifecycle design

## Status

Accepted design for the first Partiful CLI and MCP release. This document resolves [issue #10](https://github.com/KalebCole/partiful/issues/10) and is normative for authentication, credential storage, refresh, logout, diagnostics, and related test seams.

The public command inventory remains governed by [`../command-model.md`](../command-model.md). The observed wire protocol remains governed by [`../../evidence/research/authentication.md`](../../evidence/research/authentication.md) and [`../../spec/partiful.openapi.json`](../../spec/partiful.openapi.json).

## Outcome

The CLI and MCP adapters use one application-layer credential provider. Only the CLI conducts interactive login. Both adapters observe the same active account, storage backend, refresh coordination, rotated credentials, error classification, and redaction rules.

The first release supports one active Partiful account per installation.

## Evidence boundary

The observed login sequence is:

1. collect a phone number;
2. call `sendAuthCodeTrusted`;
3. collect the SMS verification code;
4. exchange the code for a Partiful custom token;
5. exchange the custom token for a Firebase session.

Evidence does not establish a browser callback flow, token lifetime, refresh-token rotation policy, remote logout or revocation, or authentication rate limits. The implementation must not invent these properties.

When the transport does not provide expiry, the credential record preserves expiry as unknown. New evidence must update the research ledger and transport contract before it expands retry classification or protocol behavior.

## Precedents

### gogcli behaviors adopted

[gogcli](https://gogcli.sh/) is the concrete CLI and agent-automation precedent, especially its [auth and secret-storage contract](https://gogcli.sh/spec.html):

- fixed-schema CLI and MCP operations instead of a generic argv bridge;
- platform credential storage by default;
- a headless fallback backend;
- bounded credential-store operations so hidden prompts cannot hang agents;
- auth diagnostics that distinguish configuration, storage, and live-token health;
- durable secrets outside dotfile-synced configuration paths;
- stable machine output with prompts and diagnostics on stderr;
- cleanup of safe current and legacy locations;
- read-after-write verification before authentication reports success.

The read-after-write rule is required by observed gogcli failures where Keychain or a file backend appeared to accept login but did not preserve a usable token.

### gogcli behaviors not transferred

Partiful does not copy gogcli's Google-specific complexity:

- multiple accounts or OAuth clients;
- browser, manual URL, remote two-step, service-account, or imported-token flows;
- OAuth scope selection;
- direct access-token command flags;
- a generic encrypted file-keyring password environment variable.

Partiful's file fallback is permission-protected. It is never described as encrypted. Adding encryption without an independently protected key would move the secret rather than protect it.

## Architecture

```text
CLI adapter ──┐
              ├─ CredentialProvider
MCP adapter ──┘    ├─ CredentialStore
                   ├─ AuthTransport
                   ├─ RefreshCoordinator
                   ├─ Clock
                   └─ DiagnosticSink
```

Neither public adapter reads, writes, caches, refreshes, formats, or deletes credentials directly.

### `CredentialProvider`

Owns login commit, status acquisition, pre-request refresh, authenticated-rejection recovery, logout, active-account replacement, and stable auth errors.

### `CredentialStore`

Stores complete versioned credential records. Production implementations are the platform credential store and the protected file fallback. Every implementation must support load, verified commit, and deletion through the same behavioral contract.

### `AuthTransport`

Implements only evidenced Partiful and Firebase authentication exchanges. It returns typed values and classified protocol errors, never raw secret-bearing response objects.

Every Firebase Identity Toolkit and Secure Token request includes
`Referer: https://partiful.com/`. Omitting this evidenced invariant produces
`403 API_KEY_HTTP_REFERRER_BLOCKED` and is a transport-contract failure, not an
expired-session signal.

### `RefreshCoordinator`

Combines in-process singleflight with a bounded cross-process lock. The lock contains no credentials and is shared by CLI and MCP processes for one installation.

The cross-process lock is owned by an open OS handle: POSIX uses an advisory
`flock`, and Windows uses the corresponding handle-owned file lock. Process
exit releases it automatically. The implementation does not use a persistent
PID marker as the source of lock ownership. Acquisition is bounded, and every
success, error, cancellation, panic-recovery, and timeout path releases an
acquired lock.

### `Clock`

Supplies current time and makes refresh boundaries deterministic in tests.

### `DiagnosticSink`

Accepts structured, already-classified diagnostics. It cannot accept credential records, raw HTTP bodies, or secret-bearing transport values.

## Interactive login

`partiful auth login` is the only interactive authentication operation.

1. Require a private interactive terminal. Under `--no-input`, redirected input, or MCP, return `auth.human_required` before sending a request.
2. Prompt for the phone number and SMS verification code. The SMS code uses hidden input. Neither value enters process arguments, environment variables, stdout, MCP messages, or logs.
3. Complete the evidenced Partiful and Firebase exchanges.
4. Validate a non-empty access token, non-empty refresh token, and usable account identity. Preserve unknown expiry as unknown.
5. Acquire the credential lock.
6. Re-read the active generation because another process may have changed it while login was in progress.
7. Commit the new record to the inactive storage slot with generation `active + 1`.
8. Read the slot back and require byte-for-byte equality plus structural validity.
9. Report success only after verified persistence.

All failures before verified persistence leave the previously active slot unchanged. A process crash after a verified slot write but before success output may leave the new account active; the next invocation resolves the highest valid generation deterministically.

A successful new login replaces the active account without requiring logout. A failed login does not replace it.

## Credential record

The private record contains:

- schema version;
- monotonic generation;
- account identity needed for status and account binding;
- access token;
- refresh token;
- absolute access-token expiry when known;
- the installation secret used to derive opaque `contact_ref` values.

The record excludes SMS codes, custom tokens, raw transport responses, and browser state.

Unknown schema versions fail as `auth.store_unavailable`. A migration must be explicit and testable; readers do not guess field meanings.

## Storage selection

### Platform store first

Use macOS Keychain, Windows Credential Manager, or Linux Secret Service by default.

Every OS-store read, write, delete, and enumeration operation has a bounded wall-clock timeout. Defaults are 30 seconds on macOS and 10 seconds elsewhere, following gogcli's operational precedent. A timeout returns `auth.store_unavailable`; a hidden permission prompt must not hang an MCP connection.

Store outcomes are classified as:

- **available:** use the OS store;
- **unsupported or operationally unavailable during initial selection:** use the file fallback and emit one stderr warning;
- **explicit user denial:** fail without fallback;
- **failure after an existing OS-backed account was selected:** fail without silently changing backends or pretending the account is missing.

A non-secret backend marker makes later selection deterministic. If the marker is absent, discovery checks the OS backend before the file backend. The marker never contains account identity or credentials.

### Protected file fallback

The fallback lives under the platform user-data directory, not the dotfile-synced configuration directory. It uses:

- a private directory with mode `0700` where POSIX modes apply;
- owner-only files with mode `0600` where POSIX modes apply;
- `lstat` checks and rejection of symlinks, hard-linked credential files, and non-regular files;
- same-filesystem temporary writes;
- complete file sync before atomic rename;
- parent-directory sync where supported;
- a bounded cross-process lock.

The CLI warns that the fallback is protected by account and filesystem permissions rather than encryption.

### Two-slot commit

Each backend exposes two fixed credential slots. Readers validate both and select the valid record with the highest generation.

To commit:

1. identify the inactive or lower-generation slot;
2. write the complete next-generation record there;
3. read it back;
4. require exact bytes and structural validity;
5. publish success by releasing the lock.

A zero-byte, truncated, mismatched, or invalid write cannot replace the still-valid higher-generation slot. A later successful commit may reuse the invalid slot. Equal generations with different records are corruption and fail closed.

Commit failure also releases the lock. Lock release is unconditional after
acquisition; only the verified record publication is conditional.

This protocol is used for login and refresh rotation. It improves on a single-key verified write because persistence failure preserves the previous usable generation.

## Credential acquisition and refresh

The early-refresh window is five minutes when expiry is known.

1. Load the highest valid credential generation.
2. If the access token is healthy, return it without network access.
3. If it is expiring, enter in-process singleflight and acquire the bounded cross-process lock.
4. Re-read storage after acquiring the lock.
5. If another process wrote a higher healthy generation, use it and skip refresh.
6. Otherwise call the refresh transport once.
7. Validate the complete refresh response.
8. Commit it as the next generation through the two-slot protocol.
9. Release the lock; waiters re-read storage.

If early refresh fails while the old access token is still valid, return the old token and emit a structured warning. Once the old token is expired, refresh failure is a hard `auth.expired` or classified remote error.

A persistence failure after a successful remote refresh is hard. The provider does not publish the new token to callers until verified persistence succeeds.

### Unavoidable rotation gap

The remote refresh and local commit cannot form one transaction. A process can crash after Firebase rotates a refresh token but before the new token is stored. Evidence does not prove whether the old refresh token remains valid.

The next invocation may therefore receive a rejected old refresh token. It returns `auth.required` with login remediation. It does not loop, infer rotation policy, or erase the record automatically.

## Authenticated rejection and request retry

Pre-request refresh is the primary mechanism. A dispatched operation is retried only when the operation's transport contract has positive evidence for an exact authentication rejection.

The current verified classifier is HTTP `401` with callable status `UNAUTHENTICATED`, as recorded for signed-out `getContacts`. A generic `401`, Firebase-originated error, network failure, timeout, `429`, or `5xx` is not automatically equivalent.

For a read operation whose contract declares the exact classifier:

1. refresh under the shared coordinator;
2. persist the next generation;
3. retry the read once.

For a mutation, do not retry after dispatch unless its specific transport contract proves both an exact authentication rejection and non-execution or supplies a verified idempotency mechanism. An ambiguous mutation result is returned without retry. No operation retries more than once.

New retry-eligible classifiers require evidence-led updates to the research ledger, OpenAPI contract, and tests.

## Status

`auth status` answers whether the shared session is usable, not only whether bytes exist.

- Healthy known-expiry credentials require no network request.
- Expiring credentials use the normal refresh path.
- Unknown-expiry credentials report `unknown` without inventing health; a live check is performed only when the command contract explicitly requests it in a later version.
- Missing credentials report `missing` without error.
- Inaccessible or corrupt storage returns a classified error rather than `missing`.

CLI JSON and MCP return the same public fields. They may include authentication state, masked account display, expiry when known, and backend kind. They never include tokens, private transport identifiers, credential paths, or raw backend errors.

## MCP behavior

The MCP server starts when credentials are missing, expired, or inaccessible. It does not conduct login.

Public tools remain usable. A protected tool without usable credentials returns:

```json
{
  "type": "auth.required",
  "message": "Partiful authentication is required.",
  "retryable": false,
  "details": {
    "remediation": "Run `partiful auth login` in a private terminal, then retry."
  }
}
```

The MCP process keeps no independent durable credential copy. Any in-memory snapshot is generation-bound and discarded when storage presents a newer generation or logout removes both slots.

## Logout

`auth logout` is local, lock-protected, and idempotent.

1. Acquire the shared credential lock.
2. Delete both slots from the selected backend.
3. Attempt safe deletion from the other supported backend and declared legacy locations.
4. Clear the backend marker and in-process snapshots.
5. Report success only when all existing known credential copies are removed.

Partial deletion is a hard `auth.persistence_failed` error. Its details identify backend kinds only, never paths, key names, account identity, or secret values.

Logout makes no network request and does not claim remote revocation. A previously issued access token may remain remotely valid until Partiful or Firebase expires it.

## Errors

Stable public auth errors are:

| Type | Meaning | Retryable |
| --- | --- | --- |
| `auth.required` | No usable session; interactive login is required. | false |
| `auth.expired` | The access token is expired and refresh did not recover it. | depends on classified cause |
| `auth.human_required` | An interactive-only operation was attempted non-interactively. | false |
| `auth.store_unavailable` | The selected credential backend is denied, timed out, corrupt, or unavailable. | true only for transient timeout/unavailability |
| `auth.persistence_failed` | A verified credential commit or complete logout did not finish. | false until storage is repaired |
| `auth.busy` | The bounded credential lock timed out. | true |

Raw backend errors remain private. Public details contain only a safe backend kind, state, and actionable remediation.

## Redaction

Secrets include access tokens, refresh tokens, custom tokens, SMS codes, cookies, authorization headers, credential payloads, installation secrets, and raw authentication response bodies. Phone numbers are private and are masked in permitted status output.

Secret-bearing domain types do not implement ordinary diagnostic string formatting. Persistence uses a dedicated private codec; diagnostic and public JSON serializers cannot serialize the credential record.

Redaction occurs before formatting and applies to:

- CLI stdout and stderr;
- human and JSON errors;
- verbose logs and traces;
- MCP results and structured errors;
- HTTP diagnostics;
- panic recovery;
- test failure output.

HTTP diagnostics use an allowlist of safe method, host class, operation ID, status, duration, and request correlation metadata. Headers and bodies are sensitive by default.

Ordinary application-visible event data follows the command output contract and is not removed merely because a request used authentication.

## Test seams

Production dependencies are replaceable by deterministic interfaces:

- fake clock;
- in-memory and fault-injected credential stores;
- scripted auth transport;
- deterministic in-process and cross-process locks;
- concurrency barriers;
- captured diagnostic sink;
- private persistence codec and public redaction serializers.

The same credential-provider contract suite runs beneath both CLI and MCP adapters.

### Required tests

Storage and login:

- OS available selects OS storage without warning;
- OS unavailable on first selection chooses fallback with one warning;
- explicit denial fails without fallback;
- selected OS backend later unavailable does not silently switch accounts;
- write succeeds but read-back is empty, mismatched, or invalid;
- failed inactive-slot write preserves the active generation;
- equal conflicting generations fail closed;
- login failure preserves the existing account;
- successful login replaces it;
- file permissions and alias defenses;
- store call timeout returns within a scheduling margin.

Refresh and concurrency:

- healthy token causes no refresh;
- one refresh under a concurrent stampede;
- waiter re-read skips redundant refresh;
- bounded lock timeout and crash release;
- successful rotation is persisted before use;
- rotation persistence failure does not publish new credentials;
- early-refresh failure returns a still-valid token with warning;
- expired-token failure is hard;
- remote/local crash gap returns login remediation without a loop.

Requests:

- exact evidenced read rejection refreshes and retries once;
- generic `401`, `5xx`, timeout, and connection reset do not qualify;
- ambiguous mutation responses are not retried;
- no operation retries more than once.

Logout and MCP:

- logout removes both slots and safe legacy copies;
- repeated logout succeeds;
- partial deletion is reported without secret metadata;
- MCP starts without credentials;
- protected tools return `auth.required` and exact remediation;
- public tools remain available;
- CLI login, CLI status, MCP status, refresh, and logout observe one shared generation sequence.

Redaction:

- a generated secret corpus never appears in any output channel;
- raw response bodies and headers are excluded by default;
- phone numbers are masked;
- storage serialization preserves secrets while public and diagnostic serialization cannot expose them.

## Mechanical acceptance

The authentication implementation is complete only when:

1. CLI and MCP dependency graphs contain one credential-provider implementation;
2. `auth login` cannot be registered as an MCP tool;
3. every OS-store operation is timeout-bounded;
4. every successful login and refresh has a verified two-slot commit;
5. cross-process tests prove one refresh and waiter re-read;
6. mutation retry tests fail on every ambiguous result class;
7. logout tests prove deletion across both slots and supported legacy locations;
8. the secret corpus is absent from stdout, stderr, JSON, MCP, logs, traces, and test errors;
9. tests use no live SMS traffic and perform no Partiful mutation; and
10. all local Markdown links resolve.

## Deferred work

The following require new evidence or a separate product decision:

- browser-based login;
- remote token revocation;
- multiple accounts;
- explicit encrypted file backend with externally supplied key material;
- live status checks for unknown-expiry tokens;
- additional authentication-rejection classifiers;
- retry of dispatched mutations.
