# GrowthOS exact-base release pull request draft

Target branch: `production-parity/settlement-readback-v1`

This draft is an implementation handoff, not a ready-to-submit pull request.
The parent operator must re-read the upstream pull request template and write
the final concise summary in their own words before opening any remote PR.

## Scope

- preserve the New API and `github.com/QuantumNous/new-api` identities;
- add settlement-readback contracts implemented in Phases A-D;
- add immutable corresponding-source, exact OCI, license and SBOM controls;
- publish only `ghcr.io/mlhjyx/new-api:sha-<full-commit>` after all release
  gates, independent review and explicit external-action authorization pass.

## AI assistance disclosure

OpenAI Codex substantially assisted with analysis, implementation, tests and
this draft. No claim is made that a human authored the generated changes.

Human review status: `NOT_YET_COMPLETED`.

Publication status: `NOT_AUTHORIZED_BY_THIS_ARTIFACT`.

The final PR must not check the upstream template's human-review boxes until a
named human has actually performed those checks. The engineering license review
is now `APPROVED`, but this draft still does not authorize an OCI or source
release and is not legal advice.
The pinned Go 1.26.1 vet run keeps an exact, fail-closed baseline. Since the
upstream v1.0.0-rc.44 sync the baseline is empty and `go vet ./...` must pass
outright; any new finding requires a separate review of
`release/go-vet-baseline.txt`.
