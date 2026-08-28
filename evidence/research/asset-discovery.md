# Bounded first-party asset discovery

## Scope and decision

This note closes evidence gate `DRIFT17-ASSET-DISCOVERY`.[1] The gate requires a fixed HTTPS seed set, same-origin traversal and resource limits, an extraction grammar, an independently accepted project discriminator, and a dated redacted manifest that demonstrates deterministic zero/one/many classification.[1][2]

The retained observation is `evidence/observations/asset-discovery-2026-08-28.json`.[1] It is candidate evidence only.[2] Public assets do not update the accepted configuration, and this observation made no live mutation.[2]

## Fixed discovery policy

`asset-discovery-v1` has this complete policy, which makes the bounds required by the accepted drift decision concrete.[1][2]

- Seed set: exactly `https://partiful.com/login`.[3]
- Request method: `GET` only.[2]
- Origin: accept only HTTPS URLs whose lowercase host is exactly `partiful.com` and whose effective port is 443.[2]
- Redirects: at most three per request; every redirect target and final URL must remain in the accepted origin. Otherwise classify the run as inconclusive and stop.[2]
- Traversal: parse the seed as HTML5-tolerant markup, inspect only `script[src]` values, resolve each value against the final seed URL, remove fragments and query strings before retention, keep first-seen order, de-duplicate by the resulting URL, and fetch only same-origin paths ending in `.js` or under `/_next/static/`. Do not recursively traverse JavaScript references.[1][2]
- Limits: at most 32 JavaScript assets, 4,194,304 bytes per response, 33,554,432 bytes for the seed and all assets, 15 seconds per request, and 60 seconds wall-clock. Exceeding a limit is inconclusive, not zero candidates.[1][2]
- Retention: retain observation time, final query-free source URLs, SHA-256 content hashes, byte counts, content types, redirect counts, extractor version, candidate counts, classification, project-discriminator result, and 16-hex-character SHA-256 prefixes for configuration comparison. Retain no full assets, response bodies, query strings, headers, cookies, or raw configuration.[1][2]

The current run fetched one seed and 10 directly referenced same-origin JavaScript assets. It retained 3,045,384 bytes only long enough to hash and inspect them, then discarded all response bodies.[3]

## Extraction grammar

For each JavaScript asset, search raw bytes for a JavaScript object region that contains both properties below within 4,096 bytes, in either order. This is the fixed grammar required by the evidence gate.[1][2]

1. an optionally quoted `apiKey` property, followed by `:`, then a single- or double-quoted value that matches `AIza[0-9A-Za-z_-]{35}` exactly;[1][2]
2. an optionally quoted `projectId` property, followed by `:`, then a single- or double-quoted lowercase value that matches `[a-z0-9-]{3,80}` exactly.[1][2]

A candidate identity is the exact `(apiKey, projectId)` pair. De-duplicate identical pairs across all assets before classification. A key-like token without the paired `projectId`, an escaped or computed property, an out-of-window pair, or a pair from a rejected URL is not a candidate. Parser failure is inconclusive.[1][2]

## Expected-project discriminator

The discriminator is exact `projectId` equality with the repository-owned Firebase project segment in `spec/partiful.openapi.json#/paths/~1v1~1projects~1getpartiful~1databases~1(default)~1documents~1events~1{eventId}/get`.[2] This source is independent of the fetched asset and predates this observation.[2] The manifest retains only its SHA-256 prefix.[1]

Only candidates that match this discriminator enter classification. Let `n` be the number of distinct matching candidate identities.[2]

| `n` | Classification | Disposition |
| ---: | --- | --- |
| `0` | `zero` | Inconclusive; nominate nothing. |
| `1` | `one` | Candidate evidence only; a later accepted probe can evaluate it. |
| `>=2` | `many` | Inconclusive; nominate nothing. |

The manifest includes fixed synthetic vectors for `n = 0`, `n = 1`, and `n = 2`; each maps to exactly one row above. Limit, network, redirect, content-type, and parser failures bypass this count and remain inconclusive.[2]

## Dated observation

At `2026-08-28T17:02:21Z`, the bounded run found one distinct extracted candidate and one exact expected-project match, so its classification is `one`.[3]

The discovered candidate fingerprint equals the accepted repository fingerprint.[3]

No configuration change is nominated.[2][3]

The manifest records all 11 source hashes, fixed limits, the discriminator result, both redacted fingerprints, and the zero/one/many vectors.[1][3]

This observation does not run the disabled Firebase negative probe, does not establish that any key will remain accepted, and does not authorize an automatic update. `DRIFT17-FIREBASE-PROBE` remains a separate gate.[2]

## Sources

[1] [Evidence bounded first-party asset discovery](https://github.com/KalebCole/partiful/issues/28)
[2] [Define public-configuration and remote-contract drift detection](https://github.com/KalebCole/partiful/issues/17)
[3] [Partiful login seed](https://partiful.com/login)
