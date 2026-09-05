ARG NEW_API_VERSION
ARG RELEASE_REVISION
ARG RELEASE_GIT_TREE
ARG UPSTREAM_REVISION
ARG PATCH_SERIES_SHA256
ARG PATCHED_TREE_SHA256
ARG BUILD_RECIPE_SHA256
ARG MODULE_GRAPH_SHA256
ARG CORRESPONDING_SOURCE_URI
ARG CORRESPONDING_SOURCE_ARCHIVE_URI
ARG CORRESPONDING_SOURCE_ARCHIVE_SHA256
ARG SOURCE_SBOM_SHA256
ARG LICENSE_SHA256
ARG NOTICE_SHA256
ARG THIRD_PARTY_SHA256
ARG GO_MODULE_PROXY=https://proxy.golang.org,direct

FROM oven/bun:1@sha256:0733e50325078969732ebe3b15ce4c4be5082f18c4ac1a0f0ca4839c2e4e42a7 AS builder

ARG NEW_API_VERSION
WORKDIR /build/web
COPY web/package.json web/bun.lock ./
COPY web/default/package.json ./default/package.json
COPY web/classic/package.json ./classic/package.json
COPY web/shared/lobe-ui-adapter/package.json ./shared/lobe-ui-adapter/package.json
RUN bun install --frozen-lockfile
COPY web/shared/lobe-ui-adapter ./shared/lobe-ui-adapter
COPY ./web/default ./default
RUN cd default && DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION="${NEW_API_VERSION}" bun run build

FROM oven/bun:1@sha256:0733e50325078969732ebe3b15ce4c4be5082f18c4ac1a0f0ca4839c2e4e42a7 AS builder-classic

ARG NEW_API_VERSION
WORKDIR /build/web
COPY web/package.json web/bun.lock ./
COPY web/default/package.json ./default/package.json
COPY web/classic/package.json ./classic/package.json
COPY web/shared/lobe-ui-adapter/package.json ./shared/lobe-ui-adapter/package.json
RUN bun install --filter ./classic --frozen-lockfile
COPY web/shared/lobe-ui-adapter ./shared/lobe-ui-adapter
COPY ./web/classic ./classic
RUN cd classic && VITE_REACT_APP_VERSION="${NEW_API_VERSION}" bun run build

FROM golang:1.26.1-alpine@sha256:2389ebfa5b7f43eeafbd6be0c3700cc46690ef842ad962f6c5bd6be49ed82039 AS builder2

ARG NEW_API_VERSION
ARG RELEASE_REVISION
ARG RELEASE_GIT_TREE
ARG UPSTREAM_REVISION
ARG PATCH_SERIES_SHA256
ARG PATCHED_TREE_SHA256
ARG BUILD_RECIPE_SHA256
ARG MODULE_GRAPH_SHA256
ARG CORRESPONDING_SOURCE_URI
ARG CORRESPONDING_SOURCE_ARCHIVE_URI
ARG CORRESPONDING_SOURCE_ARCHIVE_SHA256
ARG SOURCE_SBOM_SHA256
ARG LICENSE_SHA256
ARG NOTICE_SHA256
ARG THIRD_PARTY_SHA256
ARG GO_MODULE_PROXY

ENV GO111MODULE=on CGO_ENABLED=0
ARG TARGETOS
ARG TARGETARCH
ENV GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64}
ENV GOEXPERIMENT=greenteagc

WORKDIR /build
COPY go.mod go.sum ./
RUN --mount=type=cache,id=new-api-go-mod,target=/go/pkg/mod,sharing=locked \
    set -eu; \
    for attempt in 1 2 3; do \
      if GOPROXY="${GO_MODULE_PROXY}" GOSUMDB=sum.golang.org go mod download; then exit 0; fi; \
      test "${attempt}" -lt 3; \
    done
COPY . .
COPY --from=builder /build/web/default/dist ./web/default/dist
COPY --from=builder-classic /build/web/classic/dist ./web/classic/dist

