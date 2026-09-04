package controller

import (
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const settlementReadbackContract = "new-api-settlement-readback/v1"
const settlementReadbackPepperKeyringFileEnv = "SETTLEMENT_READBACK_CREDENTIAL_PEPPER_KEYRING_FILE"
const settlementReadbackAdminBodyMaxBytes = 1024

type settlementReadbackCapabilityResponse struct {
	SchemaVersion string `json:"schema_version"`
	Status        string `json:"status"`
}

type settlementReadbackCredentialDelivery struct {
	SchemaVersion   string `json:"schema_version"`
	CredentialID    int    `json:"credential_id"`
	Credential      string `json:"credential"`
	LookupKeyID     string `json:"lookup_key_id"`
	PepperVersion   string `json:"pepper_version"`
	DispatchTokenID int    `json:"dispatch_token_id"`
	RotationEndsAt  int64  `json:"rotation_ends_at"`
}

func settlementReadbackAdminInteger(c *gin.Context, key string) (int64, bool) {
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, settlementReadbackAdminBodyMaxBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > settlementReadbackAdminBodyMaxBytes {
		return 0, false
	}
	var body map[string]interface{}
	if err := common.Unmarshal(raw, &body); err != nil || len(body) != 1 {
		return 0, false
	}
	value, ok := body[key].(float64)
	if !ok || value < 1 || value > math.MaxInt32 || value != math.Trunc(value) {
		return 0, false
	}
	return int64(value), true
}

func settlementReadbackAdminKeyring() (*model.SettlementReadbackPepperKeyring, error) {
	return model.LoadSettlementReadbackPepperKeyring(os.Getenv(settlementReadbackPepperKeyringFileEnv))
}

func writeSettlementReadbackCredential(c *gin.Context, status int, credential *model.SettlementReadbackCredential, secret string) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, gin.H{"data": settlementReadbackCredentialDelivery{
		SchemaVersion:   "new-api-settlement-readback-credential-delivery/v1",
		CredentialID:    credential.Id,
		Credential:      secret,
		LookupKeyID:     credential.LookupPrefix,
		PepperVersion:   credential.PepperVersion,
		DispatchTokenID: credential.DispatchTokenId,
		RotationEndsAt:  credential.RotationEndsAt,
	}})
}

func settlementReadbackLifecycleFailure(c *gin.Context, err error) {
	if model.IsSettlementReadbackCredentialInvalid(err) {
		c.AbortWithStatus(http.StatusConflict)
		return
	}
	c.AbortWithStatus(http.StatusServiceUnavailable)
}

func CreateSettlementReadbackCredential(c *gin.Context) {
	dispatchTokenID, ok := settlementReadbackAdminInteger(c, "dispatch_token_id")
	if !ok {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	keyring, err := settlementReadbackAdminKeyring()
	if err != nil {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	credential, secret, err := model.CreateSettlementReadbackCredential(model.DB, int(dispatchTokenID), keyring, time.Now().Unix())
	if err != nil {
		settlementReadbackLifecycleFailure(c, err)
		return
	}
	writeSettlementReadbackCredential(c, http.StatusCreated, credential, secret)
}

func RotateSettlementReadbackCredential(c *gin.Context) {
	credentialID, err := strconv.Atoi(c.Param("id"))
	if err != nil || credentialID < 1 || strconv.Itoa(credentialID) != c.Param("id") {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	overlap, ok := settlementReadbackAdminInteger(c, "overlap_seconds")
	if !ok {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	keyring, err := settlementReadbackAdminKeyring()
	if err != nil {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	credential, secret, err := model.RotateSettlementReadbackCredential(model.DB, credentialID, overlap, keyring, time.Now().Unix())
	if err != nil {
		settlementReadbackLifecycleFailure(c, err)
		return
	}
	writeSettlementReadbackCredential(c, http.StatusCreated, credential, secret)
}

func RevokeSettlementReadbackCredential(c *gin.Context) {
	credentialID, err := strconv.Atoi(c.Param("id"))
	if err != nil || credentialID < 1 || strconv.Itoa(credentialID) != c.Param("id") {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	replay, err := model.RevokeSettlementReadbackCredential(model.DB, credentialID, time.Now().Unix())
	if err != nil {
		settlementReadbackLifecycleFailure(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"schema_version": "new-api-settlement-readback-credential-revocation/v1",
		"replay":         replay,
	}})
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
