package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

func GetCorrespondingSource(c *gin.Context) {
	identity, managed, err := common.CurrentReleaseIdentity()
	if err != nil || !managed {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "corresponding source is unavailable",
			"code":    "CORRESPONDING_SOURCE_UNAVAILABLE",
		})
		return
	}
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.JSON(http.StatusOK, identity)
}
