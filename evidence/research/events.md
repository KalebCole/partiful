# Events


## Event read: Scope and provenance

This note records privacy-safe research from Partiful's current first-party
public web assets. The assets were fetched without authentication on
August 12, 2026. No credential, account-scoped request, response containing
event or user data, or mutation was used.

The public login page supplied build ID `z1npyrEHkwRMn_JlKXQXR` and deployment
query `dpl_4w28tFBmSUwoToDpQB8CU8gt16sL`. Its build manifest assigns
`pages/events-16d5030ecfa4fd91.js` to `/events`.

Exact sources:

- [login page](https://partiful.com/login)
- [build manifest](https://partiful.com/_next/static/z1npyrEHkwRMn_JlKXQXR/_buildManifest.js?dpl=dpl_4w28tFBmSUwoToDpQB8CU8gt16sL)
- [`/events` page asset](https://partiful.com/_next/static/chunks/pages/events-16d5030ecfa4fd91.js?dpl=dpl_4w28tFBmSUwoToDpQB8CU8gt16sL)
- [shared `_app` asset](https://partiful.com/_next/static/chunks/pages/_app-05be110884cfdc20.js?dpl=dpl_4w28tFBmSUwoToDpQB8CU8gt16sL)
- [shared chunk containing the past-operation name](https://partiful.com/_next/static/chunks/8733-cef914451f66c66d.js?dpl=dpl_4w28tFBmSUwoToDpQB8CU8gt16sL)

The immutable asset responses reported `Last-Modified: Tue, 11 Aug 2026
21:17:57 GMT`. The build manifest, events asset, and `_app` asset were 11,476,
65,476, and 2,369,333 bytes. Their SHA-256 digests were, respectively:

```text
6014233108449ef82cd4066e6f29476845e3a637e0adfceb79b7cb17fd7d0159
f06c1b0d78258ebb02ad6a95152ef4f9628c25c7ab54720180ed43db97e9f1fa
46a2fe543a4aa17f2cd5d5f8d0547b8f8e692b43b9d7b7061d78f1a01228c9fe
```

Public assets are research evidence. They do not establish unobserved server
statuses, ordering, limits, pagination, or response-field presence.

## Event read: One-response event-list calls

This request classification was rechecked without authentication on August 28,
2026. The public `/events` page supplied build ID
`XoD6YZ4QlKDKpKvBo-WXS` and deployment query
`dpl_4v9QFfUe3BMAxGHTkhoR7n5URH6H`. The checked sources were:

- [`/events`](https://partiful.com/events?dpl=dpl_4v9QFfUe3BMAxGHTkhoR7n5URH6H), SHA-256
  `379cb4ae953c7e4c005ec2d7b11e1a612f8d630d2d71005e539942639b7a9a53`;
- [build manifest](https://partiful.com/_next/static/XoD6YZ4QlKDKpKvBo-WXS/_buildManifest.js?dpl=dpl_4v9QFfUe3BMAxGHTkhoR7n5URH6H),
  SHA-256
  `7df0479beab97444f9619534ef2903e89a7a690601b758523b6550fa9713e2ff`;
- [shared event chunk](https://partiful.com/_next/static/chunks/7066-2c910b2d235d0667.js?dpl=dpl_4v9QFfUe3BMAxGHTkhoR7n5URH6H),
  SHA-256
  `3c235f1486b743d84ca1145b8f2a220d9950932558e1a64ebdfa5df47eca83b4`;
- [`/events` page chunk](https://partiful.com/_next/static/chunks/pages/events-62f501908354b79f.js?dpl=dpl_4v9QFfUe3BMAxGHTkhoR7n5URH6H),
  SHA-256
  `9fb1a887af52127bcf0e8b85935f0bc07c052c9bffe7741b436a9e11817c5116`;
  and
- [shared `_app` chunk](https://partiful.com/_next/static/chunks/pages/_app-08f1358a22e2f54b.js?dpl=dpl_4v9QFfUe3BMAxGHTkhoR7n5URH6H),
  SHA-256
  `73b988c55401f68c265ac6b223b85ceae67f3be76efea2d56fd10333b7920bbf`.

The page asset reported `Last-Modified: Fri, 28 Aug 2026 16:13:30 GMT`.
The `/events` assets name the two callable operations and send empty `params`.
They read `response.data`, then read `upcomingEvents` or `pastEvents`; they send
no `paging` member.

Faithfully deminified modules `48666`, `17350`, `35932`, and `22920`:

```js
const upcomingOperation = "getMyUpcomingEventsForHomePage";

async function getUpcomingHomePageData() {
  return (await call(upcomingOperation, { params: EMPTY_OBJECT })).data ??
    {
      upcomingEvents: EMPTY_ARRAY,
      initialPastEvents: EMPTY_ARRAY,
      eventCategoryCounts: EMPTY_CATEGORY_COUNTS,
    };
}

const pastOperation = "getMyPastEventsForHomePage";

async function getPastHomePageData() {
  return (await call(pastOperation, { params: EMPTY_OBJECT })).data ??
    { pastEvents: EMPTY_ARRAY };
}
```

The names, empty `params`, and absence of a paging argument agree with the
dated one-response observations. The fallback objects are client behavior and
do not describe a successful remote body.

The shared callable wrapper merges each call argument with runtime metadata
and then always assigns `userId`. It adds `deviceInfo` only when shared device
state is non-null, `amplitudeDeviceId` and `amplitudeSessionId` only when each
analytics value is non-null, and `adminAccessRequested: true` only when admin
access is exactly true. The Firebase encoder represents an unavailable
`userId` as JSON `null`. The exact minimum encoded body is therefore:

```json
{"data":{"params":{},"userId":null}}
```

For both operations, `data.params` is required and exactly empty, `data.userId`
is required and nullable, and the four shared metadata properties are
optional under the conditions above. `paging` and every other property are
omitted. Unknown fields are rejected by the reviewed schemas. The callable SDK sends the body to
`https://api.partiful.com/<operation>` with JSON content type. Authorization,
messaging, and App Check headers are independent SDK context and are sent only
when the corresponding token exists. No live callable request was made during
this recheck.

## Event read: Event status

Shared module `18539` contains this exact minified enum construction:

```js
let d=((r={}).UNSAVED="UNSAVED",r.LIVE="PUBLISHED",
  r.CANCELED="CANCELED",r)
```

Faithfully deminified:

```js
const EventStatus = {
  UNSAVED: "UNSAVED",
  LIVE: "PUBLISHED",
  CANCELED: "CANCELED",
};
```

Thus the current client symbolic cases `LIVE` and `CANCELED` correspond to the
wire values `PUBLISHED` and `CANCELED`. The `/events` page checks
`event.status === EventStatus.CANCELED` for the canceled label and checks
`event.status === EventStatus.LIVE` when selecting live invited and attended
events. The exact symbolic-to-wire mapping is:

```text
PUBLISHED -> active
CANCELED  -> cancelled
```

`UNSAVED` is a client state. The homepage list observations do not establish
it as a returned value.

## Event read: Owner membership and hosting

Shared module `50218` exports `Ro`. Its exact minified body is:

```js
function Z(e,t){
  return t.status===l.fb.UNSAVED||null!=e&&t.ownerIds.includes(e)
}
```

Faithfully deminified for published event reads:

```js
function isHost(userId, event) {
  return userId != null && event.ownerIds.includes(userId);
}
```

The `/events` page uses this helper to build `hostedEvents`. It also
independently computes:

```js
const isHost = event.ownerIds.some(ownerId => ownerId === currentUserId);
const guest = "guest" in event ? event.guest : undefined;
```

It supplies that boolean as the event card's `isHost` value. Owner membership therefore means hosting in the current first-party events UI.
The asset does not distinguish a primary host from a cohost.

The route separates non-host records by guest presence:

```js
const hostedEvents = events.filter(event => isHost(currentUserId, event));
const guestEvents = events.filter(
  event => hasGuest(event) || !isHost(currentUserId, event),
);
```

For a representation with `ownerIds`, the current UI separates a non-owner
with a `guest` object from records with neither owner membership nor a guest
object. If `ownerIds` is absent, the role is not established.

## Event read: Guest statuses

Shared module `54257` defines this closed current enum:

```js
const GuestStatus = {
  READY_TO_SEND: "READY_TO_SEND",
  SENDING: "SENDING",
  SEND_ERROR: "SEND_ERROR",
  DELIVERY_ERROR: "DELIVERY_ERROR",
  SENT: "SENT",
  INTERESTED: "INTERESTED",
  WAITLIST: "WAITLIST",
  MAYBE: "MAYBE",
  DECLINED: "DECLINED",
  GOING: "GOING",
  PENDING_APPROVAL: "PENDING_APPROVAL",
  APPROVED: "APPROVED",
  WITHDRAWN: "WITHDRAWN",
  WAITLISTED_FOR_APPROVAL: "WAITLISTED_FOR_APPROVAL",
  REJECTED: "REJECTED",
  RESPONDED_TO_FIND_A_TIME: "RESPONDED_TO_FIND_A_TIME",
};
```

The same module's `VO` helper recognizes all 16 values for current event-card
status display. Other helpers form narrower groups for invite delivery,
attendance, application, and response behavior. None supplies one
semantically lossless grouping for a general read result.

The manuscript records a lossless normalized form by lowercasing each exact
value and replacing underscores with hyphens:

| Remote | Normalized value |
| --- | --- |
| `READY_TO_SEND` | `ready-to-send` |
| `SENDING` | `sending` |
| `SEND_ERROR` | `send-error` |
| `DELIVERY_ERROR` | `delivery-error` |
| `SENT` | `sent` |
| `INTERESTED` | `interested` |
| `WAITLIST` | `waitlist` |
| `MAYBE` | `maybe` |
| `DECLINED` | `declined` |
| `GOING` | `going` |
| `PENDING_APPROVAL` | `pending-approval` |
| `APPROVED` | `approved` |
| `WITHDRAWN` | `withdrawn` |
| `WAITLISTED_FOR_APPROVAL` | `waitlisted-for-approval` |
| `REJECTED` | `rejected` |
| `RESPONDED_TO_FIND_A_TIME` | `responded-to-find-a-time` |

This list does not establish which values the server accepts in write requests.
A missing current guest or status is distinct from an unknown present status.

## Event read: Pagination boundary

The public assets make one call for each selected homepage representation and
do not supply or consume remote paging for these arrays. The dated observation
establishes complete one-response representations of 35 and 294 items,
repeated with identical identity sequences. Their largest observed body was
773,455 bytes.

These facts do not establish remote order meaning, remote limits, server
snapshots, future completeness, or any local pagination policy.

## Event read: Facts not established

These assets do not establish:

- operation-wide presence of optional event fields;
- a primary-host/cohost distinction inside `ownerIds`;
- a universal signed-out event-read policy;
- inaccessible-event behavior or a callable permission response;
- remote paging, ordering keys, limits, snapshot behavior, or future
  completeness;
- null or alternate `getCurrentGuest` variants;
- Firestore event success or not-found behavior; or
- mappings for event address, guest limit, poster, links, or other fields not
  named above.

The dated observation records `403 PERMISSION_DENIED` for Firestore event GET
with both selected and synthetic IDs in one credential context. It does not
establish behavior for other credentials or resources.


## Event write: Scope and provenance

This note records only unauthenticated, first-party public Partiful assets and
official Firebase protocol documents. No account, credential, event ID, guest
ID, request body, or live Partiful write was used. In particular, this research
did not call `createEvent`, `cancelEvent`, any Firestore write, or any upload.

The entry point was <https://partiful.com/login>. On 2026-08-12 it named Next.js
build `2KXQa2wzQWzlyvnJPIrVj` and deployment
`dpl_D9bWXWUaTVfWyHiJz1RT7qdjuzUC`. The exact manifest URL was
<https://partiful.com/_next/static/2KXQa2wzQWzlyvnJPIrVj/_buildManifest.js?dpl=dpl_D9bWXWUaTVfWyHiJz1RT7qdjuzUC>
(SHA-256
`71b824c18ccb5779d024fde62431a302f26f2c0d9ffaafd3640d02d9c86880f3`).
Relevant deployment assets were:

| Asset | SHA-256 | Relevant modules |
|---|---|---|
| <https://partiful.com/_next/static/chunks/pages/create-27dc07c14b6ce0ff.js?dpl=dpl_D9bWXWUaTVfWyHiJz1RT7qdjuzUC> | `a74bf14e957b8c6a70267d519a8973ef6b4d70fac1b90a703b15226f62e3bc19` | `23552`, `25231`, `79372` |
| <https://partiful.com/_next/static/chunks/pages/_app-6d18e4a563898a0d.js?dpl=dpl_D9bWXWUaTVfWyHiJz1RT7qdjuzUC> | `bc5ae6516170e0331b2bac59af77bc92d93f4970a090acbc1e318ac41bc87499` | `18539`, `42919`, `48144`, `50218`, `54257`, `68997`, `92793`, `95722` |
| <https://partiful.com/_next/static/chunks/1585-2532983fb5eac8e0.js?dpl=dpl_D9bWXWUaTVfWyHiJz1RT7qdjuzUC> | `48f1fc96f0aa318eea09d99a89e0f51e1eaf3863bee7e4c43ea045641aa3ac63` | `52630`, location editor |
| <https://partiful.com/_next/static/chunks/6652-bc845eb80e835b16.js?dpl=dpl_D9bWXWUaTVfWyHiJz1RT7qdjuzUC> | `d6b57ad394646129f4702bb7d74aea214d7c43750c3a9898367fb9d4541a71b4` | `30067` |
| <https://partiful.com/_next/static/chunks/2248-0ec69126f468d508.js?dpl=dpl_D9bWXWUaTVfWyHiJz1RT7qdjuzUC> | `c70b07cd5d86f1d6ce8f0e1e02d12fd4a3dbcf6d52a7e023ed01c3dd1f96cde7` | `22248` |
| <https://partiful.com/_next/static/chunks/9580-feaa5337a786edee.js?dpl=dpl_D9bWXWUaTVfWyHiJz1RT7qdjuzUC> | `34f838947de6118a45e388074ee083a2a9a9451f9115d8a08f0ce734c6d19eb7` | `24441` |
| <https://partiful.com/_next/static/chunks/8317-f3e4abcc21cc60c3.js?dpl=dpl_D9bWXWUaTVfWyHiJz1RT7qdjuzUC> | `b0d9c7bd12cdbe021df80f2c990319af85638e2d6499580aeca79f5fcdbbc5e4` | current event editor |
| <https://partiful.com/_next/static/chunks/8290-fee201d02665178a.js?dpl=dpl_D9bWXWUaTVfWyHiJz1RT7qdjuzUC> | `a16edfddf6c6bf48d7e758f578e5fde09733a5c51dfbb4ce8a06d7aa0686d17d` | `95074` |

The current default configuration was
<https://assets.partiful.com/newEventDefaultsConfig.json> (SHA-256
`29bdacdf84365583c4014f3f52d12e94821f9dbcba6e189c085085ebabf59dbc`).
Its selected poster, theme, and reduced-motion effect vary by date, locale, and
motion preference. They are not stable CLI defaults.
Module `79372` also contains the non-international fallback
`theme: "cloudflow"`, `effect: "fireflies"`, `titleFont: "display"`, and
the `Let's Party` poster. The assets do not establish one invariant default
across environments.

## Event write: Callable wrapper and serialization

Module `95722` invokes Firebase callable functions with an object containing
`params`. Its final object always has a `userId` property; the encoder sends
null when that value is unavailable. It can add `deviceInfo`,
`amplitudeDeviceId`, `amplitudeSessionId`, and
`adminAccessRequested: true` when those values apply. It removes direct
`params` properties whose value is `undefined`; therefore analytics and
device properties are not invariant wrapper requirements.

Firebase Functions SDK module `68997` wraps the argument in top-level `data`,
encodes a JavaScript `Date` with `toISOString()`, recursively encodes
`undefined` as `null`, sends bearer authorization when available, and rejects
a success body that has neither top-level `data` nor legacy top-level
`result`. This is generic callable protocol behavior, not a Partiful
business-success predicate.

## Event write: createEvent request

Module `23552` constructs a new event. Module `50218` supplies `title`,
`startDate`, `timezone`, all-zero `guestStatusCounts`, `displaySettings`, and
`status: "UNSAVED"`. The create page adds:

- `showHostList`, `showGuestCount`, `showGuestList`,
  `showActivityTimestamps`, `displayInviteButton`,
  `allowGuestPhotoUpload`, `enableGuestReminders`, `rsvpsEnabled`, and
  `allowGuestsToInviteMutuals`, all `true`;
- `visibility: "public"`;
- `rsvpButtonGlyphType: "emojis"`;
- `image` from the selected built-in poster; and
- a browser IANA timezone, replacing the base default.

The current `guestStatusCounts` keys in module `54257` are
`READY_TO_SEND`, `SENDING`, `SENT`, `SEND_ERROR`, `DELIVERY_ERROR`,
`INTERESTED`, `MAYBE`, `GOING`, `DECLINED`, `WAITLIST`,
`PENDING_APPROVAL`, `APPROVED`, `WITHDRAWN`,
`RESPONDED_TO_FIND_A_TIME`, `WAITLISTED_FOR_APPROVAL`, and `REJECTED`.
Every new-event value is zero.

Module `25231` sends
`createEvent({params:{event,cohostIds,...optionalTicketingValues}})`.
`event` and `cohostIds` are always present in this call. Ticket types, promotion codes, and affiliate values are omitted when absent.
Before sending, it removes `canceledBy`,
`_lastInvitedBy`, and `questionnaire`; it also removes private author or
selector properties inside questionnaire-version and find-a-time values.

Current create representations include:

| Concept | Current event property |
|---|---|
| title | `title` |
| start/end | `startDate` and optional `endDate`, encoded as UTC ISO strings |
| timezone | IANA string in `timezone` |
| description | `description` |
| discoverability | public creation adds `isPublic: true`; the default private flow leaves it absent; fixed `visibility: "public"` is separate |
| free-form address | `locationInfo: {type:"freeform",value:<address>}` |
| guest limit | `maxCapacity`; the editor sends `enableWaitlist` with it |
| links | `customFields` entries with `icon`, `value`, and, for link entries, `url` |
| built-in poster | `image` as described below |

The observed create path sends `cohostIds: []` when it has no cohosts.
Optional values are omitted when not supplied. The current callable serializer
turns an explicitly nested `undefined` into `null`.

## Event write: createEvent completion

Module `25231` returns decoded callable `.data`. The caller uses that value as
an event ID in analytics and in `/e/<value>` navigation. It does not validate a
complete Event, and it does not perform a post-write event read. The only
runtime completion requirement below the caller is the callable SDK's generic
successful HTTP/result-or-data envelope. This evidence establishes the
submitted input only. It does not establish a persisted Event or persisted
state.

## Event write: Built-in poster representation

Module `42919` maps one current poster-catalog entry to:

```text
{
  source: "partiful_posters",
  poster: <the complete selected catalog entry>,
  url, blurHash, contentType, name, height, width
}
```

The last six properties are copied from the selected entry. The assets do not
define behavior for a missing or duplicate poster ID. Uploaded images are
outside this evidence.

The registered catalog permits an omitted `blurHash` and null dimensions.
The callable encoder turns the constructed outer `blurHash: undefined` into
JSON null. Outer `blurHash`, `height`, and `width` are therefore nullable in
the write request; the complete selected catalog record remains bound
unchanged in `poster`.

On 2026-08-12, privacy-safe public GETs to
<https://assets.partiful.com/posters.json> and the already registered
<https://assets.getpartiful.com/posters.json> each returned 1,125,932 bytes
with SHA-256
`35e22005b19dd5795cecf582dee4c4fe4ddc5349e3142f0aae8014f4e471cc6e`.
The current representations are byte-identical. This fact does not establish
future equality between the two hosts.

Module `95074` contains the exact built-in fallback entry with ID and name
`Let's Party`, JPEG content type, 2000 by 2000 dimensions, and the public
catalog URL ending in `/posters/Let's%20Party`. This is one current first-party
entry, not evidence of an invariant default.

## Event write: Firestore event update

Module `52630` is the generic event update helper. It uses the Firebase
Firestore client to update the `events/{eventId}` document and adds
`updatedBy` as a document reference to the current user. It removes derived
event fields and top-level `id`, `createdAt`, and `ref`. A top-level
`null` or `undefined` update value becomes Firestore field deletion.

Module `30067` applies the candidate values to local event state before the
remote calls. It awaits the field-specific calls and module `52630`, calls the
success callback without remote data, and restores the previous local fields
on an exception. It performs no event post-read and consumes no update
response body.

The current event editor does **not** use that generic update for every field.
Module `30067` routes `locationInfo` through `setEventLocation`, public
visibility through `makeEventPublic` or `unpublishEvent`, and
`displaySettings` through `updateDisplaySettings`. No general callable
`updateEvent` path was found. The observed generic Firestore update covers:

| Field | Firestore field paths and typed values |
|---|---|
| `title` | `title` / `stringValue` |
| `description` | `description` / `stringValue`; null deletes |
| `start` | `startDate` / UTC ISO `stringValue` |
| `end` | `endDate` / UTC ISO `stringValue`; null deletes |
| `timezone` | `timezone` / `stringValue` |
| `guestLimit` | `maxCapacity` / `integerValue`, plus `enableWaitlist` / `booleanValue:false`; null deletes both |
| `links` | `customFields` / `arrayValue` of maps containing `icon:"link"`, `value`, and `url`; null deletes |
| `posterId` | `image` / the built-in poster map as Firestore map/array/scalar values; null deletes |

`updatedBy` is a `referenceValue` to
`projects/getpartiful/databases/(default)/documents/users/{currentUserId}`.
This is private request data and must never be printed.

## Event write: Official Firestore PATCH protocol

The official sources are:

- <https://firebase.google.com/docs/firestore/use-rest-api>
- <https://firebase.google.com/docs/firestore/reference/rest/v1/projects.databases.documents/patch>
- <https://firestore.googleapis.com/$discovery/rest?version=v1>

They define bearer Firebase ID-token authorization, the request path
`/v1/projects/getpartiful/databases/(default)/documents/events/{eventId}`,
repeated `updateMask.fieldPaths` query values, `currentDocument.exists` or
`currentDocument.updateTime`, Firestore `Document` input/output, and the
typed-value grammar. The documented request uses sorted, percent-encoded
repeated field paths and `currentDocument.exists=true`.
A masked field omitted from `Document.fields` is deleted. HTTP `200` with a
Document is protocol completion only; it is not proof of Partiful business
state or a complete Event.

## Event write: Update preconditions

The current EventInfo provider permits editing for a current user in
`ownerIds` or through a separate administrator path.
It has no general event-status check. The date editor additionally prevents a
date change when ticketing is present, and prevents it for an event with
`hasGuests: true` after its end plus two hours (or start plus eight hours when
there is no end). No other endpoint permission is inferred.

The current client uses its local event and live Firestore subscription. It
does not establish a server precondition beyond the Firestore protocol
preconditions described above.

## Event write: cancelEvent request and completion

Module `22248` sends:

```text
{
  eventId,
  cancellationMessage,
  shouldSkipNotifyGuests
}
```

The modal starts with `cancellationMessage: ""` and notifications selected, so
the default is `shouldSkipNotifyGuests: false`. The current client awaits the
decoded callable value but does not inspect a business field and performs no
post-write read. Generic callable completion therefore establishes submission
only.

Module `24441` exports this cancel/delete predicate:

```js
function canCancel(event) {
  return event.status === EventStatus.LIVE &&
    event.guestCount != null &&
    event.guestCount > 0 &&
    !isPast(event.startDate);
}
```

The `/events` page separately computes ownership with
`event.ownerIds.some(id => id === currentUserId)` before it adds host-only menu
actions. Thus the observed UI cancel choice requires owner membership, wire
status `PUBLISHED`, a positive guest count, and a future start. UI exposure
also has an unrelated employee-tag branch; it is not promoted to endpoint
authorization. The assets do not establish endpoint authorization.

## Event write: Mutation boundary and remaining unknowns

Partiful endpoint authorization rules, endpoint-specific success meanings,
unobserved response bodies, business errors, and unknown status codes remain
protocol drift. No inaccessible-event permission response was observed or
claimed. This evidence does not establish post-write Event state.


## Scope and provenance

On August 11, 2026, the authorized operator attended authenticated, read-only
calls for event and contact reads. The agent did not handle credentials,
identities, names, event IDs, or contact details. No mutation or additional
live request was made.

The sanitized source is
`evidence/observations/event-and-contact-reads.json`. It contains only HTTP
metadata, normalized paths and types, counts, equality facts, and allowlisted
error codes. The artifact records the observation date and its evidence limits.

## Event list observations

`getMyUpcomingEventsForHomePage` and
`getMyPastEventsForHomePage` each returned HTTP `200` JSON callable
envelopes. The exact array paths were
`result.data.upcomingEvents` and `result.data.pastEvents`. One response held
35 upcoming items and one response held 294 past items.

An immediate repeat returned the same count, identity sequence, and identity
set for each operation. No duplicate identity occurred in either observed
sequence. This is stability evidence for two observations only. It does not
establish an ordering key or snapshot behavior.

The observations establish the named item fields and types in the documented
schemas. Only `id` was present on every item by an explicit aggregate check.
The selected upcoming item also had a guest object with a string status. This supports the event ID and one selected RSVP status. It does not establish
a semantic status mapping or a complete user-role mapping.

No list paging request or response was observed. The arrays were complete
representations in each observed response. Remote pagination, limits, and
future completeness remain unknown.

## Event detail observations

`getEventInfo` returned HTTP `200` for one selected readable event. The
response used `result.data.event`. The one object had the field presence and
value types recorded by the sanitized aggregate. It does not establish
operation-wide field presence, nullability, or alternate variants, so
`EventInfo` has no operation-wide top-level type and no required field list.
Related event-list representations support only the optional `endDate`
string/null and `image` object/null unions; selected-only fields without
related support remain unconstrained.
The same selected event returned HTTP `200` while signed out. This is a fact
about that selected event only. It does not establish that all events are
public.

A synthetic missing ID returned HTTP `404` with callable error status
`NOT_FOUND`. No known inaccessible event was supplied. An authenticated
callable permission denial was not observed and is not claimed.

## Guest and Firestore observations

`getCurrentGuest` returned HTTP `200` for the selected event. The observed
path was `result.data.currentGuest`. It was an object with string `id`,
`name`, `status`, and `userId`, integer `count`, and null `plusOnes`.
This one object does not establish operation-wide field presence,
nullability, or alternate variants, so `CurrentGuest` has no operation-wide
top-level type and no required field list. The schema leaves `count`,
`plusOnes`, and `userId` unconstrained. In particular, the shape of an
ordinary non-null plus-one value is unknown. No null `currentGuest` or other
variant was observed.

`firestoreGetGuest` returned HTTP `200` for the document selected by the
observed current guest ID. The document had `name`, `fields`, `createTime`,
and `updateTime`. The named `count`, `createdAt`, `name`, and `status` fields
were typed Firestore value objects with string children. The document ID
matched the current guest ID, and its status matched the callable guest
status. The complete Firestore typed-value grammar remains unknown.

With the observed authenticated credential, `firestoreGetEvent` returned
HTTP `403` and `PERMISSION_DENIED` for both the selected readable event ID and
a synthetic missing ID. This exact operation and credential behavior does not
show attendee denial, resource existence, or Firestore not-found behavior.

## Contact observations

The public-asset request evidence is in
[Contacts research](contacts.md#exact-callable-argument). It establishes
sibling `params` and `paging` with `maxResults: 1000`; normal loading uses
empty `params`, and `cursor` is null on the first request and a string on later
data-page requests. A separate administrator flow can send boolean
`useAuthUser`; its behavior remains unknown.

The authenticated observation traversed 2,451 contacts twice. Both traversals
returned pages of 1000, 1000, and 451 items, then an empty terminal sentinel.
Each data page had a string `nextCursor`. The terminal response omitted
`nextCursor`. Both traversals had the same private identity sequence and set,
and no duplicate identity was observed.

Every observed contact had a string private `id`, string `name`, and
nonnegative integer `sharedEventCount`. The private identity is sensitive transport data. Contact phone numbers, email
addresses, and identifiers must not be exposed.

The current first-party client traverses the cursor sequence and then filters
names locally. The client also deduplicates by
contact `id` and keeps the first occurrence. This does not establish server
duplicate or ordering behavior. Signed-out `getContacts` returned HTTP `401`
with callable error status `UNAUTHENTICATED`.

## Privacy boundary

The committed artifact contains no raw body values, credentials, identities,
names, IDs, phone numbers, email addresses, or tokens. Tests strictly walk
every aggregate key and string value against an exact allowlist, including
mutation checks for unknown keys and arbitrary values. They also reject
identity or credential value keys, JWT-like values, phone numbers, and email
addresses. Only allowlisted metadata, paths, types, counts, equality facts,
and stable error codes can remain.

## Remaining unknowns

Unsupported statuses and error bodies remain unknown. No claim is made for
rate limiting, request retries, invalid cursors, cursor lifetime or reuse,
backend ordering, snapshot behavior, `useAuthUser`, duplicates outside the
two contact traversals, future catalog completeness, list pagination, or null
and alternate current-guest variants. Event-detail field presence and
alternate variants, including plus-one shape, remain unknown beyond the
stated related event-list support. No inaccessible-event permission probe
exists.

## Update-mask serialization

Firestore PATCH uses repeated `updateMask.fieldPaths` query parameters. See
<https://firebase.google.com/docs/firestore/reference/rest/v1/projects.databases.documents/patch>.

## Official Firestore REST protocol

Official references define paths, PATCH masks, list pagination, and bearer auth:
<https://firebase.google.com/docs/firestore/use-rest-api> and
<https://firestore.googleapis.com/$discovery/rest?version=v1>.