RUN set -eu; \
    case "${RELEASE_REVISION}" in *[!0-9a-f]*|'') exit 1 ;; esac; \
    case "${RELEASE_GIT_TREE}" in *[!0-9a-f]*|'') exit 1 ;; esac; \
    test "${#RELEASE_REVISION}" -eq 40; \
    test "${#RELEASE_GIT_TREE}" -eq 40; \
    test "${NEW_API_VERSION}" = "production-parity-${RELEASE_REVISION}"; \
    test "${UPSTREAM_REVISION}" = "bde9b2f44887d34ec54799ae191d50f97914359e"; \
    test "${CORRESPONDING_SOURCE_URI}" = "https://github.com/mlhjyx/new-api/tree/${RELEASE_REVISION}"; \
    test "${CORRESPONDING_SOURCE_ARCHIVE_URI}" = "https://github.com/mlhjyx/new-api/releases/download/source-${RELEASE_REVISION}/new-api-${RELEASE_REVISION}.tar.gz"; \
    test "${LICENSE_SHA256}" = "8486a10c4393cee1c25392769ddd3b2d6c242d6ec7928e1414efff7dfb2f07ef"; \
    for digest in "${PATCH_SERIES_SHA256}" "${PATCHED_TREE_SHA256}" "${BUILD_RECIPE_SHA256}" "${MODULE_GRAPH_SHA256}" "${CORRESPONDING_SOURCE_ARCHIVE_SHA256}" "${SOURCE_SBOM_SHA256}" "${LICENSE_SHA256}" "${NOTICE_SHA256}" "${THIRD_PARTY_SHA256}"; do \
      case "${digest}" in *[!0-9a-f]*|'') exit 1 ;; esac; \
      test "${#digest}" -eq 64; \
    done

RUN --mount=type=cache,id=new-api-go-mod,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,id=new-api-go-build,target=/root/.cache/go-build,sharing=locked \
    GOPROXY="${GO_MODULE_PROXY}" GOSUMDB=sum.golang.org go build -trimpath -mod=readonly -ldflags "-s -w \
    -X github.com/QuantumNous/new-api/common.Version=${NEW_API_VERSION} \
    -X github.com/QuantumNous/new-api/common.BuildReleaseProfile=managed \
    -X github.com/QuantumNous/new-api/common.BuildReleaseVersion=${NEW_API_VERSION} \
    -X github.com/QuantumNous/new-api/common.BuildReleaseRevision=${RELEASE_REVISION} \
    -X github.com/QuantumNous/new-api/common.BuildReleaseGitTree=${RELEASE_GIT_TREE} \
    -X github.com/QuantumNous/new-api/common.BuildUpstreamRevision=${UPSTREAM_REVISION} \
    -X github.com/QuantumNous/new-api/common.BuildPatchSeriesSHA256=${PATCH_SERIES_SHA256} \
    -X github.com/QuantumNous/new-api/common.BuildPatchedTreeSHA256=${PATCHED_TREE_SHA256} \
    -X github.com/QuantumNous/new-api/common.BuildRecipeSHA256=${BUILD_RECIPE_SHA256} \
    -X github.com/QuantumNous/new-api/common.BuildModuleGraphSHA256=${MODULE_GRAPH_SHA256} \
    -X github.com/QuantumNous/new-api/common.BuildCorrespondingSourceURI=${CORRESPONDING_SOURCE_URI} \
    -X github.com/QuantumNous/new-api/common.BuildCorrespondingSourceArchiveURI=${CORRESPONDING_SOURCE_ARCHIVE_URI} \
    -X github.com/QuantumNous/new-api/common.BuildCorrespondingSourceArchiveSHA256=${CORRESPONDING_SOURCE_ARCHIVE_SHA256} \
    -X github.com/QuantumNous/new-api/common.BuildSourceSBOMSHA256=${SOURCE_SBOM_SHA256} \
    -X github.com/QuantumNous/new-api/common.BuildLicenseSHA256=${LICENSE_SHA256} \
    -X github.com/QuantumNous/new-api/common.BuildNoticeSHA256=${NOTICE_SHA256} \
    -X github.com/QuantumNous/new-api/common.BuildThirdPartySHA256=${THIRD_PARTY_SHA256}" -o /build/new-api

