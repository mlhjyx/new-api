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
