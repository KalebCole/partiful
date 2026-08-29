Your agent can run the party.

# Partiful

Partiful is an agent-native CLI and MCP server for event planning. It supports
events, guests, RSVPs, contacts, posters, cohosts, authentication, and text
blast coordination through a JSON-first interface.

## Manuscript status

This repository contains a curated OpenAPI contract and its evidence ledger.
Printing Press can use `spec/partiful.openapi.json` as its sole generator input.
Verified observations, public-asset research, protocol references, and explicit
uncertainty support the contract. This repository does not contain a generated
CLI or MCP server.

## Architecture

[`docs/architecture.md`](docs/architecture.md) records the accepted public
interface and application architecture, including the gogcli and Printing Press
precedents.

## Release verification

The release candidate is the 40-character commit SHA selected and recorded
by the authorized release operator. Release publication requires the native
GitHub commit status context exactly `partiful/live-worker-profiles` on that
immutable commit SHA.

An authorized release operator must use this order:

1. Check out the candidate SHA and verify it before local verification.
2. Run the local verifier. Redirect its output if useful.
3. Verify that `HEAD` still equals the candidate SHA after verification.
4. Use repository-scoped GitHub access to post one `success` status for that
   same SHA.
5. Only after the status exists, push a semantic version tag that points to the
   same SHA, or dispatch the release workflow for an existing tag that points
   to it.

Safe shell templates:

```sh
set -euo pipefail
expected_sha='<40-character-candidate-commit>'
git checkout --detach "$expected_sha"
candidate_sha=$(git rev-parse HEAD)
test "$candidate_sha" = "$expected_sha"
python3 scripts/verify_implementation_worker_profiles.py >/dev/null
test "$(git rev-parse HEAD)" = "$candidate_sha"

gh api --method POST "repos/KalebCole/partiful/statuses/${candidate_sha}" \
  -f state=success \
  -f context=partiful/live-worker-profiles \
  -f 'description=Live worker profiles verified'
```

The status post must contain only `state=success`, context
`partiful/live-worker-profiles`, and the generic description shown above. Do
not post profile output, profile content, credential values, or private data.
Posting the status and pushing a tag are external mutations for an authorized
release operator only. Do not request or show token values.

After the status post, use one release path. For a new tag:

```sh
set -euo pipefail
tag='vX.Y.Z'
test "$(git rev-parse HEAD)" = "$candidate_sha"
git tag "$tag" "$candidate_sha"
git push origin "$tag"
```

For an existing tag, verify that it points to the candidate SHA before dispatch:

```sh
set -euo pipefail
tag='vX.Y.Z'
test "$(git rev-list -n 1 "$tag^{commit}")" = "$candidate_sha"
gh workflow run release.yml --ref "$tag" -f version="$tag"
```

The release workflow resolves the tag commit, then queries GitHub's native
combined commit-status endpoint for that selected SHA. It fails closed for
missing, pending, error, failure, wrong-context, ambiguous, malformed, or
different-SHA evidence. GitHub status values are `error`, `failure`, `pending`,
and `success`; only `success` is accepted. Hosted GitHub runners do not run or
receive live Hermes profiles.
