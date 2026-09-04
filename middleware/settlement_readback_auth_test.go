package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestSettlementReadbackAuthRejectsNonReaderAuthorizationBeforeLookup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, authorization := range []string{"", "Bearer", "Basic reader", "Bearer  reader"} {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(http.MethodGet, "/api/settlement-readback/v1/capability", nil)
		context.Request.Header.Set("Authorization", authorization)
		SettlementReadbackAuth()(context)
		assert.True(t, context.IsAborted(), authorization)
		assert.Equal(t, http.StatusUnauthorized, recorder.Code, authorization)
		assert.Zero(t, context.GetInt(SettlementReadbackReaderIDContextKey), authorization)
		assert.Zero(t, context.GetInt(SettlementReadbackDispatchTokenContextKey), authorization)
	}
}
