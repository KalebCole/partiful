# RSVPs


## Public asset: Scope and provenance

This note records privacy-safe research from Partiful's current first-party
public web assets and the official Firebase callable protocol. The assets were
fetched without authentication on August 12, 2026. No account-scoped request,
credential, real identifier, questionnaire answer, message, RSVP mutation, or
SMS was used.

The public [login page](https://partiful.com/login) supplied Next build ID
`z1npyrEHkwRMn_JlKXQXR` and deployment query
`dpl_4w28tFBmSUwoToDpQB8CU8gt16sL`. The build manifest assigns the RSVP UI to
`/e/[event]`.

Exact public sources:

- [build manifest](https://partiful.com/_next/static/z1npyrEHkwRMn_JlKXQXR/_buildManifest.js?dpl=dpl_4w28tFBmSUwoToDpQB8CU8gt16sL)
- [shared `_app` asset](https://partiful.com/_next/static/chunks/pages/_app-05be110884cfdc20.js?dpl=dpl_4w28tFBmSUwoToDpQB8CU8gt16sL)
- [interest asset](https://partiful.com/_next/static/chunks/1945-cbff097107005c38.js?dpl=dpl_4w28tFBmSUwoToDpQB8CU8gt16sL)
- [RSVP controls asset](https://partiful.com/_next/static/chunks/1585-abd7081ec2f9f79f.js?dpl=dpl_4w28tFBmSUwoToDpQB8CU8gt16sL)
- [RSVP flow asset](https://partiful.com/_next/static/chunks/2565-91cc334b3dc48a18.js?dpl=dpl_4w28tFBmSUwoToDpQB8CU8gt16sL)
- [`/e/[event]` page asset](https://partiful.com/_next/static/chunks/pages/e/%5Bevent%5D-f833fec21304964a.js?dpl=dpl_4w28tFBmSUwoToDpQB8CU8gt16sL)
- [official Firebase callable protocol](https://firebase.google.com/docs/functions/callable-reference)

The manifest, `_app`, and `1945` responses reported `Last-Modified: Tue, 11
Aug 2026 21:17:57 GMT`. The other three responses reported `Last-Modified:
Tue, 11 Aug 2026 21:18:03 GMT`.

Public assets establish current client inputs and completion checks. They do
not establish endpoint business success, failure statuses, authorization, or
unseen response variants.

## Public asset: Callable transport

Shared module `95722` calls Firebase `httpsCallable`. It supplies the caller's
argument as the callable `data` value, removes `undefined` direct properties
from `params`, and returns the decoded callable `result`. The wrapper can add
`deviceInfo`, `amplitudeDeviceId`, `amplitudeSessionId`,
`adminAccessRequested`, and `userId` when those values are available. The RSVP
operation modules supply `params`; they do not inspect these generic metadata
members.

The official Firebase protocol requires a JSON request with one top-level
`data` field. A successful callable trigger has HTTP `200` and a JSON response
with `result`; an `error` member means failure. This generic rule supports the
callable success status and result envelope only. It does not prove that a
Partiful mutation changed state.

## Public asset: Guest status and request values

Shared module `54257` contains the same closed 16-value `GuestStatus` enum
recorded in the event-read note. The RSVP buttons use the narrow response set
`GOING`, `MAYBE`, and `DECLINED`; another helper restricts a no-maybe control
to `GOING` and `DECLINED`.

The current client uses these exact request values:

```text
going      -> addGuest rsvp.status = "GOING"
not-going  -> addGuest rsvp.status = "DECLINED"
interested -> markEventInterest interested = true
```

Interest removal uses `interested = false`.

Read status stays separate. A present `CurrentGuest.status` can use the full
16-value lossless `EventReadRsvp` mapping. This does not make all 16 values
writable.

## Public asset: Capacity-consuming guest statuses

Shared event helper module `50218` calculates `attendedGuestCount` by summing
only statuses accepted by guest-status helper `i9`. Module `54257` defines
that set as `GOING` and `APPROVED`. A current guest in any other status is not
subtracted from remaining capacity when planning an update.

## Public asset: getCurrentGuest

Page module `77504` sends:

```js
call("getCurrentGuest", { params: { eventId } })
```

Page module `52105` reads `response.data.currentGuest`,
`response.data.anchorGuest`, and `response.data.tickets`. It only makes this
call while augmenting an existing real-time guest that has linked plus ones,
an anchor guest, or tickets. Otherwise it uses the real-time guest directly.
The state update is null-safe, but this callable path does not demonstrate a
null `currentGuest` response.

One dated HTTP `200` observation had an object at
`result.data.currentGuest`, and one Firestore guest HTTP `200` observation had
the same document ID and status. These observations do not establish callable
nullability, missing-status behavior, ordinary non-null plus-one shapes, or
alternate responses. Those variants stay unknown.

## Public asset: addGuest request

Module `82565` constructs the RSVP form and completion flow. Module `52105`
adds the current guest ID and page-only values, then sends:

```js
call("addGuest", { params: { eventId, rsvp } })
```

For the direct, non-protected, non-discover path, the current client request
shape is:

```json
{
  "eventId": "event input",
  "rsvp": {
    "name": "current guest or authenticated profile display name",
    "count": "partySize",
    "plusOnes": [
      { "name": "plus-one display name" }
    ],
    "status": "GOING or DECLINED",
    "timezone": "input IANA timezone",
    "shouldFollowOrgs": false
  }
}
```

The client adds a nonempty message as `message` after trimming it. An absent
message is omitted, not sent as JSON null. Questionnaire answers are strings
keyed by question ID. For a going response, the client adds:

```json
{
  "questionnaireResponse": {
    "questionnaireVersion": "event.questionnaireVersions.length - 1",
    "answers": {
      "question ID": "answer"
    }
  }
}
```

The questionnaire step is skipped for `DECLINED`.

Shared module `7073` recognizes named plus ones as `{name}`. It also recognizes
private linked, phone-contact, and user-contact variants. The documented
shape below retains only named plus ones. Module `64949` removes `phoneNumber`,
`channelPreference`, and `captchaToken` before `addGuest`, and removes the
embedded `user` object from a linked plus one.

For an existing guest, module `52105` adds its private `guestId`. For no guest,
`guestId` is `undefined` and is omitted. The same module adds the browser's
timezone. It can add a stored event password. The documented direct path omits
`password`, `emailInvitationId`, `image`, and `_discoverSource`; the evidence
does not establish behavior for protected or invitation-specific paths.

The client always submits `name`, `count`, `plusOnes`, `status`, and
`shouldFollowOrgs`. Going and not-going therefore have exact wire status
values. The required name, guest ID, questionnaire version, and
protected-event or ticketing conditions depend on other client state.

## Public asset: addGuest completion

Module `52105` destructures decoded `addGuest.data`. This rejects only
null/undefined at the client boundary; JavaScript permits object
destructuring from other JSON value kinds. The client therefore does not
establish an operation-wide `data` type. When data is an object, it splits
optional `previousStatus` and `linkedPlusOneFailures`, then treats the
remaining properties as the updated guest for local state.
Module `82565` uses the optional previous status for analytics and optional
linked-plus-one failures for a warning. It checks no endpoint success boolean.

The narrow remote completion contract is therefore HTTP `200` under the
official callable protocol with:

```json
{
  "result": {
    "data": {}
  }
}
```

`data` must be an object, but the client requires no business field. All its
properties remain unclaimed. This completion means only that the
submitted callable request completed. It does not prove the stored RSVP,
delivery, notification, or another remote side effect.

## Public asset: markEventInterest request and completion

Module `64951` sends the supplied params to `markEventInterest` and returns
decoded `response.data`. Module `34679` sends:

```js
{ eventId, interested, source }
```

The direct event page passes its optional `source` from the URL query. When
the URL has no string `source`, the value is `undefined`, module `95722`
removes it from `params`, and the JSON request contains only `eventId` and
`interested`. A direct event-page-equivalent CLI request therefore omits
`source`. The same toggle sends
`interested: false` for removal.

The client accepts completion only when decoded `data.success` is truthy and
`data.interested` equals the requested boolean. Otherwise it rolls back its
optimistic local value. This is a JavaScript client predicate, not a remote
field-type claim. A representative accepted completion is:

```json
{
  "result": {
    "data": {
      "success": true,
      "interested": true
    }
  }
}
```

The `interested` member must equal the submitted value, including `false` for
removal. This is a current client completion check, not an observation of
Partiful business state.

## Public asset: Evidence limits

The public assets do not establish all `getCurrentGuest` variants, a profile
name source when no current guest exists, all event precondition fields, any
mutation response, or persisted state after a write. The dated read evidence
below adds one explicit null current guest and event safeguard aggregates. It
does not add mutation response, failure, or persisted-state evidence.


## Dated read: Scope and provenance

On August 12, 2026, the authorized operator authorized authenticated, read-only
RSVP evidence capture. The supplied sanitized artifact is
`evidence/observations/rsvp-reads.json`, captured at
`2026-08-12T13:49:55.431Z`. The agent did not use credentials, make a live
request, or handle raw response values. The raw private capture was deleted
before this contract work.

The artifact contains only counts, field names, value types, bounded numeric
ranges, safe enum values, and HTTP statuses. It contains no event, guest, user,
or account identifiers; names; messages; questionnaire answers; credentials;
tokens; phone numbers; or email addresses.

## Dated read: Event coverage

The read found 36 upcoming and 294 past list events, with 330 unique event
details. All 330 `getEventInfo` calls returned HTTP `200`. The detail artifact
records 90 field names and their presence counts, but it retains values only
for the allowlisted RSVP safeguard fields.

Forty-one list events had no inline guest and 289 had an inline guest. One
candidate from each class was used for the current-guest variant checks. Those
two probes do not establish the frequency of either callable variant.

## Dated read: Current-guest variants

One `getCurrentGuest` call returned HTTP `200` with explicit null at
`result.data.currentGuest`. This is authoritative evidence for the
no-current-guest marker. The property itself was present.

One other call returned HTTP `200` with an object. The sanitized object had
string `id`, `name`, `status`, and `userId`; number `count` and
`plusOneCount`; null `anchorGuestId`, `plusOnes`, and `rsvpDate`; object
`invitedBy`; and array `rsvpHistory`. Their values were not retained.

The explicit null and selected object are the only observed callable variants.
A missing `currentGuest` property, scalar, array, object without a valid ID or
status, non-number count, non-null plus-one variant, unsupported status, and
failure response remain unknown.

Current public asset module `52105`, cited in
[Public asset request mapping](#public-asset-addguest-request),
adds `guestId` for an existing guest and omits it for no guest. Combined with
the dated null and object observations, the narrow selection is exact:

- explicit `currentGuest: null` selects create and omits `guestId`;
- an object with valid ID and status selects update and includes its private
  ID; the mutation-compatible subset also requires nonnegative integer count;
  and
- every other variant fails closed.

## Dated read: Event safeguard observations

The table keeps raw property presence separate from the artifact's normalized
null buckets.

| Field | Raw presence | Observed retained variants |
| --- | ---: | --- |
| `rsvpsEnabled` | 330 | 325 true, 5 false |
| `atCapacity` | 330 | 315 false, 15 true |
| `plusOneNamesRequired` | 330 | 317 false, 13 true |
| `questionnaireEnabled` | 93 | 47 true, 46 false, 237 absent |
| `questionnaireVersions` | 330 | 89 arrays and 241 null; array lengths 1 (44), 2 (39), or 3 (6) |
| `ticketing` | 101 | 91 objects, 10 explicit null, 229 absent |
| `guestAction` | 22 | 18 `APPLY`, 4 `RSVP`, 308 absent |
| `maxCountPerGuest` | 36 | 36 numbers from 1 through 10, 294 absent |
| `maxCapacity` | 68 | 52 numbers from 8 through 300, 16 explicit null, 262 absent |
| `remainingCapacity` | 52 | 52 numbers from -9 through 116, 278 absent |
| `enableWaitlist` | 78 | 33 true, 31 false, 14 explicit null, 252 absent |
| `password` | 0 | 330 absent |
| `passwordProtected` | 0 | 330 absent |

The normalized artifact uses null for an absent optional value in several
aggregate groups. The raw presence list is authoritative when absence and
explicit null differ. A mutation safeguard snapshot must retain that
distinction instead of converting every absence to JSON null.

## Dated read: Observed safeguard limits

The dated evidence establishes field presence and variants, not server
enforcement. It does not establish application, ticket purchase, password,
waitlist, capacity enforcement, or party-size validation behavior.

Current public asset module `82565`, cited in
[Public asset request mapping](#public-asset-addguest-request),
sets questionnaire version to `questionnaireVersions.length - 1` for going
and skips the questionnaire for `DECLINED`. Module `7073` supports the named
plus-one request shape. These client behaviors do not establish server
validation rules.

## Dated read: Privacy boundary

Contract tests require the artifact's exact top-level shape, exact
current-guest type inventory, exact safeguard aggregates, and an allowlist of
all 90 retained event field names. Mutation tests reject unknown keys,
unallowlisted field names, and arbitrary values. Separate patterns reject
identity and credential keys, JWT-like strings, phone numbers, email
addresses, messages, display names, and questionnaire answers.

Guest, account, and user identifiers are private and must not appear in public
artifacts.

## Dated read: Remaining unknowns

No RSVP mutation response was observed. Server rejection status and body
mappings, stored RSVP state, delivery, notification behavior, waitlist,
ticketing, application, protected-event submission, inaccessible-event
responses, unobserved current-guest variants, and post-write reads remain
unknown. They are not inferred from the read-only evidence.
