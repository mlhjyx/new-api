package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCorrespondingSourceIsPublicClosedAndMatchesGlobalLink(t *testing.T) {
	revision := strings.Repeat("a", 40)
	digest := strings.Repeat("b", 64)
	restore := installControllerReleaseIdentity(t, revision, digest)
	defer restore()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.Version())
	engine.GET("/api/corresponding-source/v1", GetCorrespondingSource)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/corresponding-source/v1", nil)

	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/json; charset=utf-8", recorder.Header().Get("Content-Type"))
	assert.Equal(t, "public, max-age=31536000, immutable", recorder.Header().Get("Cache-Control"))
	assert.Equal(t, []string{"<https://github.com/mlhjyx/new-api/tree/" + revision + ">; rel=\"source\""}, recorder.Header().Values("Link"))
	var body map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	assert.ElementsMatch(t, []string{"schema_version", "project", "upstream", "fork", "build", "corresponding_source", "license", "sbom"}, mapKeys(body))
	assert.Equal(t, "new-api-corresponding-source/v1", body["schema_version"])
	assert.Equal(t, "new-api", body["project"])
	assert.NotContains(t, recorder.Body.String(), "password")
	assert.NotContains(t, recorder.Body.String(), "token")
	assert.NotContains(t, recorder.Body.String(), "/root/")
}

func TestCorrespondingSourceDoesNotClaimAnUnmanagedBuild(t *testing.T) {
	restore := snapshotControllerReleaseIdentity()
	defer restore()
	common.BuildReleaseProfile = "unmanaged"
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	GetCorrespondingSource(context)

	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.JSONEq(t, `{"success":false,"message":"corresponding source is unavailable","code":"CORRESPONDING_SOURCE_UNAVAILABLE"}`, recorder.Body.String())
}

func installControllerReleaseIdentity(t *testing.T, revision string, digest string) func() {
	t.Helper()
	restore := snapshotControllerReleaseIdentity()
	common.BuildReleaseProfile = "managed"
	common.BuildReleaseVersion = "production-parity-" + revision
	common.BuildReleaseRevision = revision
	common.BuildReleaseGitTree = strings.Repeat("c", 40)
	common.BuildUpstreamRevision = "bde9b2f44887d34ec54799ae191d50f97914359e"
	common.BuildPatchSeriesSHA256 = digest
	common.BuildPatchedTreeSHA256 = digest
	common.BuildRecipeSHA256 = digest
	common.BuildModuleGraphSHA256 = digest
	common.BuildCorrespondingSourceURI = "https://github.com/mlhjyx/new-api/tree/" + revision
	common.BuildCorrespondingSourceArchiveURI = "https://github.com/mlhjyx/new-api/releases/download/source-" + revision + "/new-api-" + revision + ".tar.gz"
	common.BuildCorrespondingSourceArchiveSHA256 = digest
	common.BuildSourceSBOMSHA256 = digest
	common.BuildLicenseSHA256 = "8486a10c4393cee1c25392769ddd3b2d6c242d6ec7928e1414efff7dfb2f07ef"
	common.BuildNoticeSHA256 = digest
	common.BuildThirdPartySHA256 = digest
	return restore
}

func snapshotControllerReleaseIdentity() func() {
	values := []string{
		common.BuildReleaseProfile,
		common.BuildReleaseVersion,
		common.BuildReleaseRevision,
		common.BuildReleaseGitTree,
		common.BuildUpstreamRevision,
		common.BuildPatchSeriesSHA256,
		common.BuildPatchedTreeSHA256,
		common.BuildRecipeSHA256,
		common.BuildModuleGraphSHA256,
		common.BuildCorrespondingSourceURI,
		common.BuildCorrespondingSourceArchiveURI,
		common.BuildCorrespondingSourceArchiveSHA256,
		common.BuildSourceSBOMSHA256,
		common.BuildLicenseSHA256,
		common.BuildNoticeSHA256,
		common.BuildThirdPartySHA256,
	}
	return func() {
		common.BuildReleaseProfile = values[0]
		common.BuildReleaseVersion = values[1]
		common.BuildReleaseRevision = values[2]
		common.BuildReleaseGitTree = values[3]
		common.BuildUpstreamRevision = values[4]
		common.BuildPatchSeriesSHA256 = values[5]
		common.BuildPatchedTreeSHA256 = values[6]
		common.BuildRecipeSHA256 = values[7]
		common.BuildModuleGraphSHA256 = values[8]
		common.BuildCorrespondingSourceURI = values[9]
		common.BuildCorrespondingSourceArchiveURI = values[10]
		common.BuildCorrespondingSourceArchiveSHA256 = values[11]
		common.BuildSourceSBOMSHA256 = values[12]
		common.BuildLicenseSHA256 = values[13]
		common.BuildNoticeSHA256 = values[14]
		common.BuildThirdPartySHA256 = values[15]
	}
}

func mapKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	return keys
}
