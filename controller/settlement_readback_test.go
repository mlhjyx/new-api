package controller

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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

func TestSettlementReadbackRootCredentialLifecycleReturnsSecretOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:settlement-readback-controller?mode=memory&cache=shared&_pragma=foreign_keys(1)"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.SettlementReadbackCredential{}))
	require.NoError(t, db.Create(&model.Token{Id: 42, UserId: 7, Key: "controller-reader-token", Status: common.TokenStatusEnabled}).Error)
	originalDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = originalDB })

	keyringPath := filepath.Join(t.TempDir(), "keyring")
	require.NoError(t, os.WriteFile(keyringPath, []byte("schema=settlement-readback-pepper-keyring/v1\npepper-v1 ACTIVE AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n"), 0o600))
	t.Setenv(settlementReadbackPepperKeyringFileEnv, keyringPath)

	createRecorder := httptest.NewRecorder()
	createContext, _ := gin.CreateTestContext(createRecorder)
	createContext.Request = httptest.NewRequest(http.MethodPost, "/api/settlement-readback/admin/v1/credentials", bytes.NewBufferString(`{"dispatch_token_id":42}`))
	CreateSettlementReadbackCredential(createContext)
	require.Equal(t, http.StatusCreated, createRecorder.Code)
	var created map[string]interface{}
	require.NoError(t, common.Unmarshal(createRecorder.Body.Bytes(), &created))
	data := created["data"].(map[string]interface{})
	secret := data["credential"].(string)
	credentialID := int(data["credential_id"].(float64))
	assert.NotEmpty(t, secret)
	assert.Equal(t, "no-store", createRecorder.Header().Get("Cache-Control"))
	var stored model.SettlementReadbackCredential
	require.NoError(t, db.First(&stored, credentialID).Error)
	assert.NotEqual(t, secret, stored.SecretDigest)

	rotateRecorder := httptest.NewRecorder()
	rotateContext, _ := gin.CreateTestContext(rotateRecorder)
	rotateContext.Params = gin.Params{{Key: "id", Value: strconv.Itoa(credentialID)}}
	rotateContext.Request = httptest.NewRequest(http.MethodPost, "/rotate", bytes.NewBufferString(`{"overlap_seconds":300}`))
	RotateSettlementReadbackCredential(rotateContext)
	require.Equal(t, http.StatusCreated, rotateRecorder.Code)
	assert.NotContains(t, rotateRecorder.Body.String(), secret)

	revokeRecorder := httptest.NewRecorder()
	revokeContext, _ := gin.CreateTestContext(revokeRecorder)
	revokeContext.Params = gin.Params{{Key: "id", Value: strconv.Itoa(credentialID)}}
	revokeContext.Request = httptest.NewRequest(http.MethodPost, "/revoke", nil)
	RevokeSettlementReadbackCredential(revokeContext)
	require.Equal(t, http.StatusOK, revokeRecorder.Code)
}

func TestSettlementReadbackAdminBodiesAreClosedBeforeCredentialWork(t *testing.T) {
	gin.SetMode(gin.TestMode)
	invalidBodies := []string{
		``,
		`null`,
		`[]`,
		`{"dispatch_token_id":1,"dispatch_token_id":2}`,
		`{"dispatch_token_id":1,"extra":2}`,
		`{"dispatch_token_id":"1"}`,
		`{"dispatch_token_id":1.0}`,
		`{"dispatch_token_id":1e0}`,
		`{"dispatch_token_id":0}`,
		`{"dispatch_token_id":1} trailing`,
	}
	for _, body := range invalidBodies {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(http.MethodPost, "/credentials", bytes.NewBufferString(body))
		CreateSettlementReadbackCredential(context)
		assert.Equal(t, http.StatusBadRequest, recorder.Code, body)
	}

	rotateRecorder := httptest.NewRecorder()
	rotateContext, _ := gin.CreateTestContext(rotateRecorder)
	rotateContext.Params = gin.Params{{Key: "id", Value: "1"}}
	rotateContext.Request = httptest.NewRequest(http.MethodPost, "/rotate", bytes.NewBufferString(`{"overlap_seconds":1,"overlap_seconds":2}`))
	RotateSettlementReadbackCredential(rotateContext)
	assert.Equal(t, http.StatusBadRequest, rotateRecorder.Code)

	revokeRecorder := httptest.NewRecorder()
	revokeContext, _ := gin.CreateTestContext(revokeRecorder)
	revokeContext.Params = gin.Params{{Key: "id", Value: "1"}}
	revokeContext.Request = httptest.NewRequest(http.MethodPost, "/revoke", bytes.NewBufferString(`{}`))
	RevokeSettlementReadbackCredential(revokeContext)
	assert.Equal(t, http.StatusBadRequest, revokeRecorder.Code)
}

