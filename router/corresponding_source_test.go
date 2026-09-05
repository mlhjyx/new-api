package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestCorrespondingSourceRouteRequiresNoAuthenticationOrDatabase(t *testing.T) {
	revision := strings.Repeat("a", 40)
	digest := strings.Repeat("b", 64)
	values := []string{common.BuildReleaseProfile, common.BuildReleaseVersion, common.BuildReleaseRevision, common.BuildReleaseGitTree, common.BuildUpstreamRevision, common.BuildPatchSeriesSHA256, common.BuildPatchedTreeSHA256, common.BuildRecipeSHA256, common.BuildModuleGraphSHA256, common.BuildCorrespondingSourceURI, common.BuildCorrespondingSourceArchiveURI, common.BuildCorrespondingSourceArchiveSHA256, common.BuildSourceSBOMSHA256, common.BuildLicenseSHA256, common.BuildNoticeSHA256, common.BuildThirdPartySHA256}
	t.Cleanup(func() {
		common.BuildReleaseProfile, common.BuildReleaseVersion, common.BuildReleaseRevision, common.BuildReleaseGitTree = values[0], values[1], values[2], values[3]
		common.BuildUpstreamRevision, common.BuildPatchSeriesSHA256, common.BuildPatchedTreeSHA256 = values[4], values[5], values[6]
		common.BuildRecipeSHA256, common.BuildModuleGraphSHA256 = values[7], values[8]
		common.BuildCorrespondingSourceURI, common.BuildCorrespondingSourceArchiveURI, common.BuildCorrespondingSourceArchiveSHA256 = values[9], values[10], values[11]
		common.BuildSourceSBOMSHA256, common.BuildLicenseSHA256, common.BuildNoticeSHA256, common.BuildThirdPartySHA256 = values[12], values[13], values[14], values[15]
	})
	common.BuildReleaseProfile = "managed"
	common.BuildReleaseVersion = "production-parity-" + revision
	common.BuildReleaseRevision = revision
	common.BuildReleaseGitTree = strings.Repeat("c", 40)
	common.BuildUpstreamRevision = "bde9b2f44887d34ec54799ae191d50f97914359e"
	common.BuildPatchSeriesSHA256, common.BuildPatchedTreeSHA256 = digest, digest
	common.BuildRecipeSHA256, common.BuildModuleGraphSHA256 = digest, digest
	common.BuildCorrespondingSourceURI = "https://github.com/mlhjyx/new-api/tree/" + revision
	common.BuildCorrespondingSourceArchiveURI = "https://github.com/mlhjyx/new-api/releases/download/source-" + revision + "/new-api-" + revision + ".tar.gz"
	common.BuildCorrespondingSourceArchiveSHA256, common.BuildSourceSBOMSHA256 = digest, digest
	common.BuildLicenseSHA256 = "8486a10c4393cee1c25392769ddd3b2d6c242d6ec7928e1414efff7dfb2f07ef"
	common.BuildNoticeSHA256, common.BuildThirdPartySHA256 = digest, digest
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)
	recorder := httptest.NewRecorder()

	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/corresponding-source/v1", nil))

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.NotContains(t, recorder.Header().Get("WWW-Authenticate"), "Bearer")
}
