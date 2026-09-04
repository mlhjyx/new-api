package releasepolicy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepositoryWorkflowsHaveOneFailClosedForkReleasePath(t *testing.T) {
	repo := filepath.Clean(filepath.Join("..", ".."))

	report, err := VerifyRepository(repo)

	require.NoError(t, err)
	assert.Equal(t, "ghcr.io/mlhjyx/new-api", report.ForkImageRepository)
	assert.Equal(t, "production-parity/settlement-readback-v1", report.ReleaseBranch)
	assert.Equal(t, 4, report.GuardedUpstreamWorkflows)
	assert.GreaterOrEqual(t, report.PinnedActionReferences, 1)
	assert.Equal(t, "65532:65532", report.RuntimeUser)
	assert.Equal(t, "scratch", report.RuntimeBase)
	assert.True(t, report.BoundedModuleDownload)
	assert.True(t, report.ChecksummedModuleProxy)
}

func TestProductionDockerfileIsPackageManagerFreeAndIdentityBound(t *testing.T) {
	tests := []struct {
		name        string
		oldValue    string
		newValue    string
		expectedErr string
	}{
		{name: "root user", oldValue: "USER 65532:65532", newValue: "USER 0:0", expectedErr: "non-root"},
		{name: "moving package install", oldValue: "FROM scratch AS runtime", newValue: "FROM debian:bookworm-slim AS runtime\nRUN apt-get update", expectedErr: "package-manager-free"},
		{name: "unowned binary", oldValue: "COPY --from=builder2 --chown=65532:65532 /build/new-api /new-api", newValue: "COPY --from=builder2 /build/new-api /new-api", expectedErr: "numeric ownership"},
		{name: "dynamic go binary", oldValue: "CGO_ENABLED=0", newValue: "CGO_ENABLED=1", expectedErr: "CGO-disabled"},
		{name: "missing source label", oldValue: "io.growthos.new-api.corresponding-source.uri=\"${CORRESPONDING_SOURCE_URI}\"", newValue: "io.growthos.new-api.corresponding-source.missing=\"${CORRESPONDING_SOURCE_URI}\"", expectedErr: "OCI labels"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			copyRoot := copyReleasePolicyFixture(t)
			path := filepath.Join(copyRoot, "Dockerfile")
			replaceOnce(t, path, test.oldValue, test.newValue)

			_, err := VerifyRepository(copyRoot)

			assert.ErrorContains(t, err, test.expectedErr)
		})
	}
}

func TestProductionDockerfileRetainsVerifiedDownloadsAcrossBoundedRetries(t *testing.T) {
	repo := copyReleasePolicyFixture(t)
	path := filepath.Join(repo, "Dockerfile")
	replaceOnce(t, path, "RUN --mount=type=cache,id=new-api-go-mod,target=/go/pkg/mod,sharing=locked \\\n    set -eu;", "RUN set -eu;")

	_, err := VerifyRepository(repo)

	assert.ErrorContains(t, err, "bounded module download")
}

func TestPolicyRejectsAnUnguardedUpstreamPublishJob(t *testing.T) {
	repo := copyReleasePolicyFixture(t)
	path := filepath.Join(repo, ".github/workflows/docker-build.yml")
	replaceOnce(t, path, "    if: github.repository == 'QuantumNous/new-api'\n", "")

	_, err := VerifyRepository(repo)

	assert.ErrorContains(t, err, "canonical upstream guard")
}

func TestPolicyRejectsMovingActionReferences(t *testing.T) {
	repo := copyReleasePolicyFixture(t)
	path := filepath.Join(repo, ".github/workflows/growthos-new-api-pr.yml")
	replaceOnce(t, path, "oven-sh/setup-bun@0c5077e51419868618aeaa5fe8019c62421857d6", "oven-sh/setup-bun@v2")

	_, err := VerifyRepository(repo)

	assert.ErrorContains(t, err, "40-character commit")
}

func TestPolicyRejectsForkReleaseNamespaceOrMovingTags(t *testing.T) {
	tests := []struct {
		name        string
		oldValue    string
		newValue    string
		expectedErr string
	}{
		{name: "upstream namespace", oldValue: "IMAGE_REPOSITORY: ghcr.io/mlhjyx/new-api", newValue: "IMAGE_REPOSITORY: calciumion/new-api", expectedErr: "authorized GHCR namespace"},
		{name: "latest", oldValue: "sha-${REVISION}", newValue: "latest", expectedErr: "immutable sha tag"},
		{name: "development dockerfile", oldValue: "file: ./Dockerfile", newValue: "file: ./Dockerfile.dev", expectedErr: "Dockerfile.dev"},
		{name: "dirty version file", oldValue: "VERSION_VALUE=production-parity-${REVISION}", newValue: "echo production-parity > VERSION", expectedErr: "VERSION"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := copyReleasePolicyFixture(t)
			path := filepath.Join(repo, ".github/workflows/growthos-new-api-release.yml")
			replaceOnce(t, path, test.oldValue, test.newValue)

			_, err := VerifyRepository(repo)

			assert.ErrorContains(t, err, test.expectedErr)
		})
	}
}

func TestPolicyRejectsForkPRWorkflowThatCanPush(t *testing.T) {
	repo := copyReleasePolicyFixture(t)
	path := filepath.Join(repo, ".github/workflows/growthos-new-api-pr.yml")
	replaceOnce(t, path, "push: false", "push: true")

	_, err := VerifyRepository(repo)

	assert.ErrorContains(t, err, "must not push")
}

func copyReleasePolicyFixture(t *testing.T) string {
	t.Helper()
	sourceRoot := filepath.Clean(filepath.Join("..", ".."))
	targetRoot := t.TempDir()
	for _, name := range []string{
		"Dockerfile",
		"Dockerfile.dev",
		".github/workflows/docker-build.yml",
		".github/workflows/docker-image-branch.yml",
		".github/workflows/electron-build.yml",
		".github/workflows/release.yml",
		".github/workflows/growthos-new-api-pr.yml",
		".github/workflows/growthos-new-api-release.yml",
	} {
		source := filepath.Join(sourceRoot, filepath.FromSlash(name))
		target := filepath.Join(targetRoot, filepath.FromSlash(name))
		data, err := os.ReadFile(source)
		require.NoError(t, err, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
		require.NoError(t, os.WriteFile(target, data, 0o644))
	}
	return targetRoot
}

func replaceOnce(t *testing.T, path string, oldValue string, newValue string) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)
	require.Equal(t, 1, strings.Count(content, oldValue), "fixture mutation must be exact")
	content = strings.Replace(content, oldValue, newValue, 1)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
