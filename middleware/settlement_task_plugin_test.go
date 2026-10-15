package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPinTaskPluginEndpointDeniesSettlementBoundRequestBeforePluginCode(t *testing.T) {
	const key = "endpoint-settlement-bound-test"
	_, err := jsplugin.DefaultRegistry.Register(taskProtocolPluginSource(
		key,
		"1.0.0",
		`["claimed-model"]`,
		"/v1/responses",
		`return {model: "claimed-model"};`,
	), jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, jsplugin.DefaultRegistry.Unregister(key)) })

	for _, testCase := range []struct {
		name      string
		bindingID int
		model     string
		wantCode  int
		wantNext  bool
	}{
		{name: "bound request on a plugin-owned model", bindingID: 9, model: "claimed-model", wantCode: http.StatusServiceUnavailable},
		{name: "bound request on an ordinary model", bindingID: 9, model: "gpt-4o-mini", wantCode: http.StatusNoContent, wantNext: true},
		{name: "unbound request on a plugin-owned model", model: "claimed-model", wantCode: http.StatusNoContent, wantNext: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			router := gin.New()
			reachedNext := false
			router.POST(
				"/v1/responses",
				func(c *gin.Context) {
					if testCase.bindingID > 0 {
						c.Set(SettlementReadbackBindingIDContextKey, testCase.bindingID)
					}
					c.Next()
				},
				PinTaskPluginEndpoint(),
				func(c *gin.Context) {
					reachedNext = true
					c.Status(http.StatusNoContent)
				},
			)
			request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"`+testCase.model+`"}`))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, request)

			assert.Equal(t, testCase.wantCode, recorder.Code)
			assert.Equal(t, testCase.wantNext, reachedNext)
			if !testCase.wantNext {
				assert.Contains(t, recorder.Body.String(), string(types.ErrorCodeSettlementDispatchFenceUnavailable))
			}
		})
	}
}
