# Evidence guide

This directory supports the Partiful API manuscript. `ledger.json` maps each
material OpenAPI claim to a classification and a source. The `research/`
documents consolidate technical findings. The `observations/` files preserve
redacted, structured results from dated read-only sessions.

## Evidence classes

- `dated-live-observation`: a dated request directly observed a response.
- `current-first-party-public-asset-research`: a public Partiful web asset
  established current client behavior.
- `official-protocol-specification`: official Firebase or Firestore material
  established generic protocol behavior.
- `unverified-inference`: retained contract detail lacks retained, accessible
  support and requires confirmation.
- `explicit-unknown`: the manuscript intentionally makes no remote claim.

Do not call an `unverified-inference` evidence. It is a precise verification
queue item for Printing Press.

## Observation redaction

Observation files retain dates, HTTP metadata, aggregate counts, and JSON
paths or types. Redaction removes tokens, verification codes, phone numbers,
email addresses, private user IDs, event IDs, invite codes, and personal
content. The files must not be used to reconstruct identities.

## Public Firebase configuration

The value `AIzaSyCky6PJ7cHRdBKk5X7gjuWERWaKWBHr4_k` is Partiful's public
Firebase web API key. Browser clients must send this public client
configuration to Firebase Identity Toolkit and Secure Token endpoints. It is
not a private credential. Tokens and account identifiers remain private.

## Unverified inferences

Some exact contract claim keys came only from excluded source material. The
ledger preserves every claim key, classifies each affected fact as
`unverified-inference`, and points here. Printing Press must confirm these facts
before it treats them as remote behavior. This includes the affected
`uploadEventPhoto` request and operation-level classification.

## Explicit unknowns

`explicit-unknown` means that observed material does not establish a status,
body, side effect, or error mapping. Implementations must fail closed rather
than guess.

## Verification boundary

Printing Press may perform authenticated, read-only verification. It must not
send authentication messages, invite guests, change RSVP state, create or
change events, upload media, manage cohosts, send text blasts, or make any other
mutation without a separate approved plan. Guest and contact data remain
permission-gated and private.
