package common

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagedReleaseIdentityIsCompleteAndBuildTimeOnly(t *testing.T) {
	revision := strings.Repeat("a", 40)
	digest := strings.Repeat("b", 64)
	restore := installReleaseBuildFixture(t, revision, digest)
	defer restore()
	t.Setenv("RELEASE_REVISION", strings.Repeat("c", 40))
	t.Setenv("CORRESPONDING_SOURCE_URI", "https://attacker.invalid/source")

	identity, managed, err := CurrentReleaseIdentity()
	require.NoError(t, err)
	assert.True(t, managed)
	assert.Equal(t, revision, identity.Fork.Revision)
	assert.Equal(t, "https://github.com/mlhjyx/new-api/tree/"+revision, identity.CorrespondingSource.URI)
	assert.Equal(t, "<https://github.com/mlhjyx/new-api/tree/"+revision+">; rel=\"source\"", identity.LinkHeader())
	assert.Equal(t, "AGPL-3.0-only", identity.License.SPDX)
}

func TestManagedReleaseIdentityFailsClosedWhenAnyBuildFieldIsMissingOrUnsafe(t *testing.T) {
	revision := strings.Repeat("a", 40)
	digest := strings.Repeat("b", 64)
	restore := installReleaseBuildFixture(t, revision, digest)
	defer restore()

	BuildCorrespondingSourceURI = "https://github.com/mlhjyx/new-api/tree/" + revision + "\r\nX-Evil: 1"
	_, managed, err := CurrentReleaseIdentity()
	assert.True(t, managed)
	assert.ErrorContains(t, err, "source URI")

	BuildCorrespondingSourceURI = "https://github.com/mlhjyx/new-api/tree/" + revision
	BuildModuleGraphSHA256 = ""
	_, managed, err = CurrentReleaseIdentity()
	assert.True(t, managed)
	assert.ErrorContains(t, err, "digest")
}

func TestUnmanagedDeveloperBinaryDoesNotClaimReleaseIdentity(t *testing.T) {
	restore := snapshotReleaseBuildVariables()
	defer restore()
	BuildReleaseProfile = "unmanaged"
	BuildReleaseRevision = strings.Repeat("a", 40)

	identity, managed, err := CurrentReleaseIdentity()

	require.NoError(t, err)
	assert.False(t, managed)
	assert.Equal(t, ReleaseIdentity{}, identity)
}

func installReleaseBuildFixture(t *testing.T, revision string, digest string) func() {
	t.Helper()
	restore := snapshotReleaseBuildVariables()
	BuildReleaseProfile = "managed"
	BuildReleaseVersion = "production-parity-" + revision
	BuildReleaseRevision = revision
	BuildReleaseGitTree = strings.Repeat("c", 40)
	BuildUpstreamRevision = "bde9b2f44887d34ec54799ae191d50f97914359e"
	BuildPatchSeriesSHA256 = digest
	BuildPatchedTreeSHA256 = digest
	BuildRecipeSHA256 = digest
	BuildModuleGraphSHA256 = digest
	BuildCorrespondingSourceURI = "https://github.com/mlhjyx/new-api/tree/" + revision
	BuildCorrespondingSourceArchiveURI = "https://github.com/mlhjyx/new-api/releases/download/source-" + revision + "/new-api-" + revision + ".tar.gz"
	BuildCorrespondingSourceArchiveSHA256 = digest
	BuildSourceSBOMSHA256 = digest
	BuildLicenseSHA256 = "8486a10c4393cee1c25392769ddd3b2d6c242d6ec7928e1414efff7dfb2f07ef"
	BuildNoticeSHA256 = digest
	BuildThirdPartySHA256 = digest
	return restore
}

func snapshotReleaseBuildVariables() func() {
	values := []string{
		BuildReleaseProfile,
		BuildReleaseVersion,
		BuildReleaseRevision,
		BuildReleaseGitTree,
		BuildUpstreamRevision,
		BuildPatchSeriesSHA256,
		BuildPatchedTreeSHA256,
		BuildRecipeSHA256,
		BuildModuleGraphSHA256,
		BuildCorrespondingSourceURI,
		BuildCorrespondingSourceArchiveURI,
		BuildCorrespondingSourceArchiveSHA256,
		BuildSourceSBOMSHA256,
		BuildLicenseSHA256,
		BuildNoticeSHA256,
		BuildThirdPartySHA256,
	}
	return func() {
		BuildReleaseProfile = values[0]
		BuildReleaseVersion = values[1]
		BuildReleaseRevision = values[2]
		BuildReleaseGitTree = values[3]
		BuildUpstreamRevision = values[4]
		BuildPatchSeriesSHA256 = values[5]
		BuildPatchedTreeSHA256 = values[6]
		BuildRecipeSHA256 = values[7]
		BuildModuleGraphSHA256 = values[8]
		BuildCorrespondingSourceURI = values[9]
		BuildCorrespondingSourceArchiveURI = values[10]
		BuildCorrespondingSourceArchiveSHA256 = values[11]
		BuildSourceSBOMSHA256 = values[12]
		BuildLicenseSHA256 = values[13]
		BuildNoticeSHA256 = values[14]
		BuildThirdPartySHA256 = values[15]
	}
}