RUN set -eu; \
    test "${CGO_ENABLED}" = "0"; \
    mkdir -p /runtime/data /runtime/licenses /runtime/tmp /runtime/etc; \
    printf 'new-api:x:65532:65532:New API runtime:/data:/sbin/nologin\n' > /runtime/etc/passwd; \
    printf 'new-api:x:65532:\n' > /runtime/etc/group; \
    cp LICENSE NOTICE THIRD-PARTY-LICENSES.md release/license-review.json release/license-review.md /runtime/licenses/; \
    chown -R 65532:65532 /runtime/data /runtime/licenses /runtime/tmp /runtime/etc

FROM scratch AS runtime

ARG NEW_API_VERSION
ARG RELEASE_REVISION
ARG RELEASE_GIT_TREE
ARG UPSTREAM_REVISION
ARG PATCH_SERIES_SHA256
ARG PATCHED_TREE_SHA256
ARG BUILD_RECIPE_SHA256
ARG MODULE_GRAPH_SHA256
ARG CORRESPONDING_SOURCE_URI
ARG CORRESPONDING_SOURCE_ARCHIVE_URI
ARG CORRESPONDING_SOURCE_ARCHIVE_SHA256
ARG SOURCE_SBOM_SHA256
ARG LICENSE_SHA256
ARG NOTICE_SHA256
ARG THIRD_PARTY_SHA256

LABEL org.opencontainers.image.title="new-api" \
      org.opencontainers.image.description="New API settlement-readback gateway fork; upstream identity retained" \
      org.opencontainers.image.authors="QuantumNous and contributors; mlhjyx fork contributors" \
      org.opencontainers.image.url="https://github.com/mlhjyx/new-api" \
      org.opencontainers.image.source="https://github.com/mlhjyx/new-api" \
      org.opencontainers.image.revision="${RELEASE_REVISION}" \
      org.opencontainers.image.version="${NEW_API_VERSION}" \
      org.opencontainers.image.licenses="AGPL-3.0-only" \
      io.growthos.new-api.upstream.source="https://github.com/QuantumNous/new-api" \
      io.growthos.new-api.upstream.revision="${UPSTREAM_REVISION}" \
      io.growthos.new-api.upstream.git-tree="8d25730d7f58a83778ef23b3a8ccd255d2d91701" \
      io.growthos.new-api.upstream.tree-sha256="07923f60654b9eadda476ea4f269524273b0edeb5287b50c602c4e6921afb6d4" \
      io.growthos.new-api.upstream.source-archive-sha256="3f532d1b4f48153277342e98c54b06665b0c472118b0f032ddcc70233c288331" \
      io.growthos.new-api.fork.git-tree="${RELEASE_GIT_TREE}" \
      io.growthos.new-api.patch-series-sha256="${PATCH_SERIES_SHA256}" \
      io.growthos.new-api.patched-tree-sha256="${PATCHED_TREE_SHA256}" \
      io.growthos.new-api.build-recipe-sha256="${BUILD_RECIPE_SHA256}" \
      io.growthos.new-api.module-graph-sha256="${MODULE_GRAPH_SHA256}" \
      io.growthos.new-api.corresponding-source.uri="${CORRESPONDING_SOURCE_URI}" \
      io.growthos.new-api.corresponding-source.archive-uri="${CORRESPONDING_SOURCE_ARCHIVE_URI}" \
      io.growthos.new-api.corresponding-source.sha256="${CORRESPONDING_SOURCE_ARCHIVE_SHA256}" \
      io.growthos.new-api.source-sbom.sha256="${SOURCE_SBOM_SHA256}" \
      io.growthos.new-api.license.sha256="${LICENSE_SHA256}" \
      io.growthos.new-api.notice.sha256="${NOTICE_SHA256}" \
      io.growthos.new-api.third-party-licenses.sha256="${THIRD_PARTY_SHA256}"

COPY --from=builder2 /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder2 --chown=65532:65532 /runtime/etc/passwd /etc/passwd
COPY --from=builder2 --chown=65532:65532 /runtime/etc/group /etc/group
COPY --from=builder2 --chown=65532:65532 /runtime/data /data
COPY --from=builder2 --chown=65532:65532 /runtime/tmp /tmp
COPY --from=builder2 --chown=65532:65532 /runtime/licenses /licenses
COPY --from=builder2 --chown=65532:65532 /build/new-api /new-api

ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
EXPOSE 3000
WORKDIR /data
USER 65532:65532
ENTRYPOINT ["/new-api"]
