package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestSettlementReadbackCapabilityFailsClosedWithoutTransactionalTopology(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/settlement-readback/v1/capability", nil)
	GetSettlementReadbackCapability(context)
	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.Equal(t, settlementReadbackContract, recorder.Header().Get("X-New-API-Settlement-Contract"))
	assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
}

func TestSettlementReadbackCannotFallbackToBroadLogsBeforeBindingContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/settlement-readback/v1", nil)
	GetSettlementReadback(context)
	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.Empty(t, recorder.Body.String())
}
