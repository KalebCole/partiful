# Authentication

## Scope and provenance

On 2026-08-11 at `02:30:19Z`, the authorized operator completed an attended
authentication session against Partiful's live endpoints. The agent did not
enter credentials, send codes, use browser login, or access the owner's
tokens. A sanitized evidence artifact retaining only HTTP metadata and
JSON paths/types is committed at
`evidence/observations/auth.json`. An independent scan
confirmed no phone numbers, verification codes, tokens, API keys, or user IDs
are present in the artifact.

Initial privacy-safe negative probes were performed at approximately `02:33Z`
using empty or invalid phone input and malformed requests that could not
contact a person, deliver a message, or authenticate. Repeat fake-token probes
for Firebase sign-in, refresh, and lookup were recorded at
`2026-08-11T03:54:17Z` in
`evidence/observations/auth-errors.json`. All probes observed
only error responses.

## Public authentication-request assets (captured 2026-08-28)

On 2026-08-28, a credential-free fetch of the public
[`/login`](https://partiful.com/login) page identified build
`XoD6YZ4QlKDKpKvBo-WXS` and deployment
`dpl_4v9QFfUe3BMAxGHTkhoR7n5URH6H`. The request classifications below come
from these first-party public assets. The retained asset URLs, build ID,
deployment ID, SHA-256 hashes, and byte counts record the 2026-08-28 capture.
A recheck on 2026-08-29 returned HTTP `404`; these records do not make the
assets fetchable now.

| Asset | SHA-256 | Size | Relevant code |
| --- | --- | ---: | --- |
| [`pages/_app-08f1358a22e2f54b.js`](https://partiful.com/_next/static/chunks/pages/_app-08f1358a22e2f54b.js?dpl=dpl_4v9QFfUe3BMAxGHTkhoR7n5URH6H) | `73b988c55401f68c265ac6b223b85ceae67f3be76efea2d56fd10333b7920bbf` | 2,371,473 bytes | modules `52039`, `95722`, and the bundled Firebase Auth client |
| [`9552-9fd3f6ad8118c783.js`](https://partiful.com/_next/static/chunks/9552-9fd3f6ad8118c783.js?dpl=dpl_4v9QFfUe3BMAxGHTkhoR7n5URH6H) | `348c61036bfdd097591f37a40f08b1c48265fdf45a7b3dd7812e8cff88252883` | 9,980 bytes | login form in the captured client |
| [`9512-fcece14a3f9598b3.js`](https://partiful.com/_next/static/chunks/9512-fcece14a3f9598b3.js?dpl=dpl_4v9QFfUe3BMAxGHTkhoR7n5URH6H) | `8031e98f68d1f81e9f1843ab268e1d5575a49f0a68a6827b9e821bd09d4e4098` | 8,287 bytes | channel selector in the captured client |

The public-asset classification used no credential, private identifier, account
response, authentication message, or live mutation. The `current` labels in
the section headings mean the client state captured on 2026-08-28; they do not
mean that the assets remain available now.

### Current `sendAuthCodeTrusted` request

The trusted-phone branch in the captured client passes these callable `params`:

- required on this client path: string `displayName`, string `phoneNumber`,
  and boolean `silent`;
- optional: string `channelPreference`, with captured values `SMS` or
  `WHATSAPP`, and string `captchaToken`; the wrapper removes either member
  when its value is `undefined`;
- omitted: `eventId` and `useAppleBusinessUpdates`.

The shared callable wrapper always constructs `data.params` and can add string
`userId`, string `amplitudeDeviceId`, integer `amplitudeSessionId`, and
boolean-true `adminAccessRequested` when available. An unavailable `userId`
is omitted during JSON serialization. The wrapper's `deviceInfo` source is
unset in this asset, and this call does not supply `deviceInfo`, so it is
omitted. The Firebase callable SDK places this object under the one top-level
`data` member.

This evidence establishes the captured client request as of 2026-08-28, not
server-side required-field validation or accepted values outside the captured
projection.

### Current `getLoginToken` request

The verification flow in the captured client constructs callable `params` with
required string `phoneNumber`, string `authCode`, and object `utms`. String
`affiliateId` is optional and is omitted when its local-storage source is
`undefined`. The same wrapper adds the optional metadata fields described
above when available, removes undefined parameter members, omits
`deviceInfo`, and places the request under the one top-level `data` member.

This evidence establishes the captured client projection and omission
behavior. It does not establish server-side required-field validation or the
allowed contents of `utms`.

### Current `signInWithCustomToken` request

The bundled Firebase Auth client calls
`/v1/accounts:signInWithCustomToken` with an application-JSON object that has
required string `token` and required boolean `returnSecureToken: true`. It
constructs no other JSON member for this call. The API-key query parameter
remains redacted in this repository; the observed Referer restriction is
documented separately below.

## Direct observations

### sendAuthCodeTrusted

Operator-attended success: `POST /sendAuthCodeTrusted` returned HTTP `200`,
`Content-Type: application/json; charset=utf-8`, 22-byte body. No structured
fields were present in the sanitized shape  -  the response is a minimal
acknowledgment.

Agent negative probe (invalid phone `"not-a-phone"`): HTTP `500`,
`Content-Type: application/json; charset=utf-8`, body
`{"error":{"message":"INTERNAL","status":"INTERNAL"}}`. This confirms the
callable error envelope shape but does not establish a contract-level failure
mapping since the request was deliberately malformed.

### getLoginToken

Operator-attended success: `POST /getLoginToken` returned HTTP `200`,
`Content-Type: application/json; charset=utf-8`, 849-byte body with shape:
- `result.data.token` (string)

The `result.data` wrapper is consistent with the March 24, 2026 browser
interception. Additional fields may exist (849 bytes vs. one recorded path);
they are not claimed.

Operator-attended wrong-code failure: HTTP `403`,
`Content-Type: application/json; charset=utf-8`, 127-byte body with shape:
- `error.details.authErrorCode` (string)
- `error.message` (string)
- `error.status` (string)

Agent negative probe (empty phone, code `"000000"`): HTTP `400`,
`Content-Type: application/json; charset=utf-8`, body with shape:
- `error.details.authErrorCode` (string)
- `error.message` (string)
- `error.status` (string)

The error envelope is consistent across both failure modes.

### signInWithCustomToken

Operator-attended success: `POST /v1/accounts:signInWithCustomToken` returned
HTTP `200`, `Content-Type: application/json; charset=UTF-8`, 1453-byte body
with shape:
- `expiresIn` (string)
- `idToken` (string)
- `kind` (string)
- `refreshToken` (string)

This is consistent with the March 24, 2026 browser interception and Firebase
Identity Toolkit documentation.

Agent negative probe (fake token, with `Referer: https://partiful.com/`):
HTTP `400`, `Content-Type: application/json; charset=UTF-8`, body with shape:
- `error.code` (number)
- `error.errors[].domain` (string)
- `error.errors[].message` (string)
- `error.errors[].reason` (string)
- `error.message` (string)

Message value: `"INVALID_CUSTOM_TOKEN : Invalid assertion format. 3 dot
separated segments required."` (no credential content).

Agent negative probe (fake token, no `Referer` header): HTTP `403`,
`Content-Type: application/json; charset=UTF-8`, body with shape:
- `error.code` (number)
- `error.details[].@type` (string)
- `error.errors[].domain` (string)
- `error.errors[].message` (string)
- `error.errors[].reason` (string)
- `error.message` (string)
- `error.status` (string)

The Firebase API key is configured with HTTP referrer restrictions. Requests
without an allowed `Referer` header receive `403
API_KEY_HTTP_REFERRER_BLOCKED`. This is a Firebase project configuration fact,
not a Partiful callable behavior.

### refreshToken

Operator-attended success: `POST /v1/token` returned HTTP `200`,
`Content-Type: application/json; charset=UTF-8`, 2632-byte body with shape:
- `access_token` (string)
- `expires_in` (string)
- `id_token` (string)
- `project_id` (string)
- `refresh_token` (string)
- `token_type` (string)
- `user_id` (string)

Operator-attended invalid-token failure: HTTP `400`,
`Content-Type: application/json; charset=UTF-8`, 111-byte body with shape:
- `error.code` (number)
- `error.message` (string)
- `error.status` (string)

Agent negative probe (fake token, with `Referer`): HTTP `400`,
`Content-Type: application/json; charset=UTF-8`, body with same shape:
- `error.code` (number)
- `error.message` (string)
- `error.status` (string)

Consistent error envelope across owner and agent probes.

### lookupFirebaseUser

Operator-attended success: `POST /v1/accounts:lookup` returned HTTP `200`,
`Content-Type: application/json; charset=UTF-8`, 765-byte body with shape:
- `kind` (string)
- `users[].createdAt` (string)
- `users[].customAuth` (boolean)
- `users[].displayName` (string)
- `users[].lastLoginAt` (string)
- `users[].lastRefreshAt` (string)
- `users[].localId` (string)
- `users[].phoneNumber` (string)
- `users[].photoUrl` (string)
- `users[].providerUserInfo[].phoneNumber` (string)
- `users[].providerUserInfo[].providerId` (string)
- `users[].providerUserInfo[].rawId` (string)
- `users[].validSince` (string)

Operator-attended invalid-token failure: HTTP `400`,
`Content-Type: application/json; charset=UTF-8`, 206-byte body with shape:
- `error.code` (number)
- `error.errors[].domain` (string)
- `error.errors[].message` (string)
- `error.errors[].reason` (string)
- `error.message` (string)

Agent negative probe (fake token, with `Referer`): HTTP `400`,
`Content-Type: application/json; charset=UTF-8`, body with identical shape.

## Firebase API key referrer restriction

The Firebase Identity Toolkit and Secure Token endpoints require an HTTP
`Referer` header matching an allowed pattern (observed: `https://partiful.com/`).
Requests without a matching referrer receive `403
PERMISSION_DENIED` with reason `API_KEY_HTTP_REFERRER_BLOCKED`. This is a
Firebase project configuration fact observed during agent negative probes and
does not affect Partiful callable endpoints (`sendAuthCodeTrusted`,
`getLoginToken`), which do not use API key authentication.

## Evidence conclusions

Each operation's only accepted success is HTTP `200` with the documented
shape. The schema-free default response remains an explicit unknown.

The following operation-specific error statuses and precise error schemas were
observed under controlled conditions:

- **getLoginToken `403`**: observed from a controlled wrong-code attempt.
  Error shape: `{error: {details: {authErrorCode}, message, status}}`.
- **signInWithCustomToken `400`**: observed from a privacy-safe fake-token
  probe. Error shape:
  `{error: {code, message, errors[]: {domain, message, reason}}}`.
- **refreshToken `400`**: observed from both operator-attended and agent
  fake-token probe. Error shape:
  `{error: {code, message, status}}`.
- **lookupFirebaseUser `400`**: observed from both operator-attended and agent
  fake-token probe. Error shape:
  `{error: {code, message, errors[]: {domain, message, reason}}}`.
- **sendAuthCodeTrusted**: no error status promoted. The 500 from a
  deliberately malformed probe does not establish a contract failure mapping.
  The 200 body shape is unclaimed.

Other statuses, no-response behavior, network failures, rate limiting, and
success bodies outside the documented shapes remain unknown.

A structured sanitized artifact for the agent fake-token probes is committed
at `evidence/observations/auth-errors.json`.

## lookupFirebaseUser evidence limit

The `lookupFirebaseUser` observation establishes one way to retrieve account
display metadata after sign-in. It does not establish that every caller must
make this request or that it is the only remote path to the same data.

## Explicit unknowns

- Whether `sendAuthCodeTrusted` differs from `sendAuthCode` in behavior beyond
  endpoint name.
- The full set of `authErrorCode` values and their semantics.
- Whether the 22-byte `sendAuthCodeTrusted` success body ever contains fields.
- Rate-limiting behavior on any authentication endpoint.
- Token lifetimes, refresh token rotation policy, and session duration.
- Whether the Firebase referrer restriction accepts additional `Referer`
  values or patterns.
- Additional fields in `getLoginToken` success (849 bytes, only `result.data.token`
  recorded in sanitized shape).
- Whether `lookupFirebaseUser` returns additional user fields for accounts with
  email or other providers.

## Firebase public API key

The Partiful browser client supplies the public Firebase web API key
`AIzaSyCky6PJ7cHRdBKk5X7gjuWERWaKWBHr4_k` in the `key` query parameter for
Firebase Identity Toolkit and Secure Token requests. This is public client
configuration. It is not a private credential and does not grant account access.

## Official Firebase callable protocol

Firebase callable functions use an HTTP `200` response with a `result` envelope
for protocol success. See <https://firebase.google.com/docs/functions/callable-reference>.
