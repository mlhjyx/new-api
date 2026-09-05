# Fork-only workflow registration and dispatch

Status: `EXTERNAL_CONFIGURATION_REQUIRED`

This document is an operator contract, not evidence that remote GitHub state
has already been changed. The parent operator owns every command and readback.

## Fixed repository and branch

- Fork repository: `mlhjyx/new-api`.
- Approved upstream base: `bde9b2f44887d34ec54799ae191d50f97914359e`.
- Release branch: `production-parity/settlement-readback-v1`.
- The release branch must be created from that exact base and protected before
  the Phase A-E pull request is merged.
- Never merge, rebase, or copy the 163 unreviewed upstream commits from the
  fork's historical `main` merely to register a workflow.

## Why default-branch registration is required

GitHub resolves a manual `workflow_dispatch` definition from the repository's
default branch. Therefore the release workflow must exist on the fork default branch
before an operator can dispatch it with `--ref`.

After the exact-base pull request is independently reviewed and merged, the
parent operator must explicitly choose and verify
`production-parity/settlement-readback-v1` as the default branch of the fork.
That is a separate external configuration action; this source change does not
perform or authorize it. The unreviewed historical `main` must not be promoted
or merged as a workaround.

## Explicit fork dispatch

After default-branch and protection readback, dispatch only with an exact full
merged commit:

```bash
gh workflow run growthos-new-api-release.yml \
  --repo mlhjyx/new-api \
  --ref production-parity/settlement-readback-v1 \
  -f revision=<40-character-merged-commit>
```

Every subsequent GitHub readback must also use the explicit fork selector, for
example `gh run list --repo mlhjyx/new-api` and
`gh run view <run-id> --repo mlhjyx/new-api`. Never rely on the current working
directory, browser repository context, or the `gh` default repository.

The workflow's own repository guard, exact branch/commit admission, protected
environment, license gate and digest-only image contract remain mandatory.

## Interrupted publication recovery

Dispatch the same exact revision again only while it is still the protected
release branch head. A complete corresponding-source release is reusable when
its asset set is exactly the three original uploaded source assets and all are
publicly retrievable. Any extra asset (including image SBOM, receipt or
attestation), duplicate name, or unfinished upload holds recovery even when
the image tag is currently absent. The workflow reconstructs
the archive and provenance from the admitted Git revision using the original
source SBOM, then requires byte-for-byte archive and provenance equality. A
new scanner invocation cannot replace the original SBOM or its recorded digest.
The verified source release is never recreated or overwritten. This permits
recovery after a public source readback, registry login, or Docker build failure
before an image tag exists.

Recovery remains fail-closed for a missing/partial source asset, a mismatch, or
an existing exact image tag. A tag published before a later signing/evidence
failure is **not** trusted merely because it has the expected name or labels.
Do not rerun a build against that tag, delete it, replace release assets, or
attest an unproven existing image. Preserve the failed run and immutable assets,
hold cutover, and use an independently reviewed forward-fix revision. Automatic
post-image recovery is not implemented; it requires verification of the original
build attestation and a digest-bound evidence-resume protocol. This is a bounded
source-stage retry, not a claim that arbitrary partial releases are recoverable.
