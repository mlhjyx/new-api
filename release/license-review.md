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
  `633b4c5b190dc1af99a2814391354d86948e7943`.
- The original `NOTICE` bytes remain an exact prefix; the fork modification
  statement is append-only.
- Direct dependency versions are derived from `go.mod`, `web/bun.lock` and
  `electron/package-lock.json`, and are checked against
  `THIRD-PARTY-LICENSES.md` by `cmd/release-license`.

## Resolved runtime dependency

`github.com/Calcium-Ion/go-epay@v0.0.4` was linked into the payment and
subscription controllers but its exact module archive and tag contained no
license. The fork first pinned the exact licensed commit
`774330a939012a2baab5776e890456d5f15d586e`
(`v0.0.5-0.20260612155053-774330a93901`). Upstream v1.0.0-rc.44 now requires
the tag `v0.0.5` (commit `b6694eb473b09a94decd8ee71f9946156fda5e17`, module
sum `h1:TgIx44c/bhdFqso4kbmDzZFzgyDJLma5hMDNA6sOXbo=`), whose MIT license is
byte-identical: SHA-256
`86f028deb5895d8994571a0393face710851c29bb938e9b23fe7a9828efd99a8`.
`cmd/release-license` re-derives that digest from the module cache.

## Removed automatic peer graph

`@lobehub/icons` imports UI primitives from its peer `@lobehub/ui`. In the
upstream v1.0.0-rc.44 lock (`@lobehub/icons@5.22.0`, `@lobehub/ui@5.22.3`)
that peer's dependency graph still installs both:

- `@giscus/react@3.1.0`, integrity
  `sha512-0TCO2TvL43+oOdyVVGHDItwxD1UMKP2ZYpT6gXmhFOqfAJtZxTzJ9hkn34iAF/b6YzyJ4Um89QIt9z/ajmAEeg==`;
- `@splinetool/runtime@1.12.98`, integrity
  `sha512-UWZH/+4XtNgMMgmDiWhZS55WOwUDGuTj4uH6fiZV6Jol3d3v3z1RRBObspx8GMio0HajOYGg7FJ8TZdyBdpW4A==`.

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

Upstream v1.0.0-rc.44 replaced the `web/default` and `web/classic` frontends
with a single `web/` app, which now selects the workspace package directly
(`"workspaces": ["shared/lobe-ui-adapter"]`). The Bun 1.4.0 lock contains no
`@giscus/react`, `@splinetool/runtime`, or external `@lobehub/ui` node; the
only lock delta against upstream is the removal of that external tree. A fresh
frozen install proves neither unresolved package is materialized, the frozen
build passes, and the bundle contains the adapter marker but neither
unresolved package identifier. The compatibility
components have server-rendered behavior tests for layout, SVG prop forwarding,
tag composition, and accessible provider fallback markup.

## Upstream v1.0.0-rc.44 re-binding

Status of this re-binding: prepared by an AI coding agent (Claude Code) on
2026-10-14; a named human must confirm it before any release dispatch.

- The direct dependency inventory was regenerated from `go.mod`,
  `web/package.json` + `web/bun.lock` and `electron/package-lock.json`
  (73 Go, 84 web, 3 Electron rows). npm licenses come from the installed
  exact package manifests, Go licenses from the module license files.
- New expressions: `(BSD-3-Clause OR GPL-2.0)` for `node-forge` (the
  distribution elects BSD-3-Clause, already reviewed) and `MPL-2.0` for
  `github.com/go-sql-driver/mysql` (distributed unmodified; already in the
  reviewed set). `github.com/klauspost/compress` is recorded as BSD-3-Clause:
  the linked packages (zstd, fse, huff0, flate, gzip, internal/snapref) are
  all under its BSD sections; the Apache-2.0 `gzhttp` and MIT `readahead`
  parts are not linked.
- `relaykit` and `tokenkit` are in-repository modules of this project and are
  recorded as AGPL-3.0-only.

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
