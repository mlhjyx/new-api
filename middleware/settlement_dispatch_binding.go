package middleware

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const SettlementReadbackBindingIDContextKey = model.SettlementReadbackBindingContextKey

var settlementDispatchPaths = map[string]struct{}{
	"/v1/chat/completions": {},
	"/v1/responses":        {},
	"/v1/messages":         {},
}

func settlementHeadersPresent(c *gin.Context) bool {
	return len(c.Request.Header.Values(common.SettlementReadbackRequestIdHeader)) > 0 || len(c.Request.Header.Values(common.SettlementReadbackNonceHeader)) > 0
}

func settlementReadOnlyModelPath(c *gin.Context) bool {
	if c.Request.Method != http.MethodGet {
		return false
	}
	switch c.Request.URL.Path {
	case "/v1/models", "/v1beta/models", "/v1beta/openai/models":
		return true
	default:
		return false
	}
}

func enforceSettlementDispatchBinding(c *gin.Context) bool {
	tokenID := c.GetInt("token_id")
	bound, err := model.HasActiveSettlementReadbackCredential(tokenID)
	if err != nil {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return false
	}
	if !bound {
		if settlementHeadersPresent(c) {
			c.AbortWithStatus(http.StatusBadRequest)
			return false
		}
		return true
	}
	if settlementReadOnlyModelPath(c) && !settlementHeadersPresent(c) {
		return true
	}
	if c.Request.Method != http.MethodPost {
		c.AbortWithStatus(http.StatusForbidden)
		return false
	}
	if _, supported := settlementDispatchPaths[c.Request.URL.Path]; !supported {
		c.AbortWithStatus(http.StatusForbidden)
		return false
	}
	if !model.SettlementReadbackRelationalLogTopologyReady() {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return false
	}
	requestValues := c.Request.Header.Values(common.SettlementReadbackRequestIdHeader)
	nonceValues := c.Request.Header.Values(common.SettlementReadbackNonceHeader)
	if len(requestValues) != 1 || len(nonceValues) != 1 {
		c.AbortWithStatus(http.StatusBadRequest)
		return false
	}
	requestDigest, requestOK := model.SettlementReadbackOpaqueDigest(requestValues[0])
	nonceDigest, nonceOK := model.SettlementReadbackOpaqueDigest(nonceValues[0])
	if !requestOK || !nonceOK {
		c.AbortWithStatus(http.StatusBadRequest)
		return false
	}
	binding, _, err := model.CreateOrGetSettlementReadbackBinding(
		model.DB,
		tokenID,
		requestDigest,
		nonceDigest,
		c.GetString(common.RequestIdKey),
	)
	if err != nil {
		if model.IsSettlementReadbackCredentialInvalid(err) {
			c.AbortWithStatus(http.StatusConflict)
		} else {
			c.AbortWithStatus(http.StatusServiceUnavailable)
		}
		return false
	}
	c.Set(SettlementReadbackBindingIDContextKey, binding.Id)
	c.Request.Header.Del(common.SettlementReadbackRequestIdHeader)
	c.Request.Header.Del(common.SettlementReadbackNonceHeader)
	return true
}

func SettlementDispatchBinding() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !enforceSettlementDispatchBinding(c) {
			return
		}
		c.Next()
	}
}