func TestSettlementReadbackReturnsOnlyExactLinkedReceiptOrPending(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:settlement-readback-receipt?mode=memory&cache=shared&_pragma=foreign_keys(1)"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, model.EnsureSettlementReadbackSharedSchema(db))
	originalDB, originalLogDB := model.DB, model.LOG_DB
	originalMainType, originalLogType := common.MainDatabaseType(), common.LogDatabaseType()
	originalConsume := common.LogConsumeEnabled
	model.DB, model.LOG_DB = db, db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	common.LogConsumeEnabled = true
	t.Cleanup(func() {
		model.DB, model.LOG_DB = originalDB, originalLogDB
		common.SetMainDatabaseType(originalMainType)
		common.SetLogDatabaseType(originalLogType)
		common.LogConsumeEnabled = originalConsume
	})

	requestID := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	nonce := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	digest := func(value string) string {
		sum := sha256.Sum256([]byte(value))
		return hex.EncodeToString(sum[:])
	}
	binding := &model.SettlementReadbackBinding{
		DispatchTokenId:           42,
		SettlementRequestIdSha256: digest(requestID),
		SettlementNonceSha256:     digest(nonce),
		GatewayRequestId:          "gateway-internal",
		State:                     model.SettlementReadbackBindingBound,
		CreatedAt:                 1,
	}
	require.NoError(t, db.Create(binding).Error)
	require.True(t, model.BeginSettlementReadbackDispatch(db, binding.Id))
	log := &model.Log{
		Type:                model.LogTypeConsume,
		TokenId:             42,
		SettlementBindingId: &binding.Id,
		CreatedAt:           2,
		ModelName:           "claude-sonnet",
		ChannelId:           7,
		Quota:               1250,
		PromptTokens:        10,
		CompletionTokens:    20,
		UpstreamRequestId:   "internal-upstream-id",
		Other:               `{"usage_semantic":"anthropic","cache_creation_tokens":3,"cache_tokens":4}`,
	}
	require.NoError(t, model.LinkSettlementReadbackConsumeLog(db, binding.Id, log))

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Set(middleware.SettlementReadbackDispatchTokenContextKey, 42)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/settlement-readback/v1?request_id="+requestID, nil)
	context.Request.Header.Set("X-New-API-Settlement-Nonce", nonce)
	GetSettlementReadback(context)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, settlementReadbackContract, recorder.Header().Get("X-New-API-Settlement-Contract"))
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	assert.JSONEq(t, `{"data":[{"request_id":"`+requestID+`","type":"consume","model_name":"claude-sonnet","channel_id":7,"quota":"1250","prompt_tokens":10,"completion_tokens":20,"usage_semantic":"anthropic","cache_creation_tokens":3,"cache_read_tokens":4,"upstream_id_state":"observed"}]}`, recorder.Body.String())
	assert.NotContains(t, recorder.Body.String(), "internal-upstream-id")
	assert.NotContains(t, recorder.Body.String(), "gateway-internal")

	pendingID := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))
	pendingNonce := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{4}, 32))
	pending := &model.SettlementReadbackBinding{DispatchTokenId: 42, SettlementRequestIdSha256: digest(pendingID), SettlementNonceSha256: digest(pendingNonce), GatewayRequestId: "pending-internal", State: model.SettlementReadbackBindingDispatchStarted, CreatedAt: 3}
	require.NoError(t, db.Create(pending).Error)
	pendingRecorder := httptest.NewRecorder()
	pendingContext, _ := gin.CreateTestContext(pendingRecorder)
	pendingContext.Set(middleware.SettlementReadbackDispatchTokenContextKey, 42)
	pendingContext.Request = httptest.NewRequest(http.MethodGet, "/api/settlement-readback/v1?request_id="+pendingID, nil)
	pendingContext.Request.Header.Set("X-New-API-Settlement-Nonce", pendingNonce)
	GetSettlementReadback(pendingContext)
	require.Equal(t, http.StatusOK, pendingRecorder.Code)
	assert.JSONEq(t, `{"data":[]}`, pendingRecorder.Body.String())

	wrongRecorder := httptest.NewRecorder()
	wrongContext, _ := gin.CreateTestContext(wrongRecorder)
	wrongContext.Set(middleware.SettlementReadbackDispatchTokenContextKey, 42)
	wrongContext.Request = httptest.NewRequest(http.MethodGet, "/api/settlement-readback/v1?request_id="+requestID, nil)
	wrongContext.Request.Header.Set("X-New-API-Settlement-Nonce", pendingNonce)
	GetSettlementReadback(wrongContext)
	assert.Equal(t, http.StatusNotFound, wrongRecorder.Code)
	assert.Empty(t, wrongRecorder.Body.String())
}

