package controller

import (
	"errors"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

const settlementReadbackContract = "new-api-settlement-readback/v1"
const settlementReadbackPepperKeyringFileEnv = "SETTLEMENT_READBACK_CREDENTIAL_PEPPER_KEYRING_FILE"
const settlementReadbackAdminBodyMaxBytes = 1024

var settlementReadbackAdminIntegerBodies = map[string]*regexp.Regexp{
	"dispatch_token_id": regexp.MustCompile(`^\{[ \t\r\n]*"dispatch_token_id"[ \t\r\n]*:[ \t\r\n]*([1-9][0-9]{0,9})[ \t\r\n]*\}$`),
	"overlap_seconds":   regexp.MustCompile(`^\{[ \t\r\n]*"overlap_seconds"[ \t\r\n]*:[ \t\r\n]*([1-9][0-9]{0,9})[ \t\r\n]*\}$`),
}
var settlementReadbackModelIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`)
var settlementReadbackCanonicalNonnegativeInteger = regexp.MustCompile(`^(?:0|[1-9][0-9]*)$`)

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

type settlementReadbackReceipt struct {
	RequestID           string `json:"request_id"`
	Type                string `json:"type"`
	ModelName           string `json:"model_name"`
	ChannelID           int    `json:"channel_id"`
	Quota               string `json:"quota"`
	PromptTokens        int64  `json:"prompt_tokens"`
	CompletionTokens    int64  `json:"completion_tokens"`
	UsageSemantic       string `json:"usage_semantic"`
	CacheCreationTokens int64  `json:"cache_creation_tokens"`
	CacheReadTokens     int64  `json:"cache_read_tokens"`
	UpstreamIDState     string `json:"upstream_id_state"`
}

func settlementReadbackAdminInteger(c *gin.Context, key string) (int64, bool) {
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, settlementReadbackAdminBodyMaxBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > settlementReadbackAdminBodyMaxBytes {
		return 0, false
	}
	pattern := settlementReadbackAdminIntegerBodies[key]
	if pattern == nil {
		return 0, false
	}
	matched := pattern.FindSubmatch(raw)
	if len(matched) != 2 {
		return 0, false
	}
	value, err := strconv.ParseInt(string(matched[1]), 10, 32)
	return value, err == nil && value > 0
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
	if raw, readError := io.ReadAll(io.LimitReader(c.Request.Body, 1)); readError != nil || len(raw) != 0 {
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
	writeSettlementReadbackHeaders(c)
	if !model.SettlementReadbackRelationalLogTopologyReady() {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	requestValues, present := c.Request.URL.Query()["request_id"]
	nonceValues := c.Request.Header.Values(common.SettlementReadbackNonceHeader)
	if !present || len(requestValues) != 1 || len(nonceValues) != 1 {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	requestDigest, requestOK := model.SettlementReadbackOpaqueDigest(requestValues[0])
	nonceDigest, nonceOK := model.SettlementReadbackOpaqueDigest(nonceValues[0])
	if !requestOK || !nonceOK {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	dispatchTokenID := c.GetInt(middleware.SettlementReadbackDispatchTokenContextKey)
	log, pending, err := model.FindSettlementReadbackConsumeLog(
		model.DB,
		dispatchTokenID,
		requestDigest,
		nonceDigest,
	)
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			c.AbortWithStatus(http.StatusNotFound)
		case model.IsSettlementReadbackIntegrityError(err):
			c.AbortWithStatus(http.StatusConflict)
		default:
			c.AbortWithStatus(http.StatusServiceUnavailable)
		}
		return
	}
	if pending {
		c.JSON(http.StatusOK, gin.H{"data": []settlementReadbackReceipt{}})
		return
	}
	receipt, ok := settlementReadbackClosedReceipt(requestValues[0], log)
	if !ok {
		c.AbortWithStatus(http.StatusConflict)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": []settlementReadbackReceipt{receipt}})
}

func settlementReadbackNonnegativeInteger(raw string) (int64, bool) {
	if !settlementReadbackCanonicalNonnegativeInteger.MatchString(raw) {
		return 0, false
	}
	number, err := strconv.ParseInt(raw, 10, 64)
	return number, err == nil && number >= 0 && strconv.FormatInt(number, 10) == raw
}

func settlementReadbackClosedReceipt(requestID string, log *model.Log) (settlementReadbackReceipt, bool) {
	if log == nil || log.Type != model.LogTypeConsume || log.ChannelId < 1 || log.Quota < 0 || log.PromptTokens < 0 || log.CompletionTokens < 0 || len(log.ModelName) < 1 || len(log.ModelName) > 191 || !settlementReadbackModelIdentifier.MatchString(log.ModelName) {
		return settlementReadbackReceipt{}, false
	}
	if !gjson.Valid(log.Other) {
		return settlementReadbackReceipt{}, false
	}
	parsed := gjson.Parse(log.Other)
	if !parsed.IsObject() {
		return settlementReadbackReceipt{}, false
	}
	values := map[string]gjson.Result{}
	validFields := true
	parsed.ForEach(func(key, value gjson.Result) bool {
		name := key.String()
		if name == "usage_semantic" || name == "cache_creation_tokens" || name == "cache_tokens" {
			if _, duplicate := values[name]; duplicate {
				validFields = false
				return false
			}
			values[name] = value
		}
		return true
	})
	if !validFields || len(values) != 3 || values["usage_semantic"].Type != gjson.String {
		return settlementReadbackReceipt{}, false
	}
	usageSemantic := values["usage_semantic"].String()
	if usageSemantic != "openai" && usageSemantic != "anthropic" {
		return settlementReadbackReceipt{}, false
	}
	cacheCreation, creationOK := settlementReadbackNonnegativeInteger(values["cache_creation_tokens"].Raw)
	cacheRead, readOK := settlementReadbackNonnegativeInteger(values["cache_tokens"].Raw)
	if !creationOK || !readOK {
		return settlementReadbackReceipt{}, false
	}
	upstreamState := "absent"
	if log.UpstreamRequestId != "" {
		upstreamState = "observed"
	}
	return settlementReadbackReceipt{
		RequestID:           requestID,
		Type:                "consume",
		ModelName:           log.ModelName,
		ChannelID:           log.ChannelId,
		Quota:               strconv.FormatInt(int64(log.Quota), 10),
		PromptTokens:        int64(log.PromptTokens),
		CompletionTokens:    int64(log.CompletionTokens),
		UsageSemantic:       usageSemantic,
		CacheCreationTokens: cacheCreation,
		CacheReadTokens:     cacheRead,
		UpstreamIDState:     upstreamState,
	}, true
}
