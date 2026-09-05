# New API fork license and distribution review

Status: **ENGINEERING APPROVED**

This record is engineering evidence for the planned
`ghcr.io/mlhjyx/new-api` OCI distribution and its public corresponding source.
It is not legal advice and does not by itself authorize publication. The
machine-readable review is `APPROVED` only because every dependency previously
holding this engineering gate has either been replaced by verified licensed
bytes or removed from the frozen build graph.

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

## Removed automatic peer graph

`@lobehub/icons@5.10.1` imports UI primitives from its peer
`@lobehub/ui@5.15.6`. That peer's dependency graph installs both:

- `@giscus/react@3.1.0`, integrity
  `sha512-0TCO2TvL43+oOdyVVGHDItwxD1UMKP2ZYpT6gXmhFOqfAJtZxTzJ9hkn34iAF/b6YzyJ4Um89QIt9z/ajmAEeg==`;
- `@splinetool/runtime@0.9.526`, integrity
  `sha512-qznHbXA5aKwDbCgESAothCNm1IeEZcmNWG145p5aXj4w5uoqR1TZ9qkTHTKLTsUbHeitCwdhzmRqan1kxboLgQ==`.

Their exact NPM metadata and installed package contents contain no license
field or license file; the Spline package also omits repository and homepage
metadata. That absence was not interpreted as a license grant.

An upgrade was rejected because both the current `@lobehub/icons@5.16.0` and
current `@lobehub/ui@5.36.2` retain the same mandatory peer/dependency shape.
`bun install --omit=peer` alone was also rejected: it changes the installed
tree without removing the unresolved packages from the lock and leaves the
icons import contract unsatisfied.

The fork now supplies a bounded local workspace adapter for exactly `Center`,
`Flexbox`, `Icon`, `Tag` and `ProviderIcon`. It is original fork code under
AGPL-3.0-only, marked `private: true`, and must not be published as a LobeHub
package. Its three product files are bound by artifact SHA-256
`beba0d5f7daa8ae401d665603edfa67db08ea95f999ac856d619375bb012ef61`.

Both frontends directly select that workspace package. The regenerated Bun
lock contains no `@giscus/react`, `@splinetool/runtime`, or external
`@lobehub/ui` node. Fresh frozen installs prove neither unresolved package is
materialized, both frozen builds pass, and the resulting bundles contain the
adapter marker but neither unresolved package identifier. The compatibility
components have server-rendered behavior tests for layout, SVG prop forwarding,
tag composition, and accessible provider fallback markup.

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