func TestSettlementReadbackClosedReceiptRejectsOversizedOrUnknownOperationalMetadata(t *testing.T) {
	baseLog := model.Log{
		Type:             model.LogTypeConsume,
		ModelName:        "gpt-4o-mini",
		ChannelId:        7,
		Quota:            1250,
		PromptTokens:     10,
		CompletionTokens: 20,
	}

	tests := []struct {
		name  string
		other string
	}{
		{
			name:  "unknown top-level field",
			other: `{"usage_semantic":"openai","cache_creation_tokens":0,"cache_tokens":0,"prompt":"must-never-be-parsed"}`,
		},
		{
			name: "metadata beyond the 16 KiB contract",
			other: `{"usage_semantic":"openai","cache_creation_tokens":0,"cache_tokens":0,"admin_info":{"bounded_legacy":"` +
				strings.Repeat("x", 16*1024) + `"}}`,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			log := baseLog
			log.Other = testCase.other
			_, ok := settlementReadbackClosedReceipt(strings.Repeat("A", 43), &log)
			assert.False(t, ok)
		})
	}
}

func TestSettlementReadbackClosedReceiptAllowsOnlyBoundedKnownLegacyTopLevelMetadata(t *testing.T) {
	log := &model.Log{
		Type:             model.LogTypeConsume,
		ModelName:        "claude-sonnet",
		ChannelId:        7,
		Quota:            1250,
		PromptTokens:     10,
		CompletionTokens: 20,
		Other: `{"usage_semantic":"anthropic","cache_creation_tokens":3,"cache_tokens":4,` +
			`"model_ratio":1.5,"group_ratio":1,"completion_ratio":5,"cache_ratio":0.1,` +
			`"model_price":-1,"user_group_ratio":-1,"frt":12,"billing_source":"wallet",` +
			`"billing_preference":"wallet_only","request_path":"/v1/messages",` +
			`"request_conversion":["Claude Messages"],"claude":true,` +
			`"admin_info":{"operational_only":"ignored without recursive parsing"}}`,
	}

	receipt, ok := settlementReadbackClosedReceipt(strings.Repeat("B", 43), log)
	require.True(t, ok)
	assert.Equal(t, "anthropic", receipt.UsageSemantic)
	assert.EqualValues(t, 3, receipt.CacheCreationTokens)
	assert.EqualValues(t, 4, receipt.CacheReadTokens)
}

func TestSettlementReadbackReceiptParsesCanonicalInt64WithoutFloatLoss(t *testing.T) {
	base := model.Log{Type: model.LogTypeConsume, ChannelId: 1, ModelName: "model-v1", Quota: 0, PromptTokens: 1, CompletionTokens: 1}
	base.Other = `{"usage_semantic":"openai","cache_creation_tokens":9007199254740993,"cache_tokens":9223372036854775807}`
	receipt, ok := settlementReadbackClosedReceipt("request", &base)
	require.True(t, ok)
	assert.Equal(t, int64(9_007_199_254_740_993), receipt.CacheCreationTokens)
	assert.Equal(t, int64(9_223_372_036_854_775_807), receipt.CacheReadTokens)

	for _, other := range []string{
		`{"usage_semantic":"openai","cache_creation_tokens":1e3,"cache_tokens":0}`,
		`{"usage_semantic":"openai","cache_creation_tokens":-1,"cache_tokens":0}`,
		`{"usage_semantic":"openai","cache_creation_tokens":01,"cache_tokens":0}`,
		`{"usage_semantic":"openai","cache_creation_tokens":9223372036854775808,"cache_tokens":0}`,
		`{"usage_semantic":"openai","usage_semantic":"anthropic","cache_creation_tokens":0,"cache_tokens":0}`,
	} {
		candidate := base
		candidate.Other = other
		_, ok := settlementReadbackClosedReceipt("request", &candidate)
		assert.False(t, ok, other)
	}
}
