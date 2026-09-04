package middleware

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSettlementDispatchBindingRequiresExactSupportedBoundRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:settlement-dispatch-middleware?mode=memory&cache=shared&_pragma=foreign_keys(1)"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, model.EnsureSettlementReadbackSharedSchema(db))
	require.NoError(t, db.AutoMigrate(&model.Token{}))
	require.NoError(t, db.Create(&model.Token{Id: 42, UserId: 7, Key: "bound-dispatch", Status: common.TokenStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Token{Id: 43, UserId: 7, Key: "ordinary-dispatch", Status: common.TokenStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.SettlementReadbackCredential{LookupPrefix: "readerlookup0001", SecretDigest: "digest", PepperVersion: "pepper-v1", DispatchTokenId: 42, Status: model.SettlementReadbackCredentialActive, CreatedAt: 1}).Error)

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
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Set("token_id", 42)
	context.Set(common.RequestIdKey, "gateway-request-id")
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	context.Request.Header.Set("X-New-API-Settlement-Request-Id", requestID)
	context.Request.Header.Set("X-New-API-Settlement-Nonce", nonce)
	SettlementDispatchBinding()(context)
	require.False(t, context.IsAborted())
	assert.Positive(t, context.GetInt(SettlementReadbackBindingIDContextKey))
	assert.Empty(t, context.Request.Header.Get("X-New-API-Settlement-Request-Id"))
	assert.Empty(t, context.Request.Header.Get("X-New-API-Settlement-Nonce"))

	unsupported := httptest.NewRecorder()
	unsupportedContext, _ := gin.CreateTestContext(unsupported)
	unsupportedContext.Set("token_id", 42)
	unsupportedContext.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	SettlementDispatchBinding()(unsupportedContext)
	assert.True(t, unsupportedContext.IsAborted())
	assert.Equal(t, http.StatusForbidden, unsupported.Code)

	unbound := httptest.NewRecorder()
	unboundContext, _ := gin.CreateTestContext(unbound)
	unboundContext.Set("token_id", 43)
	unboundContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	unboundContext.Request.Header.Set("X-New-API-Settlement-Request-Id", requestID)
	unboundContext.Request.Header.Set("X-New-API-Settlement-Nonce", nonce)
	SettlementDispatchBinding()(unboundContext)
	assert.True(t, unboundContext.IsAborted())
	assert.Equal(t, http.StatusBadRequest, unbound.Code)

	for _, target := range []string{
		"/v1/chat/completions/",
		"/v1/chat/completions?mode=other",
		"//v1/chat/completions",
		"/v1/%63hat/completions",
		"/v1/chat/completions/../images/generations",
		"/v1beta/models/gemini:generateContent",
		"/v1/realtime",
		"/suno/submit/generate",
	} {
		rejected := httptest.NewRecorder()
		rejectedContext, _ := gin.CreateTestContext(rejected)
		rejectedContext.Set("token_id", 42)
		rejectedContext.Set(common.RequestIdKey, "gateway-rejected")
		rejectedContext.Request = httptest.NewRequest(http.MethodPost, target, nil)
		rejectedContext.Request.Header.Set("X-New-API-Settlement-Request-Id", requestID)
		rejectedContext.Request.Header.Set("X-New-API-Settlement-Nonce", nonce)
		SettlementDispatchBinding()(rejectedContext)
		assert.True(t, rejectedContext.IsAborted(), target)
		assert.Equal(t, http.StatusForbidden, rejected.Code, target)
	}
}
