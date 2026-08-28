# Create-event ID semantics

## Scope and method

This note classifies `OP11-CREATE-EVENT-ID` with a bounded,
credential-free public-asset probe on August 28, 2026. The probe fetched only
the public Partiful login page, its named build manifest, the manifest's
`/create` JavaScript asset, and the public shared application asset. It did not
seek or use a credential. It did not call `createEvent` or make any live
mutation.

The probe ran at `2026-08-28T17:02:10Z`. The login page named Next.js build
`XoD6YZ4QlKDKpKvBo-WXS` and deployment
`dpl_4v9QFfUe3BMAxGHTkhoR7n5URH6H`. The build manifest maps `/create` to the
retained page asset.

## Exact sources

| Source | Bytes | SHA-256 | Last-Modified |
| --- | ---: | --- | --- |
| [login page](https://partiful.com/login) | 32,147 | `4d0adffa7351fb719fcf6bf61847a3055956cc0ad416eb5fcc11dff14219d9ec` | `Fri, 28 Aug 2026 16:13:41 GMT` |
| [build manifest](https://partiful.com/_next/static/XoD6YZ4QlKDKpKvBo-WXS/_buildManifest.js?dpl=dpl_4v9QFfUe3BMAxGHTkhoR7n5URH6H) | 11,750 | `7df0479beab97444f9619534ef2903e89a7a690601b758523b6550fa9713e2ff` | `Fri, 28 Aug 2026 16:13:30 GMT` |
| [`/create` page asset](https://partiful.com/_next/static/chunks/pages/create-b2bce16bea04026a.js?dpl=dpl_4v9QFfUe3BMAxGHTkhoR7n5URH6H) | 37,175 | `e10c289d0396d49dce55e354191d4b6cf87b527ee4355566727fa3200a9d00f3` | `Fri, 28 Aug 2026 16:13:27 GMT` |
| [shared `_app` asset](https://partiful.com/_next/static/chunks/pages/_app-08f1358a22e2f54b.js?dpl=dpl_4v9QFfUe3BMAxGHTkhoR7n5URH6H) | 2,371,473 | `73b988c55401f68c265ac6b223b85ceae67f3be76efea2d56fd10333b7920bbf` | `Fri, 28 Aug 2026 16:13:31 GMT` |

All four public sources returned HTTP `200`. No response contained an account
identifier, event identifier, token, or personal content.

## Observed completion flow

The `/create` asset's `createEvent` helper awaits the callable and returns the
callable SDK's decoded `.data` value. The create page receives that same value
and, without a list lookup or other identity inference:

1. passes it unchanged as `eventId` to the create-success analytics call;
2. passes it unchanged as `eventId` to the event-count analytics call; and
3. builds the application route with `"/e/".concat(value)` before navigation.

The shared `_app` asset's callable SDK reads a successful response's top-level
`data` member. If that member is absent, it reads the legacy top-level `result`
member. It returns the decoded member as `.data` and raises `Response is missing
data field.` if both members are absent. This agrees with the generic callable
envelope behavior retained in
[`events.md`](events.md#event-write-callable-wrapper-and-serialization).

## Gate classification and exact extraction rule

`OP11-CREATE-EVENT-ID` is **supported**: the SDK-decoded `createEvent`
completion value is the current first-party application-visible event ID.

The exact extraction rule is:

1. decode the successful Firebase callable JSON envelope;
2. select its current top-level `data` member or, when `data` is absent, its
   legacy top-level `result` member; and
3. expose that decoded member as `event_id` only when it is a non-empty JSON
   string. Fail closed for an absent member, an empty string, or any other JSON
   type.

The non-empty string check is a consumer safety rule. The current page does not
validate the value before it passes the value to analytics and URL
concatenation. The direct first-party use establishes identifier semantics; it
does not justify accepting a malformed completion value.

## Evidence limit

This public-asset evidence establishes the meaning and direct extraction of a
well-formed completion value. It does not establish that the event was
persisted, its final state, endpoint-specific authorization, unobserved status
codes, or any business-success field beyond the returned identifier. A
consumer must not infer identity from an event list when the completion value
is missing or malformed.
