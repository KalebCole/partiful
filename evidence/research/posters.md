# Posters

## Scope and provenance

On 2026-08-11 at `01:08:30Z`, an unauthenticated, read-only request was made
to the contract's public endpoint:

`GET https://assets.getpartiful.com/posters.json`

No credentials, mutations, uploads, personal data, or third-party services
were used. The response body was inspected locally and then deleted. A raw
fixture is intentionally not committed: aggregate shape evidence is sufficient
and avoids retaining a 1.1 MB copy of 2,114 public creative records.

## Direct observations

The unconditional request returned HTTP `200`, `Content-Type:
application/json`, no `Content-Range`, and a 1,125,932-byte JSON array with
2,114 entries. Its SHA-256 was
`35e22005b19dd5795cecf582dee4c4fe4ddc5349e3142f0aae8014f4e471cc6e`.
The response included:

- `ETag: W/"9dbafb9aedef91b93a1b94e8969ae8b5"`
- `x-goog-generation: 1786410042765282`
- `Last-Modified: Tue, 11 Aug 2026 01:00:42 GMT`
- `Cache-Control: public, max-age=600, s-maxage=600,
  stale-while-revalidate=86400, stale-if-error=86400`

Every entry had `id`, `name`, `url`, `contentType`, `width`, `height`, `tags`,
and `categories`. IDs and names were non-empty strings; URLs were HTTPS
strings; tags and categories were arrays containing only strings. A repeated
privacy-safe GET at `01:42:58Z` returned the same byte length and SHA-256;
every `contentType` was a string and the complete observed value set was
`image/avif`, `image/gif`, `image/jpeg`, and `image/png`. Width and height were
integers except that one entry used `null` for both. One ID occurred twice at
non-adjacent positions; the two entries differed in tags and categories. No
uniqueness or deduplication claim is supported.

An `If-None-Match` request using the observed ETag returned `304` with no body.
A deliberately nonmatching `If-Match` request still returned `200` with the
full body, so safe resumption must not depend on that precondition being
enforced. A read-only request with the unsatisfiable range
`Range: bytes=999999999-` returned `416`, `Content-Range: bytes */1125932`,
and a zero-byte body.

## Evidence conclusions

HTTP `200` was observed with the documented complete array representation. The
schema-free default response remains an explicit unknown. The observed `416`
resulted only from a Range request, so it does not establish ordinary failure
mapping. Other statuses, no-response behavior, network failures, rate
limiting, and malformed success bodies remain unknown.

The observation supplies every documented source field on every entry,
including the one entry whose dimensions are explicitly `null`. It does not
establish an alternate identifier or fallback value.

The observed response has no remote page envelope or cursor. The observation
does not establish remote pagination, cursor behavior, or any local pagination
policy.

## Explicit unknowns

- Whether this endpoint contains every poster usable anywhere in Partiful.
- How often catalog membership or order changes.
- The semantics of the duplicate ID and nullable dimensions.
- Status codes and bodies for failures of an ordinary unconditional request.
- Whether cache validators or storage-generation headers retain their current
  behavior.

No behavior beyond the observed representation is inferred from these
unknowns.

## Poster interface

Observed entries include `id`, `name`, `url`, `contentType`, `width`, `height`,
`tags`, and `categories`. Event image data can also carry `createdAt`, `version`,
`size`, `ordersMap`, `cardOrdersMap`, `bgColor`, and `blurHash`. See
[Event images](event-images.md#poster-built-in-library).

## Poster catalog fetch

Use unauthenticated `GET https://assets.getpartiful.com/posters.json`. Preserve
server order and duplicates. Do not infer remote pagination.

## Dated poster catalog observation

The direct observation and digest are under [Direct observations](#direct-observations).
