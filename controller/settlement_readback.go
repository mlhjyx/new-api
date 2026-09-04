package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const settlementReadbackContract = "new-api-settlement-readback/v1"

type settlementReadbackCapabilityResponse struct {
	SchemaVersion string `json:"schema_version"`
	Status        string `json:"status"`
}

func writeSettlementReadbackHeaders(c *gin.Context) {
	c.Header("X-New-API-Settlement-Contract", settlementReadbackContract)
	c.Header("Content-Type", "application/json")
	c.Header("Cache-Control", "no-store")
}

func GetSettlementReadbackCapability(c *gin.Context) {
	writeSettlementReadbackHeaders(c)
	if !model.SettlementReadbackRelationalLogTopologyReady() {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	c.JSON(http.StatusOK, settlementReadbackCapabilityResponse{
		SchemaVersion: "new-api-settlement-readback-capability/v1",
		Status:        "ready",
	})
}

func GetSettlementReadback(c *gin.Context) {
	// The exact receipt route is intentionally unavailable until Phase B has a
	// transactional binding-to-consume-log relation. It must never fall back to
	// the token-wide legacy log endpoint.
	writeSettlementReadbackHeaders(c)
	c.AbortWithStatus(http.StatusServiceUnavailable)
}
