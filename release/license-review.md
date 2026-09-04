# New API fork license and distribution review

Status: **ENGINEERING HOLD**

This record is engineering evidence for the planned
`ghcr.io/mlhjyx/new-api` OCI distribution and its public corresponding source.
It is not legal advice and does not authorize publication while the
machine-readable review remains `HOLD`.

## Preserved upstream terms

- The project remains New API and keeps the `github.com/QuantumNous/new-api`
  module identity, QuantumNous attribution, UI links and original notices.
- `LICENSE` remains byte-identical to the approved AGPL-3.0 source at
  `bde9b2f44887d34ec54799ae191d50f97914359e`.
- The original `NOTICE` bytes remain an exact prefix; the fork modification
  statement is append-only.
- Direct dependency versions are derived from `go.mod`, `web/bun.lock` and
  `electron/package-lock.json`, and are checked against
  `THIRD-PARTY-LICENSES.md` by `cmd/release-license`.

## Resolved runtime dependency

`github.com/Calcium-Ion/go-epay@v0.0.4` was linked into the payment and
subscription controllers but its exact module archive and tag contained no
license. It was upgraded to the exact commit
`774330a939012a2baab5776e890456d5f15d586e`, represented by pseudo-version
`v0.0.5-0.20260612155053-774330a93901`. That source contains an MIT license
whose SHA-256 is
`86f028deb5895d8994571a0393face710851c29bb938e9b23fe7a9828efd99a8`.
The only application-code difference from v0.0.4 is deletion of a comment;
the payment API is unchanged. Controller, router, model and service tests must
remain green before merge.

## Unresolved automatic peer graph

`@lobehub/icons@5.10.1` imports UI primitives from its peer
`@lobehub/ui@5.15.6`. That peer's dependency graph installs both:

- `@giscus/react@3.1.0`, integrity
  `sha512-0TCO2TvL43+oOdyVVGHDItwxD1UMKP2ZYpT6gXmhFOqfAJtZxTzJ9hkn34iAF/b6YzyJ4Um89QIt9z/ajmAEeg==`;
- `@splinetool/runtime@0.9.526`, integrity
  `sha512-qznHbXA5aKwDbCgESAothCNm1IeEZcmNWG145p5aXj4w5uoqR1TZ9qkTHTKLTsUbHeitCwdhzmRqan1kxboLgQ==`.

Their exact NPM metadata and installed package contents contain no license
field or license file; the Spline package also omits repository and homepage
metadata. Neither package name was found by a literal scan of either frozen
frontend output, but that negative observation is not treated as proof of a
license grant or complete bundle non-reachability.

`bun install --omit=peer` removes the packages, but the product then fails to
build because the dynamically selected Lobe icon objects require
`Center`, `Flexbox`, `Icon`, `Tag` and `ProviderIcon` from Lobe UI. Therefore the
fork does not ship an unreviewed dependency omission or a visual-behavior
change merely to clear this gate.

The smallest acceptable remediation is a separately reviewed product adapter
for those five UI primitives, followed by frozen default/classic builds,
component screenshots and interaction regression tests. The alternative is an
exact licensed Lobe UI package that no longer depends on the two unresolved
packages. Until one of those paths is proved, the release workflow rejects the
HOLD.

## SBOM and distribution boundary

- The source/dependency SBOM is generated before the OCI build and its digest
  is bound into the binary and image labels.
- The final-image SBOM is generated from the exact image digest and attached as
  an OCI/GitHub attestation. Its attestation digest is recorded in the release
  receipt rather than self-referenced from the image config.
- The scratch image contains the Go binary, CA bundle, zoneinfo, passwd/group
  metadata and reviewed license files. It does not contain source trees,
  `node_modules`, tests, Git metadata, credentials or build caches.
- A public exact-commit source URI and deterministic source archive are kept
  available for every distributed image digest.
